package spool

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

var (
	ErrEmptyQueue = errors.New("spool: queue empty")
	ErrCorrupt    = errors.New("spool: corrupt payload")
)

const (
	notifierBuffer   = 1
	initialBackoff   = time.Second
	maxBackoff       = time.Minute
	queueFilePattern = "%020d-%06d.spool"
	maxFileAge       = time.Minute
	maxBufferFiles   = 1000
)

type Handler func(context.Context, []byte) error

type ErrorLogger interface {
	Log(error)
}

type ErrorLoggerFunc func(error)

func (f ErrorLoggerFunc) Log(err error) {
	f(err)
}

type Queue struct {
	dir         string
	notify      chan struct{}
	counter     uint64
	errorLogger ErrorLogger
}

func New(dir string) (*Queue, error) {
	return NewWithErrorLogger(dir, nil)
}

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

	if err := os.MkdirAll(cleaned, 0o755); err != nil {
		return nil, fmt.Errorf("spool: create dir: %w", err)
	}

	probe, err := os.CreateTemp(cleaned, ".spool-probe-*")
	if err != nil {
		return nil, fmt.Errorf("spool: probe write: %w", err)
	}
	probeName := probe.Name()
	if cerr := probe.Close(); cerr != nil {
		_ = os.Remove(probeName)
		return nil, fmt.Errorf("spool: probe close: %w", cerr)
	}
	if err := os.Remove(probeName); err != nil {
		return nil, fmt.Errorf("spool: probe cleanup: %w", err)
	}

	return &Queue{
		dir:         cleaned,
		notify:      make(chan struct{}, notifierBuffer),
		errorLogger: logger,
	}, nil
}

func (q *Queue) Enqueue(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("spool: empty payload")
	}
	if err := q.cleanOldFiles(); err != nil {
		q.logError(fmt.Errorf("spool: cleanup warning: %w", err))
	}

	seq := atomic.AddUint64(&q.counter, 1)
	name := fmt.Sprintf(queueFilePattern, time.Now().UnixNano(), seq%1_000_000)
	path := filepath.Join(q.dir, name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("spool: write payload: %w", err)
	}
	q.signal()
	return name, nil
}

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

func (q *Queue) Start(ctx context.Context, handler Handler) {
	go q.loop(ctx, handler)
	q.signal()
}

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

		token, payload, err := q.next()
		if err != nil {
			if errors.Is(err, ErrEmptyQueue) {
				if !q.wait(ctx) {
					return
				}
				backoff = initialBackoff
				continue
			}
			q.logError(fmt.Errorf("spool: fetch next: %w", err))
			if !q.waitWithBackoff(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		if err := handler(ctx, payload); err != nil {
			if errors.Is(err, ErrCorrupt) {
				q.logError(fmt.Errorf("spool: corrupt payload in %s: %w", token, err))
				_ = q.Complete(token)
				backoff = initialBackoff
				continue
			}
			q.logError(fmt.Errorf("spool: handler failed for %s: %w", token, err))
			if !q.waitWithBackoff(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		_ = q.Complete(token)
		backoff = initialBackoff
	}
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

func (q *Queue) next() (string, []byte, error) {
	name, err := q.oldest()
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(q.dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("spool: read payload: %w", err)
	}
	return name, data, nil
}

func (q *Queue) oldest() (string, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return "", fmt.Errorf("spool: read dir: %w", err)
	}
	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidates = append(candidates, entry.Name())
	}
	if len(candidates) == 0 {
		return "", ErrEmptyQueue
	}
	sort.Strings(candidates)
	return candidates[0], nil
}

func (q *Queue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// cleanOldFiles enforces retention limits when the queue exceeds the buffered file threshold.
func (q *Queue) cleanOldFiles() error {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	var spoolFiles []fs.DirEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".spool") {
			continue
		}
		spoolFiles = append(spoolFiles, entry)
	}
	if len(spoolFiles) <= maxBufferFiles {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(-maxFileAge)
	removed := 0

	for _, entry := range spoolFiles {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(q.dir, entry.Name())
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				q.logError(fmt.Errorf("remove old file %s: %w", entry.Name(), err))
			} else {
				removed++
			}
		}
	}

	if removed > 0 {
		q.logError(fmt.Errorf("cleaned %d old spool files (buffer: %d, threshold: %d)", removed, len(spoolFiles), maxBufferFiles))
	}

	return nil
}
