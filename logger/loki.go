package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type lokiWriter struct {
	url      string
	username string
	password string
	labels   map[string]string
	timeout  time.Duration
	client   *http.Client
}

func newLokiWriter(cfg LokiConfig, serviceName string) io.Writer {
	labels := make(map[string]string, len(cfg.Labels)+1)
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	if serviceName != "" {
		labels["service_name"] = serviceName
	} else if _, ok := labels["service_name"]; !ok {
		labels["service_name"] = "unknown"
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultLokiTimeout
	}

	return &lokiWriter{
		url:      cfg.URL,
		username: cfg.Username,
		password: cfg.Password,
		labels:   labels,
		timeout:  timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (lw *lokiWriter) Write(p []byte) (int, error) {
	if lw == nil || lw.url == "" {
		return len(p), nil
	}

	entry := append([]byte(nil), p...)
	go lw.send(entry)
	return len(p), nil
}

func (lw *lokiWriter) send(payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), lw.timeout)
	defer cancel()

	body, err := lw.buildPayload(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lw.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if lw.username != "" && lw.password != "" {
		req.SetBasicAuth(lw.username, lw.password)
	}

	resp, err := lw.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
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
