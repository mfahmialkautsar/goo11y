package spool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// ErrEmptyQueue is returned when attempting to pop from an empty queue.
	ErrEmptyQueue = errors.New("spool: queue empty")
	// ErrCorrupt is returned when a payload cannot be read or parsed properly.
	ErrCorrupt = errors.New("spool: corrupt payload")
	defaultNow = time.Now
)

const (
	notifierBuffer = 1
	initialBackoff = time.Second
	maxBackoff     = time.Minute

	maxRetryAttempts = 10
	staleAttemptAge  = 7 * 24 * time.Hour

	tokenSuffix       = ".spool"
	tokenLegacyParts  = 2
	tokenCurrentParts = 4

	defaultQueueMaxFiles  = 1000
	defaultRetryBaseDelay = time.Second
	defaultRetryMaxDelay  = time.Minute
)

// Handler represents a function that processes a dequeued payload.
type Handler func(context.Context, []byte) error

// ErrorLogger is used by the Queue to log internal errors.
type ErrorLogger interface {
	Log(error)
}

// ErrorLoggerFunc is an adapter to allow the use of ordinary functions as an ErrorLogger.
type ErrorLoggerFunc func(error)

// Log calls f(err).
func (f ErrorLoggerFunc) Log(err error) {
	f(err)
}

// Queue provides a disk-backed, reliable queue for delayed processing.
type Queue struct {
	dir         string
	notify      chan struct{}
	counter     uint64
	errorLogger ErrorLogger

	// Configuration
	maxFiles  int
	retryBase time.Duration
	retryMax  time.Duration
	now       func() time.Time
}

type fileToken struct {
	name      string
	retryAt   time.Time
	createdAt time.Time
	seq       int
	attempts  int
}

// New creates a new Queue backed by the given directory.
func New(dir string) (*Queue, error) {
	return NewWithErrorLogger(dir, nil)
}

// NewWithErrorLogger creates a new Queue with a custom ErrorLogger.
func NewWithErrorLogger(dir string, logger ErrorLogger) (*Queue, error) {
	if dir == "" {
		return nil, fmt.Errorf("spool: queue dir is required")
	}

	cleaned := filepath.Clean(dir)
	if !filepath.IsAbs(cleaned) {
		if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}

	if err := os.MkdirAll(cleaned, 0o750); err != nil {
		return nil, fmt.Errorf("spool: create dir: %w", err)
	}

	probe, err := os.CreateTemp(cleaned, ".spool-probe-*")
	if err != nil {
		return nil, fmt.Errorf("spool: probe write: %w", err)
	}
	probeName := filepath.Base(probe.Name())
	root, err := spoofOpenRoot(cleaned)
	if err != nil {
		return nil, fmt.Errorf("spool: open root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	if cerr := probe.Close(); cerr != nil {
		_ = root.Remove(probeName)
		return nil, fmt.Errorf("spool: probe close: %w", cerr)
	}
	if err := root.Remove(probeName); err != nil {
		return nil, fmt.Errorf("spool: probe cleanup: %w", err)
	}

	return &Queue{
		dir:         cleaned,
		notify:      make(chan struct{}, notifierBuffer),
		errorLogger: logger,
		maxFiles:    defaultQueueMaxFiles,
		retryBase:   defaultRetryBaseDelay,
		retryMax:    defaultRetryMaxDelay,
		now:         defaultNow,
	}, nil
}

// Enqueue adds a payload to the queue.
func (q *Queue) Enqueue(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("spool: empty payload")
	}
	if err := q.cleanOldFiles(); err != nil {
		q.logError(fmt.Errorf("spool: cleanup warning: %w", err))
	}

	now := q.now()
	seq := int(atomic.AddUint64(&q.counter, 1) % 1_000_000)
	token := fileToken{
		retryAt:   now,
		createdAt: now,
		seq:       seq,
		attempts:  0,
	}
	name := formatToken(token)
	path := filepath.Join(q.dir, name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("spool: write payload: %w", err)
	}
	q.signal()
	return name, nil
}

// Complete removes a processed payload from the queue.
func (q *Queue) Complete(token string) error {
	if token == "" {
		return nil
	}
	if strings.Contains(token, string(os.PathSeparator)) {
		return fmt.Errorf("spool: invalid token path")
	}
	path := filepath.Join(q.dir, token)
	if !strings.HasPrefix(path, q.dir+string(os.PathSeparator)) {
		return fmt.Errorf("spool: invalid token path")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("spool: remove payload: %w", err)
	}
	return nil
}

// Start begins processing the queue in the background using the given handler.
func (q *Queue) Start(ctx context.Context, handler Handler) {
	go q.loop(ctx, handler)
	q.signal()
}

