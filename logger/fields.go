package logger

import (
	"github.com/mfahmialkautsar/goo11y/internal/attrutil"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
)

func applyFields(event *zerolog.Event, fields []any) {
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		value := fields[i+1]
		switch v := value.(type) {
		case error:
			event.Stack().Err(v)
		default:
			event.Interface(key, v)
		}
	}
}

func attributesFromFields(fields []any) []attribute.KeyValue {
	return attrutil.ToKeyValues(fields)
}
