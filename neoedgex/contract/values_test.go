package contract

// PortFieldData has left the wire (values ride as native CBOR values now); it
// survives only for sending-side validation, the mock config format and the
// agent gatewaymetrics payload builder. These tests pin the retained semantics
// so that consumer does not break.

import (
	"bytes"
	"math"
	"testing"
)

// TestNewPortFieldDataWithStringKeepsValueVerbatim pins the semantics
// gatewaymetrics depends on: the string is validated against the tag type but
// stored EXACTLY as given — no normalization, no re-formatting.
func TestNewPortFieldDataWithStringKeepsValueVerbatim(t *testing.T) {
	cases := []struct {
		value    string
		dataType DataType
	}{
		{"007", TypeInt64},                   // leading zeros kept
		{"25.50", TypeDouble},                // trailing zero kept (not "25.5")
		{"9223372036854775807", TypeInt64},   // int64 max stays exact text
		{"18446744073709551615", TypeUint64}, // uint64 max stays exact text
		{"true", TypeBool},                   //
		{"AAECAP8=", TypeRaw},                // base64 text form for raw
		{"any text", TypeString},             //
		{"-32768", TypeInt16},                // boundary
	}
	for _, tc := range cases {
		field, err := NewPortFieldDataWithString(tc.value, tc.dataType)
		if err != nil {
			t.Fatalf("NewPortFieldDataWithString(%q, %q) failed: %v", tc.value, tc.dataType, err)
		}
		if field.Value != tc.value {
			t.Fatalf("value normalized: got %q, want verbatim %q", field.Value, tc.value)
		}
		if field.Type != tc.dataType {
			t.Fatalf("type: got %q, want %q", field.Type, tc.dataType)
		}
	}
}

// TestNewPortFieldDataWithStringRejectsIncompatible pins the validation gate:
// an unparsable value or an unsupported tag type errors instead of producing a
// field that explodes later at GetAnyValue time.
func TestNewPortFieldDataWithStringRejectsIncompatible(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		dataType DataType
	}{
		{"out of int16 range", "70000", TypeInt16},
		{"not a number", "abc", TypeInt64},
		{"float text into int", "2.5", TypeInt32},
		{"negative into uint", "-1", TypeUint32},
		{"invalid base64 into raw", "!!!not-base64!!!", TypeRaw},
		{"undefined type", "1", TypeUndefined},
		{"removed container type", "{}", DataType("jsonObject")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, err := NewPortFieldDataWithString(tc.value, tc.dataType)
			if err == nil {
				t.Fatalf("expected error, got %#v", field)
			}
			if field != nil {
				t.Fatalf("expected nil field on error, got %#v", field)
			}
		})
	}
}

// TestPortFieldDataGetAnyValueAndCast pins the read side used by the agent:
// GetAnyValue returns the concrete Go type of the tag type, and
// GetValueAndCast fails loudly on a wrong target type instead of returning a
// zero value silently.
func TestPortFieldDataGetAnyValueAndCast(t *testing.T) {
	field := PortFieldData{Type: TypeUint64, Value: "18446744073709551615"}
	got, err := field.GetAnyValue()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got.(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("GetAnyValue: got %#v (%T), want uint64 max", got, got)
	}

	casted, err := GetValueAndCast[uint64](field)
	if err != nil {
		t.Fatalf("unexpected cast error: %v", err)
	}
	if casted != math.MaxUint64 {
		t.Fatalf("GetValueAndCast: got %d", casted)
	}
	if _, err := GetValueAndCast[int64](field); err == nil {
		t.Fatal("expected cast error for the wrong target type")
	}

	// raw fields decode from their base64 text form back to bytes.
	rawField := PortFieldData{Type: TypeRaw, Value: "AAECAP8="}
	rawValue, err := GetValueAndCast[[]byte](rawField)
	if err != nil {
		t.Fatalf("unexpected raw cast error: %v", err)
	}
	if !bytes.Equal(rawValue, []byte{0x00, 0x01, 0x02, 0x00, 0xff}) {
		t.Fatalf("raw bytes: got % x", rawValue)
	}
}

