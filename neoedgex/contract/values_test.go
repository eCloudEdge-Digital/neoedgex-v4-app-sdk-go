package contract

import "testing"

func TestNewPortFieldDataWithAnyRejectsNilValue(t *testing.T) {
	field, err := NewPortFieldDataWithAny(nil, TypeInt64)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
	if field != nil {
		t.Fatalf("expected nil field, got %#v", field)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestConvertAnyValueRejectsNilValue(t *testing.T) {
	value, dataType, err := ConvertAnyValue(nil)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
	if dataType != TypeUndefined {
		t.Fatalf("expected undefined type, got %s", dataType)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestConvertAnyValueRejectsTypedNilSlice(t *testing.T) {
	var raw []byte

	value, dataType, err := ConvertAnyValue(raw)
	if err == nil {
		t.Fatal("expected error for typed nil slice")
	}
	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
	if dataType != TypeUndefined {
		t.Fatalf("expected undefined type, got %s", dataType)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}
