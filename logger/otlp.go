package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mfahmialkautsar/goo11y/internal/persistenthttp"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type otlpWriter struct {
	config      OTLPConfig
	client      httpClient
	serviceName string
	environment string
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	queue       chan []byte
}

func newOTLPWriter(ctx context.Context, cfg OTLPConfig, serviceName, environment string) (*otlpWriter, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("missing otlp endpoint")
	}

	if cfg.Protocol != "" && cfg.Protocol != "http" && cfg.Protocol != "http/protobuf" {
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}

	endpoint := cfg.Endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if cfg.Insecure {
			endpoint = "http://" + endpoint
		} else {
			endpoint = "https://" + endpoint
		}
	}

	cfg.Endpoint = endpoint

	var client httpClient
	if cfg.UseSpool {
		c, err := persistenthttp.NewClientWithComponent(cfg.QueueDir, cfg.Timeout, "logger")
		if err != nil {
			return nil, fmt.Errorf("persistenthttp test: %w", err)
		}
		client = c
	} else {
		client = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	subCtx, cancel := context.WithCancel(ctx)

	w := &otlpWriter{
		config:      cfg,
		client:      client,
		serviceName: serviceName,
		environment: environment,
		ctx:         subCtx,
		cancel:      cancel,
		queue:       make(chan []byte, 4096),
	}

	w.wg.Add(1)
	go w.run()

	return w, nil
}

func (w *otlpWriter) Write(p []byte) (int, error) {
	copyBuf := make([]byte, len(p))
	copy(copyBuf, p)

	select {
	case <-w.ctx.Done():
		return 0, fmt.Errorf("writer closed")
	default:
	}

	select {
	case w.queue <- copyBuf:
		return len(p), nil
	default:
		// Queue full, drop log to avoid blocking
		return len(p), nil
	}
}

func (w *otlpWriter) Close() error {
	w.cancel()
	w.wg.Wait()
	return nil
}

func (w *otlpWriter) run() {
	defer w.wg.Done()

	var batch [][]byte
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			close(w.queue)
			for p := range w.queue {
				batch = append(batch, p)
			}
			w.flush(batch)
			return
		case p := <-w.queue:
			batch = append(batch, p)
			if len(batch) >= 100 {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *otlpWriter) flush(batch [][]byte) {
	if len(batch) == 0 {
		return
	}

	logs := make([]map[string]interface{}, 0, len(batch))
	for _, p := range batch {
		var l map[string]interface{}
		if err := json.Unmarshal(p, &l); err == nil {
			delete(l, ServiceNameKey)
			delete(l, DeploymentEnvironmentNameKey)
			logs = append(logs, l)
		}
	}

	// minimal OTLP log payload
	payload := map[string]interface{}{
		"resourceLogs": []interface{}{
			map[string]interface{}{
				"resource": map[string]interface{}{
					"attributes": []interface{}{
						map[string]interface{}{
							"key": "service.name",
							"value": map[string]interface{}{
								"stringValue": w.serviceName,
							},
						},
						map[string]interface{}{
							"key": "deployment.environment",
							"value": map[string]interface{}{
								"stringValue": w.environment,
							},
						},
					},
				},
				"scopeLogs": []interface{}{
					map[string]interface{}{
						"logRecords": logs,
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", w.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}

	for k, v := range w.config.headerMap() {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
}
