package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/spool"
)

type lokiWriter struct {
	endpoint string
	labels   map[string]string
	headers  map[string][]string
	queue    *spool.Queue
}

func newLokiWriter(cfg LokiConfig, serviceName string) (io.Writer, error) {
	labels := make(map[string]string, len(cfg.Labels)+1)
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	if serviceName != "" {
		labels["service_name"] = serviceName
	} else if _, ok := labels["service_name"]; !ok {
		labels["service_name"] = "unknown"
	}

	trimmed := strings.TrimRight(cfg.URL, "/")
	if trimmed == "" {
		return nil, fmt.Errorf("loki: url is required")
	}
	endpoint := trimmed
	if !strings.HasSuffix(trimmed, "/loki/api/v1/push") {
		endpoint = trimmed + "/loki/api/v1/push"
	}

	queue, err := spool.New(cfg.QueueDir)
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultLokiTimeout
	}

	transport := cloneDefaultTransport()
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	queue.Start(context.Background(), spool.HTTPHandler(client))

	headers := map[string][]string{
		"Content-Type": {"application/json"},
	}
	if creds := cfg.Credentials.HeaderMap(); len(creds) > 0 {
		for key, value := range creds {
			if key == "" || value == "" {
				continue
			}
			headers[key] = []string{value}
		}
	}

	return &lokiWriter{
		endpoint: endpoint,
		labels:   labels,
		headers:  headers,
		queue:    queue,
	}, nil
}

func (lw *lokiWriter) Write(p []byte) (int, error) {
	if lw == nil {
		return len(p), nil
	}
	body, err := lw.buildPayload(p)
	if err != nil {
		return 0, err
	}

	req := &spool.HTTPRequest{
		Method: http.MethodPost,
		URL:    lw.endpoint,
		Header: copyHeaders(lw.headers),
		Body:   body,
	}

	payload, err := req.Marshal()
	if err != nil {
		return 0, err
	}

	if _, err := lw.queue.Enqueue(payload); err != nil {
		return 0, err
	}
	lw.queue.Notify()
	return len(p), nil
}

func (lw *lokiWriter) buildPayload(entry []byte) ([]byte, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano())

	payload := struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"streams"`
	}{
		Streams: []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		}{
			{
				Stream: lw.labels,
				Values: [][]string{{timestamp, string(entry)}},
			},
		},
	}

	return json.Marshal(payload)
}

func copyHeaders(src map[string][]string) map[string][]string {
	dup := make(map[string][]string, len(src))
	for key, values := range src {
		vv := make([]string, len(values))
		copy(vv, values)
		dup[key] = vv
	}
	return dup
}

func cloneDefaultTransport() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return http.DefaultTransport
}
