package contract

import "testing"

func TestJsonTypesAreSupported(t *testing.T) {
	for _, dataType := range []DataType{TypeJsonObject, TypeJsonArray} {
		if _, exists := SupportedTypes[dataType]; !exists {
			t.Fatalf("expected %q to be present in SupportedTypes", dataType)
		}
		if !dataType.IsSupported() {
			t.Fatalf("expected %q to report IsSupported() == true", dataType)
		}
	}
}

func TestJsonTypesAreNotNumbers(t *testing.T) {
	for _, dataType := range []DataType{TypeJsonObject, TypeJsonArray} {
		if dataType.IsNumber() {
			t.Fatalf("expected %q to report IsNumber() == false", dataType)
		}
	}
}

func TestJsonTypeConstantValues(t *testing.T) {
	if TypeJsonObject != "jsonObject" {
		t.Fatalf("expected TypeJsonObject == %q, got %q", "jsonObject", TypeJsonObject)
	}
	if TypeJsonArray != "jsonArray" {
		t.Fatalf("expected TypeJsonArray == %q, got %q", "jsonArray", TypeJsonArray)
	}
}

func TestConvertValueByTypeJsonStrict(t *testing.T) {
	// valid object / array pass and decode to the expected Go shape.
	if got, err := ConvertValueByType(`{"a":1}`, TypeJsonObject); err != nil {
		t.Fatalf("expected valid JSON object to convert, got error: %v", err)
	} else if _, ok := got.(map[string]any); !ok {
		t.Fatalf("expected JSON object to decode into map[string]any, got %T", got)
	}
	if got, err := ConvertValueByType(`[1,2,3]`, TypeJsonArray); err != nil {
		t.Fatalf("expected valid JSON array to convert, got error: %v", err)
	} else if _, ok := got.([]any); !ok {
		t.Fatalf("expected JSON array to decode into []any, got %T", got)
	}

	// "null" must be rejected for both json types.
	if _, err := ConvertValueByType("null", TypeJsonObject); err == nil {
		t.Fatalf("expected %q to be rejected for TypeJsonObject", "null")
	}
	if _, err := ConvertValueByType("null", TypeJsonArray); err == nil {
		t.Fatalf("expected %q to be rejected for TypeJsonArray", "null")
	}

	// shape must match: object-as-array and array-as-object are rejected.
	if _, err := ConvertValueByType(`[1,2,3]`, TypeJsonObject); err == nil {
		t.Fatalf("expected an array value to be rejected for TypeJsonObject")
	}
	if _, err := ConvertValueByType(`{"a":1}`, TypeJsonArray); err == nil {
		t.Fatalf("expected an object value to be rejected for TypeJsonArray")
	}
}

// json types are type-only: their value is carried as a JSON-encoded string.
// GetDataType maps a Go string to TypeString (not a json type); the json
// distinction is a schema-level declaration, so a raw Go string is never
// auto-detected as json.
func TestGetDataTypeDoesNotInferJsonFromString(t *testing.T) {
	if got := GetDataType(`{"a":1}`); got != TypeString {
		t.Fatalf("expected a Go string to map to TypeString, got %q", got)
	}
	if got := GetDataType(`[1,2,3]`); got != TypeString {
		t.Fatalf("expected a Go string to map to TypeString, got %q", got)
	}
}
