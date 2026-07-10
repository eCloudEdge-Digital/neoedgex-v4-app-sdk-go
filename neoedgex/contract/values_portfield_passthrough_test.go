package contract

import (
	"testing"
)

// TestNewPortFieldDataWithAnyPortFieldSameTypeVerbatim pins the same-type
// pass-through path: a pre-built PortFieldData handed to the constructor is
// validated then carried byte-exact (no parse/re-serialize round trip), so json
// big-int precision, key order, and base64 raw payloads survive untouched.
func TestNewPortFieldDataWithAnyPortFieldSameTypeVerbatim(t *testing.T) {
	tests := []struct {
		name     string
		input    PortFieldData
		destType DataType
	}{
		{
			name:     "jsonObject big int verbatim",
			input:    PortFieldData{Type: TypeJsonObject, Value: `{"id": 9223372036854775807, "b": 1, "a": 2}`},
			destType: TypeJsonObject,
		},
		{
			name:     "jsonArray big int verbatim",
			input:    PortFieldData{Type: TypeJsonArray, Value: `[9223372036854775807, 2, 3]`},
			destType: TypeJsonArray,
		},
		{
			name:     "int64 scalar verbatim",
			input:    PortFieldData{Type: TypeInt64, Value: "9223372036854775807"},
			destType: TypeInt64,
		},
		{
			name:     "raw base64 verbatim",
			input:    PortFieldData{Type: TypeRaw, Value: "aGVsbG8gd29ybGQ="},
			destType: TypeRaw,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			field, err := NewPortFieldDataWithAny(tc.input, tc.destType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if field.Type != tc.input.Type {
				t.Fatalf("expected type %q, got %q", tc.input.Type, field.Type)
			}
			if field.Value != tc.input.Value {
				t.Fatalf("expected value byte-identical %q, got %q", tc.input.Value, field.Value)
			}
		})
	}
}

// TestNewPortFieldDataWithAnyPortFieldPointer pins the *PortFieldData variant is
// dereferenced and follows the same verbatim path as the value form.
func TestNewPortFieldDataWithAnyPortFieldPointer(t *testing.T) {
	input := &PortFieldData{Type: TypeJsonObject, Value: `{"id":9223372036854775807}`}

	field, err := NewPortFieldDataWithAny(input, TypeJsonObject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Type != TypeJsonObject {
		t.Fatalf("expected type jsonObject, got %q", field.Type)
	}
	if field.Value != input.Value {
		t.Fatalf("expected value byte-identical %q, got %q", input.Value, field.Value)
	}
}

// TestNewPortFieldDataWithAnyPortFieldNilPointer pins that a nil *PortFieldData
// keeps the existing isNilAnyValue rejection (not the pass-through path).
func TestNewPortFieldDataWithAnyPortFieldNilPointer(t *testing.T) {
	var nilPtr *PortFieldData

	field, err := NewPortFieldDataWithAny(nilPtr, TypeJsonObject)
	if err == nil {
		t.Fatalf("expected error for nil *PortFieldData, got field %#v", field)
	}
	if field != nil {
		t.Fatalf("expected nil field on error, got %#v", field)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}

// TestNewPortFieldDataWithAnyPortFieldUndefinedIsEmptyField pins Q3: a
// TypeUndefined PortFieldData (e.g. NewEmptyField) is treated like nil and yields
// an empty field with no error.
func TestNewPortFieldDataWithAnyPortFieldUndefinedIsEmptyField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input any
	}{
		{"value form empty", PortFieldData{Type: TypeUndefined}},
		{"NewEmptyField product", *NewEmptyField()},
		{"pointer form empty", &PortFieldData{Type: TypeUndefined}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field, err := NewPortFieldDataWithAny(tc.input, TypeJsonObject)
			if err != nil {
				t.Fatalf("expected no error for TypeUndefined, got %v", err)
			}
			if field == nil {
				t.Fatal("expected empty field, got nil")
			}
			if field.Type != TypeUndefined || field.Value != "" {
				t.Fatalf("expected empty field, got type %q value %q", field.Type, field.Value)
			}
		})
	}
}

// TestNewPortFieldDataWithAnyPortFieldCrossType pins Q1 cross-type: a pre-built
// field into a different dest type is converted via the existing CanConvertTo
// matrix (allowed for scalar widening, denied for json cross-type).
func TestNewPortFieldDataWithAnyPortFieldCrossType(t *testing.T) {
	tests := []struct {
		name      string
		input     PortFieldData
		destType  DataType
		wantType  DataType
		wantValue string
	}{
		{
			name:      "int32 into double",
			input:     PortFieldData{Type: TypeInt32, Value: "42"},
			destType:  TypeDouble,
			wantType:  TypeDouble,
			wantValue: "4.2e+01",
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

// TestNewPortFieldDataWithAnyPortFieldCrossTypeDenied pins that json cross-type
// (and any non-convertible pair) is denied by the existing matrix.
func TestNewPortFieldDataWithAnyPortFieldCrossTypeDenied(t *testing.T) {
	tests := []struct {
		name     string
		input    PortFieldData
		destType DataType
	}{
		{"jsonObject into jsonArray", PortFieldData{Type: TypeJsonObject, Value: `{"a":1}`}, TypeJsonArray},
		{"jsonObject into int32", PortFieldData{Type: TypeJsonObject, Value: `{"a":1}`}, TypeInt32},
		{"jsonArray into jsonObject", PortFieldData{Type: TypeJsonArray, Value: `[1]`}, TypeJsonObject},
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

// TestNewPortFieldDataWithAnyPortFieldRejectsInvalidValue pins Q2: the embedded
// value is parse-validated against its own type before acceptance. A malformed
// value, a shape-mismatched json value, or a non-numeric int value is rejected.
func TestNewPortFieldDataWithAnyPortFieldRejectsInvalidValue(t *testing.T) {
	tests := []struct {
		name     string
		input    PortFieldData
		destType DataType
	}{
		{"jsonObject not json", PortFieldData{Type: TypeJsonObject, Value: "not json"}, TypeJsonObject},
		{"jsonObject array-shaped", PortFieldData{Type: TypeJsonObject, Value: "[1]"}, TypeJsonObject},
		{"int32 non-numeric", PortFieldData{Type: TypeInt32, Value: "abc"}, TypeInt32},
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