// TestPortFieldDataConvertTo pins the retained conversion helper: same type is
// a no-op passthrough, a cross-type conversion goes through the native matrix
// and re-stringifies, and a denied cell errors.
func TestPortFieldDataConvertTo(t *testing.T) {
	source := PortFieldData{Type: TypeInt64, Value: "007"}

	same, err := source.ConvertTo(TypeInt64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if same.Value != "007" || same.Type != TypeInt64 {
		t.Fatalf("same-type ConvertTo must pass through verbatim, got %#v", same)
	}

	asString, err := source.ConvertTo(TypeString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if asString.Type != TypeString || asString.Value != "7" {
		t.Fatalf("int64 -> string: got %#v, want value \"7\"", asString)
	}

	if _, err := source.ConvertTo(TypeRaw); err == nil {
		t.Fatal("expected matrix denial for int64 -> raw")
	}
	if _, err := (PortFieldData{Type: TypeInt16, Value: "70000"}).ConvertTo(TypeInt64); err == nil {
		t.Fatal("expected error for a field whose value does not fit its own type")
	}
}

// TestNewPortFieldDataWithAnyStringifiesNativeValues pins the native-value
// implementation (deviation (c)): the field carries the tag type's canonical
// string form — float uses the 'e' format the agent formatting rules mirror.
func TestNewPortFieldDataWithAnyStringifiesNativeValues(t *testing.T) {
	cases := []struct {
		value     any
		destType  DataType
		wantValue string
	}{
		{int64(math.MaxInt64), TypeInt64, "9223372036854775807"},
		{uint64(math.MaxUint64), TypeUint64, "18446744073709551615"},
		{float64(25.5), TypeDouble, "25.5"},
		{float32(1.5), TypeFloat, "1.5"},
		{true, TypeBool, "true"},
		{"text", TypeString, "text"},
		{[]byte{0x00, 0x01, 0x02, 0x00, 0xff}, TypeRaw, "AAECAP8="},
		{float64(2.9), TypeInt16, "2"}, // matrix truncation, then stringified
		{"42", TypeInt32, "42"},        // matrix parse, then stringified
	}
	for _, tc := range cases {
		field, err := NewPortFieldDataWithAny(tc.value, tc.destType)
		if err != nil {
			t.Fatalf("NewPortFieldDataWithAny(%#v, %q) failed: %v", tc.value, tc.destType, err)
		}
		if field.Type != tc.destType || field.Value != tc.wantValue {
			t.Fatalf("got %#v, want {Type:%q Value:%q}", field, tc.destType, tc.wantValue)
		}
	}
}

// TestNewEmptyFieldIsUndefined pins the undefined marker the mock injection
// path turns into a CBOR null.
// TestConvertAnyValueFloatsAreFixedPointDecimal pins the canonical float
// string form: strconv's 'f' verb at shortest round-trip precision, the same
// rendering the platform's formula engine and forwarder payloads use. The
// notation never switches to an exponent, an integer-valued float carries no
// ".0", and negative zero keeps its sign. The Python SDK pins the identical
// table, so the two SDKs cannot drift apart.
func TestConvertAnyValueFloatsAreFixedPointDecimal(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{float64(25.34), "25.34"},
		{float64(500), "500"},
		{float64(0), "0"},
		{math.Copysign(0, -1), "-0"},
		{float64(0.000123), "0.000123"},
		{float64(1e-7), "0.0000001"},
		{float64(1e21), "1000000000000000000000"},
		{float64(-2.5), "-2.5"},
		{float64(1234567), "1234567"},
		// float32 formats at 32-bit shortest precision: the widened double of
		// float32(25.34) is 25.340000152587891, but the string stays "25.34".
		{float32(25.34), "25.34"},
		{float32(500), "500"},
	}
	for _, tc := range cases {
		s, _, err := ConvertAnyValue(tc.value)
		if err != nil {
			t.Fatalf("ConvertAnyValue(%v): unexpected error: %v", tc.value, err)
		}
		if s != tc.want {
			t.Fatalf("ConvertAnyValue(%v) = %q, want %q", tc.value, s, tc.want)
		}
	}

	// The parse side must keep accepting the OLD scientific form: a message
	// stringified by a pre-v2.2.0 publisher still converts. Tightening this
	// would break old-publisher-to-new-consumer traffic.
	for _, tc := range []struct {
		text     string
		destType DataType
		want     any
	}{
		{"2.534e+01", TypeDouble, float64(25.34)},
		{"1.5e+00", TypeFloat, float32(1.5)},
	} {
		got, err := ConvertToTypedValue(tc.text, tc.destType)
		if err != nil {
			t.Fatalf("legacy form %q no longer parses: %v", tc.text, err)
		}
		if got != tc.want {
			t.Fatalf("legacy form %q parsed to %#v, want %#v", tc.text, got, tc.want)
		}
	}
}

func TestNewEmptyFieldIsUndefined(t *testing.T) {
	field := NewEmptyField()
	if field.Type != TypeUndefined {
		t.Fatalf("type: got %q, want undefined", field.Type)
	}
	if field.Value != "" {
		t.Fatalf("value: got %q, want empty", field.Value)
	}
	if _, err := field.GetAnyValue(); err == nil {
		t.Fatal("expected GetAnyValue to reject an undefined field")
	}
}

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
