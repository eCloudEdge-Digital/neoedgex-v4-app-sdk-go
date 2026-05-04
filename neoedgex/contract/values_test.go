package contract

import "testing"

func TestNewPortFieldDataWithAnyRejectsNilValue(t *testing.T) {
	field, err := NewPortFieldDataWithAny(nil, FormatInt64)
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
	value, format, err := ConvertAnyValue(nil)
	if err == nil {
		t.Fatal("expected error for nil value")
	}
	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
	if format != FormatUndefined {
		t.Fatalf("expected undefined format, got %s", format)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestConvertAnyValueRejectsTypedNilSlice(t *testing.T) {
	var raw []byte

	value, format, err := ConvertAnyValue(raw)
	if err == nil {
		t.Fatal("expected error for typed nil slice")
	}
	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
	if format != FormatUndefined {
		t.Fatalf("expected undefined format, got %s", format)
	}
	if got := err.Error(); got != "nil value is not supported for conversion" {
		t.Fatalf("unexpected error: %s", got)
	}
}
