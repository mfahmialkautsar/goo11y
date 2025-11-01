package attrutil

import (
	"fmt"
	"math"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
)

// ToKeyValues converts key-value pairs to OpenTelemetry attributes.
func ToKeyValues(fields []any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok || key == "" {
			continue
		}
		if attr, ok := FromValue(key, fields[i+1]); ok {
			attrs = append(attrs, attr)
		}
	}
	return attrs
}

// FromValue converts an arbitrary value to an OpenTelemetry attribute.
func FromValue(key string, value any) (attribute.KeyValue, bool) {
	switch v := value.(type) {
	case string:
		return attribute.String(key, v), true
	case fmt.Stringer:
		return attribute.String(key, v.String()), true
	case error:
		return attribute.String(key, v.Error()), true
	case bool:
		return attribute.Bool(key, v), true
	case int:
		return attribute.Int64(key, int64(v)), true
	case int8:
		return attribute.Int64(key, int64(v)), true
	case int16:
		return attribute.Int64(key, int64(v)), true
	case int32:
		return attribute.Int64(key, int64(v)), true
	case int64:
		return attribute.Int64(key, v), true
	case uint:
		return fromUnsigned(key, uint64(v))
	case uint8:
		return fromUnsigned(key, uint64(v))
	case uint16:
		return fromUnsigned(key, uint64(v))
	case uint32:
		return fromUnsigned(key, uint64(v))
	case uint64:
		return fromUnsigned(key, v)
	case float32:
		return attribute.Float64(key, float64(v)), true
	case float64:
		return attribute.Float64(key, v), true
	case []byte:
		return attribute.String(key, string(v)), true
	default:
		if value == nil {
			return attribute.String(key, ""), true
		}
		return attribute.String(key, fmt.Sprint(value)), true
	}
}

func fromUnsigned(key string, value uint64) (attribute.KeyValue, bool) {
	if value > math.MaxInt64 {
		return attribute.String(key, strconv.FormatUint(value, 10)), true
	}
	return attribute.Int64(key, int64(value)), true
}
