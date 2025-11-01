package logger

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mfahmialkautsar/goo11y/internal/spool"
)

type otlpWriter struct {
	endpoint      string
	headers       map[string][]string
	queue         *spool.Queue
	client        *http.Client
	resourceAttrs []otlpKeyValue
	async         bool
}

func newOTLPWriter(cfg OTLPConfig, serviceName, environment string) (io.Writer, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("otlp: endpoint is required")
	}

	if !strings.Contains(endpoint, "://") {
		scheme := "https://"
		if cfg.Insecure {
			scheme = "http://"
		}
		endpoint = scheme + endpoint
	}

	var queue *spool.Queue
	var err error
	if cfg.UseSpool {
		queue, err = spool.NewWithErrorLogger(cfg.QueueDir, spool.ErrorLoggerFunc(logOTLPSpoolError))
		if err != nil {
			return nil, err
		}
	}

	transport := configureTransport(cfg.Insecure)
	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}

	if cfg.UseSpool {
		queue.Start(context.Background(), spool.HTTPHandler(client))
	}

	headers := cfg.headerMap()

	resourceAttrs := make([]otlpKeyValue, 0, 4)
	if serviceName != "" {
		resourceAttrs = append(resourceAttrs,
			stringKeyValue("service.name", serviceName),
			stringKeyValue("service_name", serviceName),
		)
	}
	if environment != "" {
		resourceAttrs = append(resourceAttrs,
			stringKeyValue("deployment.environment", environment),
			stringKeyValue("environment", environment),
		)
	}

	return &otlpWriter{
		endpoint:      endpoint,
		headers:       headers,
		queue:         queue,
		client:        client,
		resourceAttrs: resourceAttrs,
		async:         cfg.Async,
	}, nil
}

func (ow *otlpWriter) Write(p []byte) (int, error) {
	if ow == nil {
		return len(p), nil
	}
	payload, err := ow.buildPayload(p)
	if err != nil {
		return 0, err
	}

	if ow.queue != nil {
		request := &spool.HTTPRequest{
			Method: http.MethodPost,
			URL:    ow.endpoint,
			Header: copyHeaders(ow.headers),
			Body:   payload,
		}

		envelope, err := request.Marshal()
		if err != nil {
			return 0, err
		}

		if _, err := ow.queue.Enqueue(envelope); err != nil {
			return 0, err
		}
		ow.queue.Notify()
		return len(p), nil
	}

	if ow.async {
		go ow.sendSync(payload)
		return len(p), nil
	}

	return len(p), ow.sendSync(payload)
}

// logOTLPSpoolError writes spool warnings to stderr for visibility during development and tests.
func logOTLPSpoolError(err error) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "[otlp-spool] %v\n", err)
}

func (ow *otlpWriter) sendSync(payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, ow.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("otlp: create request: %w", err)
	}

	for key, values := range ow.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := ow.client.Do(req)
	if err != nil {
		return fmt.Errorf("otlp: send request: %w", err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("otlp: remote status %d", resp.StatusCode)
	}

	return nil
}

func (ow *otlpWriter) buildPayload(entry []byte) ([]byte, error) {
	now := time.Now()

	record := otlpLogRecord{
		TimeUnixNano:         strconv.FormatInt(now.UnixNano(), 10),
		ObservedTimeUnixNano: strconv.FormatInt(now.UnixNano(), 10),
		SeverityText:         "INFO",
		SeverityNumber:       severityNumber("INFO"),
		Body:                 otlpValue{StringValue: string(entry)},
	}

	attributes := make([]otlpKeyValue, 0)
	var parsed map[string]any
	if err := json.Unmarshal(entry, &parsed); err == nil {
		if ts, ok := parsed["time"].(string); ok {
			if parsedTime, perr := time.Parse(time.RFC3339Nano, ts); perr == nil {
				record.TimeUnixNano = strconv.FormatInt(parsedTime.UnixNano(), 10)
			}
		}
		if lvl, ok := parsed["level"].(string); ok {
			record.SeverityText = strings.ToUpper(lvl)
			record.SeverityNumber = severityNumber(record.SeverityText)
		}
		if traceID, ok := parsed[traceIDField].(string); ok {
			record.TraceID = traceID
		}
		if spanID, ok := parsed[spanIDField].(string); ok {
			record.SpanID = spanID
		}

		for key, value := range parsed {
			if key == "time" || key == "level" || key == "message" || key == traceIDField || key == spanIDField || key == "service_name" {
				continue
			}
			if attr, ok := anyToAttribute(key, value); ok {
				attributes = append(attributes, attr)
			}
		}
	}

	if len(attributes) > 0 {
		record.Attributes = attributes
	}

	payload := otlpExport{
		ResourceLogs: []otlpResourceLogs{
			{
				Resource: otlpResource{Attributes: duplicateAttributes(ow.resourceAttrs)},
				ScopeLogs: []otlpScopeLogs{
					{
						Scope:      otlpScope{Name: "github.com/mfahmialkautsar/goo11y/logger"},
						LogRecords: []otlpLogRecord{record},
					},
				},
			},
		},
	}

	return json.Marshal(payload)
}

