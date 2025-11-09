package persistenthttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/spool"
)

type Client struct {
	*http.Client
	queue  *spool.Queue
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func NewClient(queueDir string, timeout time.Duration) (*Client, error) {
	return NewClientWithComponent(queueDir, timeout, "")
}

func NewClientWithComponent(queueDir string, timeout time.Duration, component string) (*Client, error) {
	queue, err := spool.NewWithErrorLogger(queueDir, spool.ErrorLoggerFunc(func(err error) {
		if err == nil {
			return
		}
		prefix := "[persistenthttp]"
		if component != "" {
			prefix = fmt.Sprintf("[%s/spool]", component)
		}
		fmt.Fprintf(os.Stderr, "%s %v\n", prefix, err)
	}))
	if err != nil {
		return nil, err
	}

	transport := cloneDefaultTransport()
	workerClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	ctx, cancel := context.WithCancel(context.Background())
	queue.Start(ctx, spool.HTTPHandler(workerClient))

	persistent := &transportWrapper{queue: queue}

	return &Client{
		Client: &http.Client{
			Timeout:   timeout,
			Transport: persistent,
		},
		queue:  queue,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
	return nil
}

type transportWrapper struct {
	queue *spool.Queue
}

func (t *transportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
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
	defer func() {
		_ = body.Close()
	}()
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
