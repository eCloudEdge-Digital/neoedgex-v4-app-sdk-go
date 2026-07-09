package contract

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestConvertValueByTypeRawReturnsRawMessageForJsonObject(t *testing.T) {
	// A large integer that would lose precision through float64 parsing.
	raw := `{"id":9223372036854775807,"name":"x"}`

	value, err := ConvertValueByTypeRaw(raw, TypeJsonObject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, ok := value.(json.RawMessage)
	if !ok {
		t.Fatalf("expected json.RawMessage, got %T", value)
	}
	if !bytes.Equal(msg, []byte(raw)) {
		t.Fatalf("expected raw bytes %q, got %q", raw, string(msg))
	}
}

func TestConvertValueByTypeRawReturnsRawMessageForJsonArray(t *testing.T) {
	raw := `[9223372036854775807,2,3]`

	value, err := ConvertValueByTypeRaw(raw, TypeJsonArray)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, ok := value.(json.RawMessage)
	if !ok {
		t.Fatalf("expected json.RawMessage, got %T", value)
	}
	if !bytes.Equal(msg, []byte(raw)) {
		t.Fatalf("expected raw bytes %q, got %q", raw, string(msg))
	}
}

func TestConvertValueByTypeRawRejectsNullForJsonTypes(t *testing.T) {
	for _, dt := range []DataType{TypeJsonObject, TypeJsonArray} {
		if value, err := ConvertValueByTypeRaw("null", dt); err == nil {
			t.Fatalf("expected error for null %s, got value %#v", dt, value)
		}
	}
}

func TestConvertValueByTypeRawRejectsMalformedJson(t *testing.T) {
	if value, err := ConvertValueByTypeRaw(`{"broken":}`, TypeJsonObject); err == nil {
		t.Fatalf("expected error for malformed jsonObject, got value %#v", value)
	}
	if value, err := ConvertValueByTypeRaw(`[1,2,`, TypeJsonArray); err == nil {
		t.Fatalf("expected error for malformed jsonArray, got value %#v", value)
	}
}

func TestConvertValueByTypeRawRejectsWrongShape(t *testing.T) {
	// An array supplied where an object is expected, and vice versa.
	if value, err := ConvertValueByTypeRaw(`[1,2,3]`, TypeJsonObject); err == nil {
		t.Fatalf("expected error for array-as-jsonObject, got value %#v", value)
	}
	if value, err := ConvertValueByTypeRaw(`{"a":1}`, TypeJsonArray); err == nil {
		t.Fatalf("expected error for object-as-jsonArray, got value %#v", value)
	}
}

func TestConvertValueByTypeRawMatchesConvertValueByTypeForNonJson(t *testing.T) {
	value, err := ConvertValueByTypeRaw("42", TypeInt64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := value.(int64); !ok || got != 42 {
		t.Fatalf("expected int64(42) for non-json type, got %#v", value)
	}
}

// TestRawMessageMarshalsVerbatimInsidePayload documents the pass-through
// property the forwarders rely on: a json.RawMessage stored in a
// map[string]any serializes as nested json, not as an escaped string, and
// preserves the exact original bytes (including a >2^53 integer).
func TestRawMessageMarshalsVerbatimInsidePayload(t *testing.T) {
	raw := `{"id":9223372036854775807,"nested":{"k":[1,2]}}`

	value, err := ConvertValueByTypeRaw(raw, TypeJsonObject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := map[string]any{"field": value}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	expected := `{"field":{"id":9223372036854775807,"nested":{"k":[1,2]}}}`
	if string(out) != expected {
		t.Fatalf("expected verbatim nested json %q, got %q", expected, string(out))
	}
}
