package spool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRequestMarshalNil(t *testing.T) {
	var req *HTTPRequest
	if _, err := req.Marshal(); err == nil {
		t.Fatal("expected error for nil marshal")
	}
}

func TestHTTPRequestMarshalUnmarshal(t *testing.T) {
	original := &HTTPRequest{
		Method: http.MethodPost,
		URL:    "http://example.com/upload",
		Header: map[string][]string{"X-Test": {"value"}},
		Body:   []byte("payload"),
	}
	payload, err := original.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored HTTPRequest
	if err := restored.Unmarshal(payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Method != original.Method || restored.URL != original.URL {
		t.Fatalf("unexpected request: %#v", restored)
	}
	if len(restored.Header["X-Test"]) != 1 || restored.Header["X-Test"][0] != "value" {
		t.Fatalf("unexpected header: %#v", restored.Header)
	}
	if string(restored.Body) != "payload" {
		t.Fatalf("unexpected body: %q", restored.Body)
	}
}

func TestHTTPRequestUnmarshalNil(t *testing.T) {
	var req *HTTPRequest
	if err := req.Unmarshal([]byte("{}")); err == nil {
		t.Fatal("expected error for nil unmarshal")
	}
}

type capturedRequest struct {
	method string
	header http.Header
	body   []byte
}

func TestHTTPHandlerSuccess(t *testing.T) {
	received := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		_ = r.Body.Close()
		received <- capturedRequest{method: r.Method, header: r.Header.Clone(), body: data}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	handler := HTTPHandler(srv.Client())

	req := &HTTPRequest{
		Method: http.MethodPut,
		URL:    srv.URL + "/ingest",
		Header: map[string][]string{"X-Custom": {"A", "B"}},
		Body:   []byte("hello"),
	}
	payload, err := req.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := handler(context.Background(), payload); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	httpReq := <-received
	if httpReq.method != http.MethodPut {
		t.Fatalf("unexpected method: %s", httpReq.method)
	}
	if got := httpReq.header.Values("X-Custom"); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("unexpected header: %v", got)
	}
	if string(httpReq.body) != "hello" {
		t.Fatalf("unexpected body: %q", httpReq.body)
	}
}

func TestHTTPHandlerErrors(t *testing.T) {
	handler := HTTPHandler(http.DefaultClient)
	if err := handler(context.Background(), []byte("not-json")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	req := &HTTPRequest{Method: http.MethodGet, URL: srv.URL}
	payload, err := req.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := HTTPHandler(srv.Client())(context.Background(), payload); err == nil {
		t.Fatal("expected non-2xx error")
	}
}
