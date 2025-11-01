package logger

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOTLPSendSync(t *testing.T) {
	received := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	writer := &otlpWriter{
		endpoint: srv.URL,
		headers:  map[string][]string{"X-Test": {"value"}},
		client:   srv.Client(),
	}

	if err := writer.sendSync([]byte(`{"message":"ok"}`)); err != nil {
		t.Fatalf("sendSync: %v", err)
	}

	req := <-received
	if req.Method != http.MethodPost {
		t.Fatalf("unexpected method: %s", req.Method)
	}
	if req.Header.Get("X-Test") != "value" {
		t.Fatalf("unexpected header: %s", req.Header.Get("X-Test"))
	}
}

func TestOTLPSendSyncError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	writer := &otlpWriter{
		endpoint: srv.URL,
		headers:  map[string][]string{},
		client:   srv.Client(),
	}

	if err := writer.sendSync([]byte(`{"message":"fail"}`)); err == nil {
		t.Fatal("expected remote status error")
	}
}

func TestConfigureTransportInsecure(t *testing.T) {
	transport := configureTransport(true)
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", transport)
	}
	if httpTransport.TLSClientConfig == nil || !httpTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify true, got %+v", httpTransport.TLSClientConfig)
	}
}

func TestConfigureTransportSecure(t *testing.T) {
	transport := configureTransport(false)
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", transport)
	}
	if httpTransport.TLSClientConfig != nil && httpTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unexpected InsecureSkipVerify true for secure transport")
	}
}

func TestAnyToAttribute(t *testing.T) {
	cases := []struct {
		key   string
		value any
	}{
		{"str", "value"},
		{"bool", true},
		{"int", 42.0},
		{"nan", math.NaN()},
		{"map", map[string]any{"foo": "bar"}},
		{"slice", []any{"a", 1}},
		{"struct", struct{ X string }{X: "x"}},
	}

	for _, tc := range cases {
		attr, ok := anyToAttribute(tc.key, tc.value)
		if tc.key == "nan" {
			if !ok || attr.Key != tc.key {
				t.Fatalf("expected attribute for %s", tc.key)
			}
			continue
		}
		if !ok {
			t.Fatalf("expected attribute for %s", tc.key)
		}
		if attr.Key != tc.key {
			t.Fatalf("unexpected key: %s", attr.Key)
		}
	}

	if _, ok := anyToAttribute("nil", nil); ok {
		t.Fatal("expected nil value to be skipped")
	}
}

func TestDuplicateAttributes(t *testing.T) {
	attrs := []otlpKeyValue{{Key: "a", Value: otlpValue{StringValue: "b"}}}
	dup := duplicateAttributes(attrs)
	if len(dup) != 1 || dup[0].Key != "a" {
		t.Fatalf("unexpected duplicate: %#v", dup)
	}
	if &dup[0] == &attrs[0] {
		t.Fatal("expected copy of attributes")
	}
	if duplicateAttributes(nil) != nil {
		t.Fatal("expected nil copy for empty slice")
	}
}

func TestSeverityNumber(t *testing.T) {
	cases := map[string]int{
		"trace":   1,
		"debug":   5,
		"info":    9,
		"warn":    13,
		"error":   17,
		"fatal":   21,
		"panic":   21,
		"unknown": 9,
	}
	for level, expected := range cases {
		if got := severityNumber(level); got != expected {
			t.Fatalf("level %s expected %d, got %d", level, expected, got)
		}
	}
}

func TestStringKeyValue(t *testing.T) {
	attr := stringKeyValue("foo", "bar")
	if attr.Key != "foo" || attr.Value.StringValue != "bar" {
		t.Fatalf("unexpected key value: %#v", attr)
	}
}

func TestOTLPWriterWriteAsync(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := OTLPConfig{
		Endpoint: srv.Listener.Addr().String(),
		Insecure: true,
		Timeout:  time.Second,
		Async:    true,
		UseSpool: false,
	}
	writer, err := newOTLPWriter(cfg, "svc", "test")
	if err != nil {
		t.Fatalf("newOTLPWriter: %v", err)
	}
	ow := writer.(*otlpWriter)
	ow.endpoint = "http://" + cfg.Endpoint
	ow.client = srv.Client()

	if _, err := ow.Write([]byte(`{"message":"async"}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for async send")
	}
}

func TestOTLPWriterWriteSpool(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tempDir := t.TempDir()
	cfg := OTLPConfig{
		Endpoint: srv.Listener.Addr().String(),
		Insecure: true,
		Timeout:  time.Second,
		UseSpool: true,
		QueueDir: tempDir,
		Async:    false,
	}
	writer, err := newOTLPWriter(cfg, "svc", "test")
	if err != nil {
		t.Fatalf("newOTLPWriter: %v", err)
	}
	ow := writer.(*otlpWriter)
	ow.endpoint = "http://" + cfg.Endpoint
	ow.client = srv.Client()

	if _, err := ow.Write([]byte(`{"message":"spool"}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for spool send")
	}
}
