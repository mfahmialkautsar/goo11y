package persistenthttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/spool"
)

func NewClient(queueDir string, timeout time.Duration) (*http.Client, error) {
	queue, err := spool.NewWithErrorLogger(queueDir, spool.ErrorLoggerFunc(func(err error) {
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[persistenthttp] %v\n", err)
	}))
	if err != nil {
		return nil, err
	}

	transport := cloneDefaultTransport()
	workerClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	queue.Start(context.Background(), spool.HTTPHandler(workerClient))

	persistent := &transportWrapper{queue: queue}

	return &http.Client{
		Timeout:   timeout,
		Transport: persistent,
	}, nil
}

type transportWrapper struct {
	queue *spool.Queue
}

func (t *transportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("persistenthttp: nil request")
	}

	body, err := readAll(req.Body)
	if err != nil {
		return nil, err
	}

	headerCopy := make(map[string][]string, len(req.Header))
	for key, values := range req.Header {
		vv := make([]string, len(values))
		copy(vv, values)
		headerCopy[key] = vv
	}

	entry := &spool.HTTPRequest{
		Method: req.Method,
		URL:    req.URL.String(),
		Header: headerCopy,
		Body:   body,
	}

	payload, err := entry.Marshal()
	if err != nil {
		return nil, err
	}

	if _, err := t.queue.Enqueue(payload); err != nil {
		return nil, err
	}

	t.queue.Notify()

	req.Body = io.NopCloser(bytes.NewReader(body))
	if body != nil {
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}

	return dummyResponse(req), nil
}

func dummyResponse(req *http.Request) *http.Response {
	return &http.Response{
		Status:        "202 Accepted",
		StatusCode:    http.StatusAccepted,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(nil)),
		ContentLength: 0,
		Request:       req,
	}
}

func readAll(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func cloneDefaultTransport() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return http.DefaultTransport
}
