package contract

import (
	"encoding/json"
	"testing"
)

// TestConvertAnyValueJsonHappyPaths pins the three accepted outbound json input
// forms across both json shapes: map[string]any / []any are SDK-marshaled, and
// json.RawMessage is validated then returned byte-exact (trim only).
func TestConvertAnyValueJsonHappyPaths(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		wantValue string
		wantType  DataType
	}{
		{
			name:      "map to jsonObject marshaled",
			input:     map[string]any{"k": float64(1)},
			wantValue: `{"k":1}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "empty map to jsonObject marshaled",
			input:     map[string]any{},
			wantValue: `{}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "slice to jsonArray marshaled",
			input:     []any{float64(1), float64(2), float64(3)},
			wantValue: `[1,2,3]`,
			wantType:  TypeJsonArray,
		},
		{
			name:      "empty slice to jsonArray marshaled",
			input:     []any{},
			wantValue: `[]`,
			wantType:  TypeJsonArray,
		},
		{
			// Big integer beyond 2^53 plus preserved key order and inner
			// whitespace: verbatim pass-through, no re-marshal/compact.
			name:      "RawMessage object verbatim preserves big int and order",
			input:     json.RawMessage(`{"id": 9223372036854775807, "b": 1, "a": 2}`),
			wantValue: `{"id": 9223372036854775807, "b": 1, "a": 2}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "RawMessage object trims surrounding whitespace only",
			input:     json.RawMessage("  \n\t{\"k\":1}\n  "),
			wantValue: `{"k":1}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "RawMessage array verbatim preserves big int",
			input:     json.RawMessage(`[9223372036854775807, 2, 3]`),
			wantValue: `[9223372036854775807, 2, 3]`,
			wantType:  TypeJsonArray,
		},
		{
			name:      "RawMessage empty object",
			input:     json.RawMessage(`{}`),
			wantValue: `{}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "RawMessage empty array",
			input:     json.RawMessage(`[]`),
			wantValue: `[]`,
			wantType:  TypeJsonArray,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value, dataType, err := ConvertAnyValue(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dataType != tc.wantType {
				t.Fatalf("expected type %q, got %q", tc.wantType, dataType)
			}
			if value != tc.wantValue {
				t.Fatalf("expected value %q, got %q", tc.wantValue, value)
			}
		})
	}
}

// TestConvertAnyValueRejectsInvalidRawMessage pins the strict-validation rejects
// on the json.RawMessage path (shape, null, scalar, malformed).
func TestConvertAnyValueRejectsInvalidRawMessage(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
	}{
		{"null literal", json.RawMessage("null")},
		{"scalar number", json.RawMessage("42")},
		{"scalar string", json.RawMessage(`"hello"`)},
		{"malformed object", json.RawMessage(`{"broken":}`)},
		{"malformed array", json.RawMessage(`[1,2,`)},
		{"whitespace only", json.RawMessage("   ")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value, dataType, err := ConvertAnyValue(tc.input)
			if err == nil {
				t.Fatalf("expected error for %s, got value %q type %q", tc.name, value, dataType)
			}
			if value != "" {
				t.Fatalf("expected empty value on error, got %q", value)
			}
			if dataType != TypeUndefined {
				t.Fatalf("expected undefined type on error, got %q", dataType)
			}
		})
	}
}

// TestNewPortFieldDataWithAnyJsonHappyPaths proves the full outbound path
// (ConvertAnyValue + CanConvertTo same-type gate + ConvertTo verbatim) produces
// the expected PortFieldData for a matching json field.
func TestNewPortFieldDataWithAnyJsonHappyPaths(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		destType  DataType
		wantValue string
		wantType  DataType
	}{
		{
			name:      "map into jsonObject field",
			input:     map[string]any{"k": float64(1)},
			destType:  TypeJsonObject,
			wantValue: `{"k":1}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "slice into jsonArray field",
			input:     []any{float64(1), float64(2)},
			destType:  TypeJsonArray,
			wantValue: `[1,2]`,
			wantType:  TypeJsonArray,
		},
		{
			name:      "RawMessage object into jsonObject field verbatim",
			input:     json.RawMessage(`{"id":9223372036854775807}`),
			destType:  TypeJsonObject,
			wantValue: `{"id":9223372036854775807}`,
			wantType:  TypeJsonObject,
		},
		{
			name:      "RawMessage array into jsonArray field verbatim",
			input:     json.RawMessage(`[9223372036854775807]`),
			destType:  TypeJsonArray,
			wantValue: `[9223372036854775807]`,
			wantType:  TypeJsonArray,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			field, err := NewPortFieldDataWithAny(tc.input, tc.destType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if field.Type != tc.wantType {
				t.Fatalf("expected type %q, got %q", tc.wantType, field.Type)
			}
			if field.Value != tc.wantValue {
				t.Fatalf("expected value %q, got %q", tc.wantValue, field.Value)
			}
		})
	}
}