// Notify triggers the queue to process immediately.
func (q *Queue) Notify() {
	q.signal()
}

func (q *Queue) loop(ctx context.Context, handler Handler) {
	backoff := initialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !q.processNext(ctx, handler, &backoff) {
			return
		}
	}
}

func (q *Queue) processNext(ctx context.Context, handler Handler, backoff *time.Duration) bool {
	token, count, err := q.oldest()
	if err != nil {
		return q.handleOldestError(ctx, err, backoff)
	}

	if delay := time.Until(token.retryAt); delay > 0 {
		if !q.waitWithBackoff(ctx, delay) {
			return false
		}
		*backoff = initialBackoff
		return true
	}

	payload, err := q.readPayload(token.name)
	if err != nil {
		return q.handleReadError(ctx, token.name, err, backoff)
	}

	if err := handler(ctx, payload); err != nil {
		return q.handleHandlerError(ctx, &token, count, err, backoff)
	}

	if err := q.Complete(token.name); err != nil {
		q.logError(err)
	}
	*backoff = initialBackoff
	return true
}

func (q *Queue) handleOldestError(ctx context.Context, err error, backoff *time.Duration) bool {
	if errors.Is(err, ErrEmptyQueue) {
		if !q.wait(ctx) {
			return false
		}
		*backoff = initialBackoff
		return true
	}
	q.logError(fmt.Errorf("spool: fetch next: %w", err))
	if !q.waitWithBackoff(ctx, *backoff) {
		return false
	}
	*backoff = nextBackoff(*backoff)
	return true
}

func (q *Queue) handleReadError(ctx context.Context, name string, err error, backoff *time.Duration) bool {
	q.logError(fmt.Errorf("spool: read payload for %s: %w", name, err))
	if errors.Is(err, fs.ErrNotExist) {
		*backoff = initialBackoff
		return true
	}
	if !q.waitWithBackoff(ctx, *backoff) {
		return false
	}
	*backoff = nextBackoff(*backoff)
	return true
}

func (q *Queue) handleHandlerError(ctx context.Context, token *fileToken, count int, err error, backoff *time.Duration) bool {
	if errors.Is(err, ErrCorrupt) {
		q.logError(fmt.Errorf("spool: corrupt payload in %s: %w", token.name, err))
		_ = q.Complete(token.name)
		*backoff = initialBackoff
		return true
	}
	q.logError(fmt.Errorf("spool: handler failed for %s: %w", token.name, err))
	if q.shouldDrop(*token, count) {
		_ = q.Complete(token.name)
	} else if err := q.scheduleRetry(*token); err != nil {
		q.logError(fmt.Errorf("spool: schedule retry for %s: %w", token.name, err))
		if !q.waitWithBackoff(ctx, *backoff) {
			return false
		}
		*backoff = nextBackoff(*backoff)
		return true
	}
	*backoff = initialBackoff
	return true
}

func (q *Queue) logError(err error) {
	if q.errorLogger != nil {
		q.errorLogger.Log(err)
	}
}

func (q *Queue) wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-q.notify:
		return true
	}
}

func (q *Queue) waitWithBackoff(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-q.notify:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	if next < initialBackoff {
		return initialBackoff
	}
	return next
}

func (q *Queue) oldest() (fileToken, int, error) {
	tokens, err := q.listTokens()
	if err != nil {
		return fileToken{}, 0, err
	}
	if len(tokens) == 0 {
		return fileToken{}, 0, ErrEmptyQueue
	}
	sortTokens(tokens)
	return tokens[0], len(tokens), nil
}

func (q *Queue) listTokens() ([]fileToken, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil, fmt.Errorf("spool: read dir: %w", err)
	}
	tokens := make([]fileToken, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, tokenSuffix) {
			continue
		}
		meta, err := parseToken(name)
		if err != nil {
			q.logError(fmt.Errorf("spool: invalid token %s: %w", name, err))
			continue
		}
		tokens = append(tokens, meta)
	}
	return tokens, nil
}

func sortTokens(tokens []fileToken) {
	sort.Slice(tokens, func(i, j int) bool {
		a, b := tokens[i], tokens[j]
		if !a.retryAt.Equal(b.retryAt) {
			return a.retryAt.Before(b.retryAt)
		}
		if !a.createdAt.Equal(b.createdAt) {
			return a.createdAt.Before(b.createdAt)
		}
		if a.seq != b.seq {
			return a.seq < b.seq
		}
		return a.name < b.name
	})
}

func (q *Queue) readPayload(name string) ([]byte, error) {
	root, err := spoofOpenRoot(q.dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = root.Close()
	}()

	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	return io.ReadAll(f)
}