func configureTransport(insecure bool) http.RoundTripper {
	transport := cloneDefaultTransport()
	if !insecure {
		return transport
	}
	if base, ok := transport.(*http.Transport); ok {
		clone := base.Clone()
		if clone.TLSClientConfig == nil {
			clone.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		} else {
			cfg := clone.TLSClientConfig.Clone()
			cfg.InsecureSkipVerify = true
			clone.TLSClientConfig = cfg
		}
		return clone
	}
	return transport
}

func cloneDefaultTransport() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return http.DefaultTransport
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

type otlpExport struct {
	ResourceLogs []otlpResourceLogs `json:"resourceLogs"`
}

type otlpResourceLogs struct {
	Resource  otlpResource    `json:"resource"`
	ScopeLogs []otlpScopeLogs `json:"scopeLogs"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpScopeLogs struct {
	Scope      otlpScope       `json:"scope,omitempty"`
	LogRecords []otlpLogRecord `json:"logRecords"`
}

type otlpScope struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type otlpLogRecord struct {
	TimeUnixNano         string         `json:"timeUnixNano,omitempty"`
	ObservedTimeUnixNano string         `json:"observedTimeUnixNano,omitempty"`
	SeverityText         string         `json:"severityText,omitempty"`
	SeverityNumber       int            `json:"severityNumber,omitempty"`
	Body                 otlpValue      `json:"body"`
	Attributes           []otlpKeyValue `json:"attributes,omitempty"`
	TraceID              string         `json:"traceId,omitempty"`
	SpanID               string         `json:"spanId,omitempty"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue string  `json:"stringValue,omitempty"`
	BoolValue   bool    `json:"boolValue,omitempty"`
	IntValue    string  `json:"intValue,omitempty"`
	DoubleValue float64 `json:"doubleValue,omitempty"`
}

func anyToAttribute(key string, value any) (otlpKeyValue, bool) {
	switch v := value.(type) {
	case string:
		return otlpKeyValue{Key: key, Value: otlpValue{StringValue: v}}, true
	case bool:
		return otlpKeyValue{Key: key, Value: otlpValue{BoolValue: v}}, true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return otlpKeyValue{Key: key, Value: otlpValue{StringValue: fmt.Sprintf("%v", v)}}, true
		}
		if math.Trunc(v) == v {
			return otlpKeyValue{Key: key, Value: otlpValue{IntValue: strconv.FormatInt(int64(v), 10)}}, true
		}
		return otlpKeyValue{Key: key, Value: otlpValue{DoubleValue: v}}, true
	case nil:
		return otlpKeyValue{}, false
	case map[string]any, []any:
		marshaled, err := json.Marshal(v)
		if err != nil {
			return otlpKeyValue{Key: key, Value: otlpValue{StringValue: fmt.Sprintf("%v", v)}}, true
		}
		return otlpKeyValue{Key: key, Value: otlpValue{StringValue: string(marshaled)}}, true
	default:
		return otlpKeyValue{Key: key, Value: otlpValue{StringValue: fmt.Sprint(v)}}, true
	}
}

func duplicateAttributes(attrs []otlpKeyValue) []otlpKeyValue {
	if len(attrs) == 0 {
		return nil
	}
	dup := make([]otlpKeyValue, len(attrs))
	copy(dup, attrs)
	return dup
}

func stringKeyValue(key, value string) otlpKeyValue {
	return otlpKeyValue{Key: key, Value: otlpValue{StringValue: value}}
}

func severityNumber(level string) int {
	switch strings.ToUpper(level) {
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO":
		return 9
	case "WARN", "WARNING":
		return 13
	case "ERROR":
		return 17
	case "FATAL", "PANIC":
		return 21
	default:
		return 9
	}
}
