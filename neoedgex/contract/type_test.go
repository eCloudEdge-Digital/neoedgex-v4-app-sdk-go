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
