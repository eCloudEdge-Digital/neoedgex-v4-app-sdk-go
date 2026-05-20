package contract

import (
	"testing"
)

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

// ---------- FormatJson ----------

func TestNewPortFieldDataAcceptsMapAsJson(t *testing.T) {
	field, err := NewPortFieldDataWithAny(map[string]any{"foo": "bar"}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Type != TypeString {
		t.Fatalf("expected type %s, got %s", TypeString, field.Type)
	}
	if field.Format != FormatJson {
		t.Fatalf("expected format %s, got %s", FormatJson, field.Format)
	}
	if field.Value != `{"foo":"bar"}` {
		t.Fatalf("unexpected value: %q", field.Value)
	}
}

func TestNewPortFieldDataAcceptsSliceAsJson(t *testing.T) {
	field, err := NewPortFieldDataWithAny([]any{int64(1), "two", true}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Value != `[1,"two",true]` {
		t.Fatalf("unexpected value: %q", field.Value)
	}
}

func TestNewPortFieldDataAcceptsStructAsJson(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	field, err := NewPortFieldDataWithAny(sample{Name: "x", Age: 7}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Value != `{"name":"x","age":7}` {
		t.Fatalf("unexpected value: %q", field.Value)
	}
}

func TestNewPortFieldDataAcceptsEmptyObjectAndArray(t *testing.T) {
	emptyObj, err := NewPortFieldDataWithAny(map[string]any{}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error for empty object: %v", err)
	}
	if emptyObj.Value != `{}` {
		t.Fatalf("unexpected value for empty object: %q", emptyObj.Value)
	}

	emptyArr, err := NewPortFieldDataWithAny([]any{}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error for empty array: %v", err)
	}
	if emptyArr.Value != `[]` {
		t.Fatalf("unexpected value for empty array: %q", emptyArr.Value)
	}
}

func TestNewPortFieldDataPassesStringAsRawJson(t *testing.T) {
	// Pre-serialised JSON object as a Go string should pass through
	// without re-marshalling.
	input := `{"foo":"bar","id":9007199254740993}`
	field, err := NewPortFieldDataWithAny(input, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Value != input {
		t.Fatalf("string passthrough should preserve input verbatim: got %q, want %q", field.Value, input)
	}
}

func TestNewPortFieldDataPassesByteSliceAsRawJson(t *testing.T) {
	input := []byte(`[1,2,3]`)
	field, err := NewPortFieldDataWithAny(input, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Value != `[1,2,3]` {
		t.Fatalf("unexpected value: %q", field.Value)
	}
}

func TestNewPortFieldDataRejectsScalarMarshalAsJson(t *testing.T) {
	// json.Marshal of these scalars produces JSON primitives like "42",
	// "3.14", "true" — all rejected by the shape check.
	for _, value := range []any{42, int64(42), uint64(42), 3.14, true} {
		_, err := NewPortFieldDataWithAny(value, FormatJson)
		if err == nil {
			t.Errorf("scalar %T(%v) should be rejected, was not", value, value)
		}
	}
}

func TestNewPortFieldDataRejectsScalarStringAsJson(t *testing.T) {
	// Plain string is valid JSON (quoted string), but not an object/array.
	_, err := NewPortFieldDataWithAny(`"hello"`, FormatJson)
	if err == nil {
		t.Fatal("scalar JSON string should be rejected")
	}
}

func TestNewPortFieldDataRejectsInvalidJsonString(t *testing.T) {
	_, err := NewPortFieldDataWithAny(`{not valid json}`, FormatJson)
	if err == nil {
		t.Fatal("invalid JSON string should be rejected")
	}
}

func TestNewPortFieldDataRejectsInvalidJsonByteSlice(t *testing.T) {
	_, err := NewPortFieldDataWithAny([]byte(`{not valid json}`), FormatJson)
	if err == nil {
		t.Fatal("invalid JSON []byte should be rejected")
	}
}

func TestNewPortFieldDataRejectsEmptyStringAsJson(t *testing.T) {
	_, err := NewPortFieldDataWithAny("", FormatJson)
	if err == nil {
		t.Fatal("empty string should be rejected")
	}
}

func TestNewPortFieldDataRejectsNonMarshalableAsJson(t *testing.T) {
	_, err := NewPortFieldDataWithAny(make(chan struct{}), FormatJson)
	if err == nil {
		t.Fatal("non-marshalable value should be rejected")
	}
}

func TestNewPortFieldDataAcceptsJsonWithWhitespace(t *testing.T) {
	// Shape check trims whitespace; stored value preserves the original.
	input := "  \n  {\"foo\":\"bar\"}  \n  "
	field, err := NewPortFieldDataWithAny(input, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.Value != input {
		t.Fatalf("value should be preserved verbatim: got %q, want %q", field.Value, input)
	}
}

func TestConvertValueByFormatReturnsRawJsonString(t *testing.T) {
	// Decode path is a no-op: hand the wire string back as-is.
	raw := `{"id":9007199254740993,"label":"demo"}`
	got, err := ConvertValueByFormat(raw, FormatJson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if gotStr != raw {
		t.Fatalf("decode should pass through verbatim: got %q, want %q", gotStr, raw)
	}
}

func TestJsonIsIsolatedFromOtherFormats(t *testing.T) {
	for _, otherFormat := range []DataFormat{
		FormatBool, FormatInt32, FormatInt64, FormatUint64, FormatFloat,
		FormatDouble, FormatString, FormatDatetime, FormatBase64,
	} {
		if FormatJson.CanConvertTo(otherFormat) {
			t.Errorf("json should not be convertible to %s", otherFormat)
		}
		if otherFormat.CanConvertTo(FormatJson) {
			t.Errorf("%s should not be convertible to json", otherFormat)
		}
	}
	if !FormatJson.CanConvertTo(FormatJson) {
		t.Error("json should be convertible to itself")
	}
}

func TestNewPortFieldDataWithStringAcceptsValidJson(t *testing.T) {
	cases := map[string]string{
		"object": `{"foo":"bar"}`,
		"array":  `[1,2,3]`,
	}
	for name, input := range cases {
		field, err := NewPortFieldDataWithString(input, FormatJson)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if field.Type != TypeString || field.Format != FormatJson {
			t.Fatalf("%s: unexpected (type, format): (%s, %s)", name, field.Type, field.Format)
		}
		if field.Value != input {
			t.Fatalf("%s: value should be preserved verbatim: got %q, want %q", name, field.Value, input)
		}
	}
}

func TestNewPortFieldDataWithStringRejectsInvalidJson(t *testing.T) {
	// NewPortFieldDataWithString must validate FormatJson the same way
	// NewPortFieldDataWithAny does — otherwise the public string
	// constructor silently lets through malformed or scalar JSON, because
	// ConvertValueByFormat is a no-op passthrough for FormatJson.
	rejectCases := map[string]string{
		"malformed":      `{not valid json}`,
		"quoted scalar":  `"hello"`,
		"scalar number":  `42`,
		"scalar bool":    `true`,
		"json null":      `null`,
		"empty string":   ``,
		"plain string":   `hello`,
	}
	for name, input := range rejectCases {
		_, err := NewPortFieldDataWithString(input, FormatJson)
		if err == nil {
			t.Errorf("%s: input %q should be rejected as FormatJson, was not", name, input)
		}
	}
}

func TestPortFieldDataGetAnyValueRoundTripsJson(t *testing.T) {
	field, err := NewPortFieldDataWithAny(map[string]any{"k": "v"}, FormatJson)
	if err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}
	decoded, err := field.GetAnyValue()
	if err != nil {
		t.Fatalf("unexpected GetAnyValue error: %v", err)
	}
	if decoded != `{"k":"v"}` {
		t.Fatalf("round-trip mismatch: got %#v, want %q", decoded, `{"k":"v"}`)
	}
}
