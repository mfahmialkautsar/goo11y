package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mfahmialkautsar/go-o11y/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"go.opentelemetry.io/otel/trace"
)

type Logger interface {
	Debug(ctx context.Context, msg string, fields ...any)
	Info(ctx context.Context, msg string, fields ...any)
	Warn(ctx context.Context, msg string, fields ...any)
	Error(ctx context.Context, err error, msg string, fields ...any)
	Fatal(ctx context.Context, err error, msg string, fields ...any)
}

type zerologLogger struct {
	logger      zerolog.Logger
	lokiWriter  *lokiWriter
	multiWriter io.Writer
}

func NewWithConfig(cfg config.Logger) Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixNano
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	if cfg.Environment != "production" {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339Nano,
		}
		cfg.Writers = append(cfg.Writers, consoleWriter)
	}

	var loki *lokiWriter
	if cfg.LokiURL != "" {
		loki = newLokiWriter(cfg.LokiURL, cfg.ServiceName, cfg.LokiUser, cfg.LokiPass)
		cfg.Writers = append(cfg.Writers, loki)
	}

	multiWriter := io.MultiWriter(cfg.Writers...)

	zlog := zerolog.New(multiWriter).With().
		Timestamp().
		Logger()

	zlevel, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		zlevel = zerolog.InfoLevel
	}
	zlog = zlog.Level(zlevel)

	return &zerologLogger{
		logger:      zlog,
		lokiWriter:  loki,
		multiWriter: multiWriter,
	}
}

func (l *zerologLogger) Debug(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Debug().Caller(1), msg, fields...)
}

func (l *zerologLogger) Info(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Info().Caller(1), msg, fields...)
}

func (l *zerologLogger) Warn(ctx context.Context, msg string, fields ...any) {
	l.log(ctx, l.logger.Warn().Caller(1), msg, fields...)
}

func (l *zerologLogger) Error(ctx context.Context, err error, msg string, fields ...any) {
	event := l.logger.Error().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.log(ctx, event, msg, fields...)
}

func (l *zerologLogger) Fatal(ctx context.Context, err error, msg string, fields ...any) {
	event := l.logger.Fatal().Caller(1)
	if err != nil {
		event = event.Stack().Err(err)
	}
	l.log(ctx, event, msg, fields...)
}

func (l *zerologLogger) log(ctx context.Context, event *zerolog.Event, msg string, fields ...any) {
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			key, ok := fields[i].(string)
			if !ok {
				continue
			}
			if err, isErr := fields[i+1].(error); isErr {
				event = event.Stack().Err(err)
				continue
			}
			event = event.Interface(key, fields[i+1])
		}
	}

	if ctx != nil {
		span := trace.SpanFromContext(ctx)
		if span.SpanContext().IsValid() {
			traceID := span.SpanContext().TraceID().String()
			spanID := span.SpanContext().SpanID().String()
			event = event.
				Str("trace_id", traceID).
				Str("span_id", spanID)
		}
	}

	event.Msg(msg)
}

type lokiWriter struct {
	url         string
	serviceName string
	username    string
	password    string
	client      *http.Client
}

type lokiPayload struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

func newLokiWriter(url, serviceName, username, password string) *lokiWriter {
	return &lokiWriter{
		url:         url,
		serviceName: serviceName,
		username:    username,
		password:    password,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (lw *lokiWriter) Write(p []byte) (n int, err error) {
	go lw.send(p)
	return len(p), nil
}

func (lw *lokiWriter) send(logEntry []byte) {
	timestamp := time.Now().UnixNano()

	var entryMap map[string]any
	if err := json.Unmarshal(logEntry, &entryMap); err == nil {
		if tsVal, ok := entryMap["time"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, tsVal); err == nil {
				timestamp = parsed.UnixNano()
			}
		}
	}

	payload := lokiPayload{
		Streams: []lokiStream{
			{
				Stream: map[string]string{
					"service_name": lw.serviceName,
				},
				Values: [][]string{
					{fmt.Sprintf("%d", timestamp), string(logEntry)},
				},
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lw.url, bytes.NewReader(jsonPayload))
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
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("lokiWriter: failed to close response body: %v\n", cerr)
		}
	}()
}
