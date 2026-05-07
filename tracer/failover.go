package tracer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
)

const (
	initialReplayBackoff = time.Second
	maxReplayBackoff     = time.Minute
)

type traceFailoverJournal struct {
	directory string
	buffer    int
	now       func() time.Time
	seq       atomic.Uint64
}

func newTraceFailoverJournal(cfg FailoverConfig) (*traceFailoverJournal, error) {
	if cfg.Directory == "" {
		return nil, fmt.Errorf("missing trace failover directory")
	}
	if cfg.Buffer <= 0 {
		return nil, fmt.Errorf("trace failover buffer must be greater than zero")
	}
	return &traceFailoverJournal{
		directory: cfg.Directory,
		buffer:    cfg.Buffer,
		now:       time.Now,
	}, nil
}

func (j *traceFailoverJournal) RecoverPending() error {
	if err := os.MkdirAll(j.directory, traceFileDirMode); err != nil {
		return fmt.Errorf("create trace failover directory: %w", err)
	}

	entries, err := os.ReadDir(j.directory)
	if err != nil {
		return fmt.Errorf("read trace failover directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, ".tmp-"):
			if err := os.Remove(filepath.Join(j.directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove temp trace failover file %s: %w", name, err)
			}
		case strings.HasSuffix(name, tracePendingExt):
			if _, err := j.PromotePending(name); err != nil {
				return err
			}
		}
	}

	return nil
}

func (j *traceFailoverJournal) StorePending(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("empty trace failover payload")
	}
	if err := os.MkdirAll(j.directory, traceFileDirMode); err != nil {
		return "", fmt.Errorf("create trace failover directory: %w", err)
	}

	seq := j.seq.Add(1)
	baseName := fmt.Sprintf("%020d-%06d", j.now().UTC().UnixNano(), seq%1_000_000)
	pendingName := baseName + tracePendingExt
	pendingPath := filepath.Join(j.directory, pendingName)

	tmpFile, err := os.CreateTemp(j.directory, ".tmp-trace-*")
	if err != nil {
		return "", fmt.Errorf("create trace failover temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	writer := bufio.NewWriterSize(tmpFile, j.buffer)
	if _, err := writer.Write(payload); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("write trace failover payload: %w", err)
	}
	if err := writer.WriteByte('\n'); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("write trace failover newline: %w", err)
	}
	if err := writer.Flush(); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("flush trace failover payload: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("sync trace failover payload: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close trace failover payload: %w", err)
	}
	if err := os.Rename(tmpName, pendingPath); err != nil {
		return "", fmt.Errorf("promote trace failover temp file: %w", err)
	}

	return pendingName, nil
}

func (j *traceFailoverJournal) PromotePending(name string) (string, error) {
	if !strings.HasSuffix(name, tracePendingExt) {
		return "", fmt.Errorf("trace failover file %s is not pending", name)
	}

	readyName := strings.TrimSuffix(name, tracePendingExt) + traceJournalExt
	pendingPath := filepath.Join(j.directory, name)
	readyPath := filepath.Join(j.directory, readyName)
	if err := os.Rename(pendingPath, readyPath); err != nil {
		return "", fmt.Errorf("promote trace failover file %s: %w", name, err)
	}
	return readyName, nil
}

func (j *traceFailoverJournal) Delete(name string) error {
	if name == "" {
		return nil
	}
	if err := os.Remove(filepath.Join(j.directory, filepath.Base(name))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove trace failover file %s: %w", name, err)
	}
	return nil
}

func (j *traceFailoverJournal) OldestReady() (string, bool, error) {
	entries, err := os.ReadDir(j.directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read trace failover directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), traceJournalExt) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", false, nil
	}
	sort.Strings(names)
	return names[0], true, nil
}

func (j *traceFailoverJournal) Read(name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(j.directory, filepath.Base(name)))
	if err != nil {
		return nil, err
	}
	return data, nil
}

type traceReplayManager struct {
	journal *traceFailoverJournal
	sender  traceBackendSender
	notify  chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

func newTraceReplayManager(journal *traceFailoverJournal, sender traceBackendSender) *traceReplayManager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &traceReplayManager{
		journal: journal,
		sender:  sender,
		notify:  make(chan struct{}, 1),
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	go manager.run(ctx)
	manager.Notify()

	return manager
}

func (m *traceReplayManager) Notify() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *traceReplayManager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}

	m.once.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
	})

	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *traceReplayManager) run(ctx context.Context) {
	defer close(m.done)

	backoff := initialReplayBackoff
	for {
		name, ok, err := m.journal.OldestReady()
		if err != nil {
			otlputil.LogExportFailure("tracer", "file", err)
			if !m.wait(ctx, backoff) {
				return
			}
			backoff = nextReplayBackoff(backoff)
			continue
		}
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-m.notify:
				backoff = initialReplayBackoff
				continue
			}
		}

		payload, err := m.journal.Read(name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				backoff = initialReplayBackoff
				continue
			}
			otlputil.LogExportFailure("tracer", "file", err)
			if !m.wait(ctx, backoff) {
				return
			}
			backoff = nextReplayBackoff(backoff)
			continue
		}

		batch := &encodedTraceBatch{json: payload}
		if err := m.sender.Send(ctx, batch); err != nil {
			otlputil.LogExportFailure("tracer", m.sender.Transport(), err)
			if errors.Is(err, errTracePayloadCorrupt) {
				if deleteErr := m.journal.Delete(name); deleteErr != nil {
					otlputil.LogExportFailure("tracer", "file", deleteErr)
				}
				backoff = initialReplayBackoff
				continue
			}
			if !m.wait(ctx, backoff) {
				return
			}
			backoff = nextReplayBackoff(backoff)
			continue
		}

		if err := m.journal.Delete(name); err != nil {
			otlputil.LogExportFailure("tracer", "file", err)
			if !m.wait(ctx, backoff) {
				return
			}
			backoff = nextReplayBackoff(backoff)
			continue
		}

		backoff = initialReplayBackoff
	}
}

func (m *traceReplayManager) wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-m.notify:
		return true
	case <-timer.C:
		return true
	}
}

func nextReplayBackoff(current time.Duration) time.Duration {
	if current >= maxReplayBackoff {
		return maxReplayBackoff
	}
	next := current * 2
	if next > maxReplayBackoff {
		return maxReplayBackoff
	}
	return next
}
