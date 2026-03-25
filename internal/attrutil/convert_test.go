package attrutil

import (
	"errors"
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

func checkInt64Val(want int64) func(*testing.T, attribute.KeyValue) {
	return func(t *testing.T, kv attribute.KeyValue) {
		if kv.Value.AsInt64() != want {
			t.Errorf("want %d, got %v", want, kv.Value.AsInt64())
		}
	}
}

func checkStringVal(want string) func(*testing.T, attribute.KeyValue) {
	return func(t *testing.T, kv attribute.KeyValue) {
		if kv.Value.AsString() != want {
			t.Errorf("want %s, got %v", want, kv.Value.AsString())
		}
	}
}

func checkBoolVal(want bool) func(*testing.T, attribute.KeyValue) {
	return func(t *testing.T, kv attribute.KeyValue) {
		if kv.Value.AsBool() != want {
			t.Errorf("want %v, got %v", want, kv.Value.AsBool())
		}
	}
}

func checkFloat64Range(min, max float64) func(*testing.T, attribute.KeyValue) {
	return func(t *testing.T, kv attribute.KeyValue) {
		f := kv.Value.AsFloat64()
		if f < min || f > max {
			t.Errorf("want between %v and %v, got %v", min, max, f)
		}
	}
}

func TestFromValueTypes(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
		check func(*testing.T, attribute.KeyValue)
	}{
		{name: "string", key: "str", value: "hello", check: checkStringVal("hello")},
		{name: "bool", key: "flag", value: true, check: checkBoolVal(true)},
		{name: "int", key: "num", value: int(42), check: checkInt64Val(42)},
		{name: "int8", key: "num", value: int8(8), check: checkInt64Val(8)},
		{name: "int16", key: "num", value: int16(16), check: checkInt64Val(16)},
		{name: "int32", key: "num", value: int32(32), check: checkInt64Val(32)},
		{name: "int64", key: "num", value: int64(64), check: checkInt64Val(64)},
		{name: "uint", key: "num", value: uint(42), check: checkInt64Val(42)},
		{name: "uint8", key: "num", value: uint8(8), check: checkInt64Val(8)},
		{name: "uint16", key: "num", value: uint16(16), check: checkInt64Val(16)},
		{name: "uint32", key: "num", value: uint32(32), check: checkInt64Val(32)},
		{name: "uint64_small", key: "num", value: uint64(64), check: checkInt64Val(64)},
		{name: "uint64_large", key: "num", value: uint64(18446744073709551615), check: checkStringVal("18446744073709551615")},
		{name: "float32", key: "num", value: float32(3.14), check: checkFloat64Range(3.13, 3.15)},
		{name: "float64", key: "num", value: float64(2.718), check: checkFloat64Range(2.71, 2.72)},
		{name: "bytes", key: "data", value: []byte("binary"), check: checkStringVal("binary")},
		{name: "error", key: "err", value: errors.New("boom"), check: checkStringVal("boom")},
		{name: "nil", key: "null", value: nil, check: checkStringVal("")},
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
