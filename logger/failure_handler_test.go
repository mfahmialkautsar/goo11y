package logger

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/mfahmialkautsar/goo11y/internal/otlputil"
	"github.com/rs/zerolog"
)

type failingWriter struct{}

func (f failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("fail")
}

type capturingHandler struct {
	mu      sync.Mutex
	records []string
}

func (c *capturingHandler) append(component, transport string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, component+":"+transport)
}

func (c *capturingHandler) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.records))
	copy(out, c.records)
	return out
}

func TestExportFailureLoggerSkipsFailingTransport(t *testing.T) {
	fanout := newWriterRegistry()
	otlpBuf := &bytes.Buffer{}
	stdoutBuf := &bytes.Buffer{}
	fanout.add("http", otlpBuf)
	fanout.add("stdout", stdoutBuf)

	base := zerolog.New(fanout.writer())
	log := &Logger{
		Logger:  &base,
		outputs: fanout,
	}

	handler := exportFailureLogger(log)
	handler("logger", "http", errors.New("boom"))

	if otlpBuf.Len() != 0 {
		t.Fatalf("expected otlp writer to be skipped, got %q", otlpBuf.String())
	}

	if stdoutBuf.Len() == 0 {
		t.Fatalf("expected stdout writer to receive failure log")
	}
}

func TestWriterRegistryReportsFailures(t *testing.T) {
	fanout := newWriterRegistry()
	payload := []byte(`{"msg":"hi"}`)

	okBuf := &bytes.Buffer{}
	fanout.add("stdout", okBuf)
	fanout.add("http", failingWriter{})

	collector := &capturingHandler{}
	otlputil.SetExportFailureHandler(func(component, transport string, err error) {
		collector.append(component, transport)
	})
	defer otlputil.SetExportFailureHandler(nil)
	writer := fanout.writer()
	if _, err := writer.Write(payload); err == nil {
		t.Fatalf("expected write error when one writer fails")
	}

	if okBuf.Len() == 0 {
		t.Fatalf("expected successful writer to receive payload")
	}

	records := collector.snapshot()
	if len(records) == 0 {
		t.Fatalf("expected failure handler invocation")
	}
	if records[0] != "logger:http" {
		t.Fatalf("unexpected handler record %q", records[0])
	}
}

func TestFailureExclusionsEmptyTransport(t *testing.T) {
	got := failureExclusions("logger", "")
	wanted := map[string]struct{}{
		"http":    {},
		"grpc":    {},
		"file":    {},
		"stdout":  {},
		"stderr":  {},
		"console": {},
	}
	if len(got) != len(wanted) {
		t.Fatalf("expected %d exclusions, got %d", len(wanted), len(got))
	}
	for _, entry := range got {
		if _, ok := wanted[entry]; !ok {
			t.Fatalf("unexpected exclusion %q", entry)
		}
	}
}

func TestWriterRegistryWriterExceptFallback(t *testing.T) {
	fanout := newWriterRegistry()
	fanout.add("stdout", &bytes.Buffer{})
	writer := fanout.writerExcept("stdout")
	file, ok := writer.(*os.File)
	if !ok || file != os.Stderr {
		t.Fatalf("expected os.Stderr fallback, got %#v", writer)
	}
}

func TestFanoutWriterHandlesNil(t *testing.T) {
	buf := &bytes.Buffer{}
	fanout := fanoutWriter{writers: []namedWriter{{name: "nil"}, {name: "stdout", writer: buf}}}
	data := []byte("payload")
	if n, err := fanout.Write(data); err != nil || n != len(data) {
		t.Fatalf("unexpected write result n=%d err=%v", n, err)
	}
	if buf.String() != "payload" {
		t.Fatalf("expected buffer to receive payload, got %q", buf.String())
	}
}

func TestNilWriterWrite(t *testing.T) {
	data := []byte("noop")
	if n, err := (nilWriter{}).Write(data); err != nil || n != len(data) {
		t.Fatalf("nilWriter returned n=%d err=%v", n, err)
	}
}