func parseToken(name string) (fileToken, error) {
	base := strings.TrimSuffix(name, tokenSuffix)
	parts := strings.Split(base, "-")
	switch len(parts) {
	case tokenCurrentParts:
		retryNano, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return fileToken{}, fmt.Errorf("parse retry timestamp: %w", err)
		}
		createdNano, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return fileToken{}, fmt.Errorf("parse created timestamp: %w", err)
		}
		seq, err := strconv.Atoi(parts[2])
		if err != nil {
			return fileToken{}, fmt.Errorf("parse seq: %w", err)
		}
		attempts, err := strconv.Atoi(parts[3])
		if err != nil {
			return fileToken{}, fmt.Errorf("parse attempts: %w", err)
		}
		return fileToken{
			name:      name,
			retryAt:   time.Unix(0, retryNano),
			createdAt: time.Unix(0, createdNano),
			seq:       seq,
			attempts:  attempts,
		}, nil
	case tokenLegacyParts:
		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return fileToken{}, fmt.Errorf("parse legacy timestamp: %w", err)
		}
		seq, err := strconv.Atoi(parts[1])
		if err != nil {
			return fileToken{}, fmt.Errorf("parse legacy seq: %w", err)
		}
		t := time.Unix(0, ts)
		return fileToken{
			name:      name,
			retryAt:   t,
			createdAt: t,
			seq:       seq,
			attempts:  0,
		}, nil
	default:
		return fileToken{}, fmt.Errorf("unexpected token format")
	}
}

func formatToken(token fileToken) string {
	retry := token.retryAt.UnixNano()
	if retry < 0 {
		retry = 0
	}
	created := token.createdAt.UnixNano()
	if created < 0 {
		created = 0
	}
	if token.seq < 0 {
		token.seq = 0
	}
	if token.attempts < 0 {
		token.attempts = 0
	}
	return fmt.Sprintf("%020d-%020d-%06d-%03d%s", retry, created, token.seq%1_000_000, token.attempts, tokenSuffix)
}

func (q *Queue) shouldDrop(token fileToken, queueLen int) bool {
	if queueLen >= q.maxFiles {
		return true
	}
	if token.attempts+1 >= maxRetryAttempts {
		if q.now().Sub(token.createdAt) > staleAttemptAge {
			return true
		}
	}
	return false
}

func (q *Queue) scheduleRetry(token fileToken) error {
	next := token
	next.attempts++
	delay := q.retryDelay(next.attempts)
	next.retryAt = q.now().Add(delay)
	next.seq = int(atomic.AddUint64(&q.counter, 1) % 1_000_000)
	newName := formatToken(next)
	oldPath := filepath.Join(q.dir, token.name)
	newPath := filepath.Join(q.dir, newName)
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	q.signal()
	return nil
}

func (q *Queue) retryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		attempts = 1
	}
	delay := q.retryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= q.retryMax {
			return q.retryMax
		}
	}
	if delay < q.retryBase {
		return q.retryBase
	}
	if delay > q.retryMax {
		return q.retryMax
	}
	return delay
}

func (q *Queue) cleanOldFiles() error {
	tokens, err := q.listTokens()
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return nil
	}

	removedStale := q.removeStaleFiles(tokens)

	tokens, err = q.listTokens()
	if err != nil {
		return err
	}

	removedOverflow := q.removeOverflowFiles(tokens)
	removed := removedStale + removedOverflow

	if removed > 0 {
		q.logError(fmt.Errorf("cleaned %d spool files (buffer: %d, threshold: %d)", removed, len(tokens), q.maxFiles))
	}

	return nil
}

func (q *Queue) removeStaleFiles(tokens []fileToken) int {
	sortTokens(tokens)
	now := q.now()
	removed := 0
	for _, token := range tokens {
		if token.attempts >= maxRetryAttempts && now.Sub(token.createdAt) > staleAttemptAge {
			if err := q.Complete(token.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				q.logError(fmt.Errorf("spool: remove stale file %s: %w", token.name, err))
			} else {
				removed++
			}
		}
	}
	return removed
}

func (q *Queue) removeOverflowFiles(tokens []fileToken) int {
	if len(tokens) <= q.maxFiles {
		return 0
	}

	sortTokens(tokens)
	removed := 0
	excess := len(tokens) - q.maxFiles
	for i := 0; i < excess; i++ {
		name := tokens[i].name
		if err := q.Complete(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			q.logError(fmt.Errorf("spool: remove overflow file %s: %w", name, err))
		} else {
			removed++
		}
	}
	return removed
}

func (q *Queue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