// TestNewPortFieldDataWithAnyJsonShapeMismatchRejected pins that shape/type
// mismatches are rejected by the EXISTING CanConvertTo same-type gate (map into
// a jsonArray field, RawMessage object into a jsonArray field), plus the strict
// RawMessage validation rejects null / scalar / malformed regardless of field.
func TestNewPortFieldDataWithAnyJsonShapeMismatchRejected(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		destType DataType
	}{
		{"map into jsonArray field", map[string]any{"k": 1}, TypeJsonArray},
		{"slice into jsonObject field", []any{1, 2}, TypeJsonObject},
		{"RawMessage object into jsonArray field", json.RawMessage(`{"a":1}`), TypeJsonArray},
		{"RawMessage array into jsonObject field", json.RawMessage(`[1,2]`), TypeJsonObject},
		{"RawMessage null into jsonObject field", json.RawMessage("null"), TypeJsonObject},
		{"RawMessage scalar into jsonObject field", json.RawMessage("42"), TypeJsonObject},
		{"RawMessage malformed into jsonObject field", json.RawMessage(`{"broken":}`), TypeJsonObject},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			field, err := NewPortFieldDataWithAny(tc.input, tc.destType)
			if err == nil {
				t.Fatalf("expected error, got field %#v", field)
			}
			if field != nil {
				t.Fatalf("expected nil field on error, got %#v", field)
			}
		})
	}
}

// TestGetDataTypeJsonForms pins detection of the three outbound input forms and
// the RawMessage shape-sniff (including the undefined fallbacks).
func TestGetDataTypeJsonForms(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  DataType
	}{
		{"map is jsonObject", map[string]any{"k": 1}, TypeJsonObject},
		{"slice is jsonArray", []any{1}, TypeJsonArray},
		{"RawMessage object sniff", json.RawMessage(`{"a":1}`), TypeJsonObject},
		{"RawMessage array sniff", json.RawMessage(`[1]`), TypeJsonArray},
		{"RawMessage leading whitespace object", json.RawMessage("  {"), TypeJsonObject},
		{"RawMessage scalar is undefined", json.RawMessage("42"), TypeUndefined},
		{"RawMessage empty is undefined", json.RawMessage(""), TypeUndefined},
		{"plain []byte stays raw", []byte("hi"), TypeRaw},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GetDataType(tc.input); got != tc.want {
				t.Fatalf("GetDataType(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestConvertAnyValueTypedNilJsonStaysUnsupported pins that typed-nil map / slice
// / json.RawMessage keep erroring via the existing isNilAnyValue pre-filter and
// are NEVER silently marshaled to "null". Publish pre-filters nils anyway.
func TestConvertAnyValueTypedNilJsonStaysUnsupported(t *testing.T) {
	var nilMap map[string]any
	var nilSlice []any
	var nilRaw json.RawMessage

	for _, tc := range []struct {
		name  string
		input any
	}{
		{"typed nil map", nilMap},
		{"typed nil slice", nilSlice},
		{"typed nil RawMessage", nilRaw},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, dataType, err := ConvertAnyValue(tc.input)
			if err == nil {
				t.Fatalf("expected error for %s, got value %q", tc.name, value)
			}
			if value != "" {
				t.Fatalf("expected empty value, got %q", value)
			}
			if dataType != TypeUndefined {
				t.Fatalf("expected undefined type, got %q", dataType)
			}
			if got := err.Error(); got != "nil value is not supported for conversion" {
				t.Fatalf("unexpected error: %s", got)
			}
		})
	}
}
