package logger

import (
	"context"

	"go.opentelemetry.io/otel/sdk/log"
)

// fakeExporter captures records emitted via the SDK exporter pipeline.
type fakeExporter struct {
	records []log.Record
}

func (f *fakeExporter) Export(_ context.Context, records []log.Record) error {
	for _, rec := range records {
		f.records = append(f.records, rec.Clone())
	}
	return nil
}

func (f *fakeExporter) Shutdown(context.Context) error { return nil }

func (f *fakeExporter) ForceFlush(context.Context) error { return nil }
