package logger

import (
	"io"
	"os"
	"strings"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
)

type namedWriter struct {
	name   string
	writer io.Writer
}

type writerRegistry struct {
	writers []namedWriter
}

func newWriterRegistry() *writerRegistry {
	return &writerRegistry{writers: make([]namedWriter, 0)}
}

func (f *writerRegistry) add(name string, writer io.Writer) {
	if writer == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "custom"
	}
	f.writers = append(f.writers, namedWriter{name: name, writer: writer})
}

func (f *writerRegistry) len() int {
	return len(f.writers)
}

func (f *writerRegistry) writer() io.Writer {
	if len(f.writers) == 0 {
		return io.Discard
	}
	return fanoutWriter{writers: append([]namedWriter(nil), f.writers...)}
}

func (f *writerRegistry) writerExcept(excluded ...string) io.Writer {
	if len(f.writers) == 0 {
		return os.Stderr
	}
	if len(excluded) == 0 {
		return fanoutWriter{writers: append([]namedWriter(nil), f.writers...)}
	}
	exclude := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		exclude[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	filtered := make([]namedWriter, 0, len(f.writers))
	for _, w := range f.writers {
		if _, skip := exclude[w.name]; skip {
			continue
		}
		filtered = append(filtered, w)
	}
	if len(filtered) == 0 {
		return os.Stderr
	}
	return fanoutWriter{writers: filtered}
}

type fanoutWriter struct {
	writers []namedWriter
}

func (w fanoutWriter) Write(p []byte) (int, error) {
	if len(w.writers) == 0 {
		return len(p), nil
	}
	var firstErr error
	for _, writer := range w.writers {
		if writer.writer == nil {
			continue
		}
		if _, err := writer.writer.Write(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			otlputil.LogExportFailure("logger", writer.name, err)
		}
	}
	if firstErr != nil {
		return len(p), firstErr
	}
	return len(p), nil
}
