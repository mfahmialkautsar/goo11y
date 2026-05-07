package tracer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	traceFileDirMode = 0o755
	traceFileMode    = 0o644
	traceFileExt     = ".jsonl"
	traceJournalExt  = ".json"
	tracePendingExt  = ".pending"
)

type traceFileExporter struct {
	sink *dailyTraceFileSink
}

func newTraceFileExporter(cfg FileConfig) (sdktrace.SpanExporter, error) {
	sink, err := newDailyTraceFileSink(cfg)
	if err != nil {
		return nil, err
	}
	return &traceFileExporter{sink: sink}, nil
}

func (e *traceFileExporter) Shutdown(context.Context) error {
	if e == nil || e.sink == nil {
		return nil
	}
	return e.sink.Close()
}

func (e *traceFileExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	_ = ctx
	batch, err := encodeTraceBatch(spans)
	if err != nil {
		otlputil.LogExportFailure("tracer", "file", err)
		return err
	}
	if batch == nil {
		return nil
	}
	return e.exportBatch(batch)
}

func (e *traceFileExporter) exportBatch(batch *encodedTraceBatch) error {
	if batch == nil {
		return nil
	}
	if err := e.sink.Append(batch.JSON()); err != nil {
		otlputil.LogExportFailure("tracer", "file", err)
		return err
	}
	return nil
}

type dailyTraceFileSink struct {
	directory string
	buffer    int
	now       func() time.Time

	mu          sync.Mutex
	currentDate string
	file        *os.File
}

func newDailyTraceFileSink(cfg FileConfig) (*dailyTraceFileSink, error) {
	if cfg.Directory == "" {
		return nil, fmt.Errorf("missing trace export directory")
	}
	if cfg.Buffer <= 0 {
		return nil, fmt.Errorf("trace export buffer must be greater than zero")
	}

	return &dailyTraceFileSink{
		directory: cfg.Directory,
		buffer:    cfg.Buffer,
		now:       time.Now,
	}, nil
}

func (w *dailyTraceFileSink) Append(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}

	date := w.now().Format("2006-01-02")

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.ensureFile(date); err != nil {
		return err
	}
	if w.file == nil {
		return fmt.Errorf("trace export file handle unavailable")
	}

	writer := bufio.NewWriterSize(w.file, w.buffer)
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("write trace export file: %w", err)
	}
	if err := writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("write trace export newline: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush trace export file: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("sync trace export file: %w", err)
	}
	return nil
}

func (w *dailyTraceFileSink) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyTraceFileSink) ensureFile(date string) error {
	fileName := date + traceFileExt

	if err := os.MkdirAll(w.directory, traceFileDirMode); err != nil {
		return fmt.Errorf("create trace export directory: %w", err)
	}

	root, err := os.OpenRoot(w.directory)
	if err != nil {
		return fmt.Errorf("open trace root: %w", err)
	}
	defer func() { _ = root.Close() }()

	if w.currentDate == date && w.file != nil {
		if _, err := root.Stat(fileName); err == nil {
			return nil
		}
	}

	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	file, err := root.OpenFile(fileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, traceFileMode)
	if err != nil {
		return fmt.Errorf("open trace export file: %w", err)
	}

	w.file = file
	w.currentDate = date
	return nil
}
