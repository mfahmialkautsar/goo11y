package attrutil

import (
	"errors"
	"math"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestToKeyValues(t *testing.T) {
	attrs := ToKeyValues([]any{
		"string_key", "string_value",
		"int_key", 42,
		"bool_key", true,
	})

	if len(attrs) != 3 {
		t.Fatalf("expected 3 attributes, got %d", len(attrs))
	}

	attrMap := make(map[string]attribute.Value)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value
	}

	if attrMap["string_key"].AsString() != "string_value" {
		t.Errorf("string_key: want string_value, got %v", attrMap["string_key"].AsString())
	}
	if attrMap["int_key"].AsInt64() != 42 {
		t.Errorf("int_key: want 42, got %v", attrMap["int_key"].AsInt64())
	}
	if !attrMap["bool_key"].AsBool() {
		t.Errorf("bool_key: want true, got false")
	}
}

func TestToKeyValuesSkipsInvalidKeys(t *testing.T) {
	attrs := ToKeyValues([]any{
		42, "value", // non-string key
		"", "value", // empty key
		"valid", "ok",
	})

	if len(attrs) != 1 {
		t.Fatalf("expected 1 attribute, got %d", len(attrs))
	}
	if string(attrs[0].Key) != "valid" {
		t.Errorf("expected valid key, got %s", attrs[0].Key)
	}
}

func TestFromValueTypes(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
		check func(*testing.T, attribute.KeyValue)
	}{
		{
			name:  "string",
			key:   "str",
			value: "hello",
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsString() != "hello" {
					t.Errorf("want hello, got %v", kv.Value.AsString())
				}
			},
		},
		{
			name:  "bool",
			key:   "flag",
			value: true,
			check: func(t *testing.T, kv attribute.KeyValue) {
				if !kv.Value.AsBool() {
					t.Error("want true, got false")
				}
			},
		},
		{
			name:  "int",
			key:   "num",
			value: int(42),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 42 {
					t.Errorf("want 42, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "int8",
			key:   "num",
			value: int8(8),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 8 {
					t.Errorf("want 8, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "int16",
			key:   "num",
			value: int16(16),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 16 {
					t.Errorf("want 16, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "int32",
			key:   "num",
			value: int32(32),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 32 {
					t.Errorf("want 32, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "int64",
			key:   "num",
			value: int64(64),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 64 {
					t.Errorf("want 64, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint",
			key:   "num",
			value: uint(42),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 42 {
					t.Errorf("want 42, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint8",
			key:   "num",
			value: uint8(8),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 8 {
					t.Errorf("want 8, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint16",
			key:   "num",
			value: uint16(16),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 16 {
					t.Errorf("want 16, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint32",
			key:   "num",
			value: uint32(32),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 32 {
					t.Errorf("want 32, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint64_small",
			key:   "num",
			value: uint64(64),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsInt64() != 64 {
					t.Errorf("want 64, got %v", kv.Value.AsInt64())
				}
			},
		},
		{
			name:  "uint64_large",
			key:   "num",
			value: uint64(math.MaxUint64),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsString() != "18446744073709551615" {
					t.Errorf("want 18446744073709551615, got %v", kv.Value.AsString())
				}
			},
		},
		{
			name:  "float32",
			key:   "num",
			value: float32(3.14),
			check: func(t *testing.T, kv attribute.KeyValue) {
				f := kv.Value.AsFloat64()
				if f < 3.13 || f > 3.15 {
					t.Errorf("want ~3.14, got %v", f)
				}
			},
		},
		{
			name:  "float64",
			key:   "num",
			value: float64(2.718),
			check: func(t *testing.T, kv attribute.KeyValue) {
				f := kv.Value.AsFloat64()
				if f < 2.71 || f > 2.72 {
					t.Errorf("want ~2.718, got %v", f)
				}
			},
		},
		{
			name:  "bytes",
			key:   "data",
			value: []byte("binary"),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsString() != "binary" {
					t.Errorf("want binary, got %v", kv.Value.AsString())
				}
			},
		},
		{
			name:  "error",
			key:   "err",
			value: errors.New("boom"),
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsString() != "boom" {
					t.Errorf("want boom, got %v", kv.Value.AsString())
				}
			},
		},
		{
			name:  "nil",
			key:   "null",
			value: nil,
			check: func(t *testing.T, kv attribute.KeyValue) {
				if kv.Value.AsString() != "" {
					t.Errorf("want empty string for nil, got %v", kv.Value.AsString())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv, ok := FromValue(tt.key, tt.value)
			if !ok {
				t.Fatal("FromValue returned false")
			}
			if string(kv.Key) != tt.key {
				t.Fatalf("key mismatch: want %s, got %s", tt.key, kv.Key)
			}
			tt.check(t, kv)
		})
	}
}

type stringer struct{}

func (stringer) String() string { return "stringified" }

func TestFromValueStringer(t *testing.T) {
	kv, ok := FromValue("key", stringer{})
	if !ok {
		t.Fatal("FromValue returned false for stringer")
	}
	if kv.Value.AsString() != "stringified" {
		t.Errorf("want stringified, got %v", kv.Value.AsString())
	}
}
