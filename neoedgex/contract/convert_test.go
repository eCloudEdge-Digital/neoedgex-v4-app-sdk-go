package contract

// Replaces values_json_outbound_test.go: the outbound path no longer
// stringifies values for the wire. ConvertToTypedValue is the single native
// value cross-type conversion matrix shared by Publish (outbound quadrant) and
// the schema-driven decode path (inbound quadrant); this file pins it cell by
// cell, including range checks, truncation, parsing, NaN/Inf rejection and the
// value-domain rejections (containers, time.Time, nil).

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// TestConvertToTypedValueSameType pins the identity cell for all 11 scalars:
// a value already of the schema type passes through with type and value intact.
func TestConvertToTypedValueSameType(t *testing.T) {
	cases := []struct {
		destType DataType
		value    any
	}{
		{TypeBool, true},
		{TypeInt16, int16(-12345)},
		{TypeInt32, int32(-2000000000)},
		{TypeInt64, int64(math.MinInt64)},
		{TypeUint16, uint16(math.MaxUint16)},
		{TypeUint32, uint32(math.MaxUint32)},
		{TypeUint64, uint64(math.MaxUint64)},
		{TypeFloat, float32(0.1)},
		{TypeDouble, float64(3.141592653589793)},
		{TypeString, "hello 世界"},
		{TypeRaw, []byte{0x00, 0xff, 0x80}},
	}
	for _, tc := range cases {
		t.Run(string(tc.destType), func(t *testing.T) {
			got, err := ConvertToTypedValue(tc.value, tc.destType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.value) {
				t.Fatalf("identity conversion changed value: got %#v, want %#v", got, tc.value)
			}
		})
	}
}

// TestConvertToTypedValueIntegerPrecision pins that int64/uint64 extremes
// survive the matrix without any float64 round-trip: the result is the exact
// integer, never a 9.22e+18-style corrupted value.
func TestConvertToTypedValueIntegerPrecision(t *testing.T) {
	got, err := ConvertToTypedValue(int64(math.MaxInt64), TypeInt64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got.(int64); !ok || v != math.MaxInt64 {
		t.Fatalf("int64 max corrupted: got %#v", got)
	}

	got, err = ConvertToTypedValue(uint64(math.MaxUint64), TypeUint64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got.(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("uint64 max corrupted: got %#v", got)
	}

	// uint64 -> int64 within range keeps exactness; max-1 of int64 via uint64.
	got, err = ConvertToTypedValue(uint64(math.MaxInt64), TypeInt64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got.(int64); !ok || v != math.MaxInt64 {
		t.Fatalf("uint64(MaxInt64) -> int64 corrupted: got %#v", got)
	}
	if s := fmt.Sprintf("%v", got); strings.Contains(s, "e+") {
		t.Fatalf("integer rendered in float notation: %s", s)
	}
}

// TestConvertToTypedValueCrossTypeAllowed pins the allowed cross-type cells.
func TestConvertToTypedValueCrossTypeAllowed(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		destType DataType
		want     any
	}{
		// float -> int: fractional part truncated (matrix rule), sign kept.
		{"double 2.9 -> int16 truncates", float64(2.9), TypeInt16, int16(2)},
		{"double -2.9 -> int16 truncates", float64(-2.9), TypeInt16, int16(-2)},
		{"float 100.7 -> uint32 truncates", float32(100.7), TypeUint32, uint32(100)},
		// string -> number: parsed (integer strings for int dests).
		{"string '42' -> int32", "42", TypeInt32, int32(42)},
		{"string '-7' -> int64", "-7", TypeInt64, int64(-7)},
		{"string '2.5' -> double", "2.5", TypeDouble, float64(2.5)},
		{"string '1.5' -> float", "1.5", TypeFloat, float32(1.5)},
		{"string '65535' -> uint16", "65535", TypeUint16, uint16(65535)},
		// int family widening / narrowing within range.
		{"int16 -> int64", int16(-5), TypeInt64, int64(-5)},
		{"int64 300 -> uint16", int64(300), TypeUint16, uint16(300)},
		{"uint32 -> int32", uint32(7), TypeInt32, int32(7)},
		{"int -> double exact", int64(1 << 52), TypeDouble, float64(1 << 52)},
		// bool <-> number.
		{"bool true -> int32", true, TypeInt32, int32(1)},
		{"bool true -> int16", true, TypeInt16, int16(1)},
		{"bool false -> uint64", false, TypeUint64, uint64(0)},
		{"bool true -> uint64", true, TypeUint64, uint64(1)},
		{"bool true -> uint16", true, TypeUint16, uint16(1)},
		{"bool true -> uint32", true, TypeUint32, uint32(1)},
		{"bool true -> double", true, TypeDouble, float64(1)},
		{"int64 -3 -> bool", int64(-3), TypeBool, true},
		{"uint16 0 -> bool", uint16(0), TypeBool, false},
		{"double 0.5 -> bool", float64(0.5), TypeBool, true},
		// number/bool -> string (stringification via ConvertAnyValue).
		{"int64 -> string", int64(7), TypeString, "7"},
		{"bool -> string", true, TypeString, "true"},
		{"double -> string", float64(2.5), TypeString, "2.5e+00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConvertToTypedValue(tc.value, tc.destType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestConvertToTypedValueFloatToDoubleShortestDecimal pins the D14/E11
// scalar-literal guarantee: float -> double widening restores the
// shortest-decimal value (25.34, not 25.34000015258789), matching the
// pre-CBOR stringified path.
func TestConvertToTypedValueFloatToDoubleShortestDecimal(t *testing.T) {
	got, err := ConvertToTypedValue(float32(25.34), TypeDouble)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got.(float64); !ok || v != float64(25.34) {
		t.Fatalf("float -> double lost shortest-decimal value: got %#v, want %v", got, float64(25.34))
	}

	s, dt, err := ConvertAnyValue(got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dt != TypeDouble || s != "2.534e+01" {
		t.Fatalf("display string drifted: got %q (%s), want %q (%s)", s, dt, "2.534e+01", TypeDouble)
	}
}

// TestConvertToTypedValueRangeChecked pins the out-of-range rejects: the
// matrix must error (never wrap or silently saturate).
func TestConvertToTypedValueRangeChecked(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		destType DataType
	}{
		{"int64 70000 -> int16", int64(70000), TypeInt16},
		{"int32 -40000 -> int16", int32(-40000), TypeInt16},
		{"uint64 max -> int64", uint64(math.MaxUint64), TypeInt64},
		{"int64 -1 -> uint16", int64(-1), TypeUint16},
		{"int64 -1 -> uint64", int64(-1), TypeUint64},
		{"double 1e10 -> int32", float64(1e10), TypeInt32},
		{"double -0.5 -> uint16 (negative after trunc? no: -0.5 truncs to -0? use -1.5)", float64(-1.5), TypeUint16},
		{"double 2e19 -> uint64", float64(2e19), TypeUint64},
		{"double 1e300 -> float", float64(1e300), TypeFloat},
		{"double -1e300 -> float", float64(-1e300), TypeFloat},
		{"string '1e300' -> float", "1e300", TypeFloat},
		{"string '70000' -> int16", "70000", TypeInt16},
		{"string 'abc' -> int32", "abc", TypeInt32},
		{"string '2.9' -> int32 (integer parse only)", "2.9", TypeInt32},
		{"string 'abc' -> double", "abc", TypeDouble},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConvertToTypedValue(tc.value, tc.destType)
			if err == nil {
				t.Fatalf("expected error, got %#v", got)
			}
		})
	}

	// Underflow-to-zero is standard narrowing, not Inf — stays allowed.
	if got, err := ConvertToTypedValue(float64(1e-300), TypeFloat); err != nil || got != float32(0) {
		t.Fatalf("double 1e-300 -> float: got %#v, err %v; want float32(0), nil", got, err)
	}
}

// TestConvertToTypedValueRejectsNaNAndInf pins the D6 fail-fast: NaN and
// +/-Inf are rejected for every destination, including via string parsing.
func TestConvertToTypedValueRejectsNaNAndInf(t *testing.T) {
	for _, dest := range []DataType{TypeInt32, TypeUint64, TypeFloat, TypeDouble, TypeBool} {
		for _, v := range []any{math.NaN(), math.Inf(1), math.Inf(-1), float32(float64(math.Inf(1)))} {
			if got, err := ConvertToTypedValue(v, dest); err == nil {
				t.Fatalf("expected NaN/Inf rejection for %v -> %s, got %#v", v, dest, got)
			}
		}
	}
	// string forms parse to NaN/Inf and must also be rejected on float dests.
	for _, s := range []string{"NaN", "Inf", "-Inf"} {
		if got, err := ConvertToTypedValue(s, TypeDouble); err == nil {
			t.Fatalf("expected rejection for string %q -> double, got %#v", s, got)
		}
	}
}

// TestConvertToTypedValueMatrixDenials pins the denied cells: raw is an
// island (only raw -> raw), string never becomes bool, nothing becomes raw.
func TestConvertToTypedValueMatrixDenials(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		destType DataType
	}{
		{"raw -> int32", []byte{1}, TypeInt32},
		{"raw -> string", []byte("hi"), TypeString},
		{"raw -> bool", []byte{1}, TypeBool},
		{"string -> raw", "hi", TypeRaw},
		{"int64 -> raw", int64(1), TypeRaw},
		{"string 'true' -> bool", "true", TypeBool},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ConvertToTypedValue(tc.value, tc.destType); err == nil {
				t.Fatalf("expected matrix denial, got %#v", got)
			}
		})
	}

	// raw -> raw stays byte-identical.
	in := []byte{0x00, 0xff}
	got, err := ConvertToTypedValue(in, TypeRaw)
	if err != nil {
		t.Fatalf("unexpected error for raw -> raw: %v", err)
	}
	if b, ok := got.([]byte); !ok || string(b) != string(in) {
		t.Fatalf("raw -> raw changed bytes: %#v", got)
	}
}

// TestConvertToTypedValueNativeGoIntegerKinds pins the widened INPUT domain:
// int, uint, int8 and uint8 — the kinds an untyped constant or a small counter
// naturally lands in — are accepted and converted to the declared tag type.
// Before this they yielded TypeUndefined, so ctx.Publish(h, map[string]any{
// "count": 5}) sent CBOR null plus a node error while returning nil.
func TestConvertToTypedValueNativeGoIntegerKinds(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		destType DataType
		want     any
	}{
		// the motivating case: an untyped constant defaults to int.
		{"int 5 -> int64", 5, TypeInt64, int64(5)},
		{"int 5 -> int16", int(5), TypeInt16, int16(5)},
		{"int 5 -> uint32", int(5), TypeUint32, uint32(5)},
		{"int -5 -> int32", int(-5), TypeInt32, int32(-5)},
		{"int max -> int64", int(math.MaxInt64), TypeInt64, int64(math.MaxInt64)},
		{"int -> double", int(1 << 52), TypeDouble, float64(1 << 52)},
		{"int -> float", int(3), TypeFloat, float32(3)},
		{"int -> string", int(-7), TypeString, "-7"},
		{"int 0 -> bool", int(0), TypeBool, false},
		{"int 3 -> bool", int(3), TypeBool, true},

		{"uint 5 -> uint64", uint(5), TypeUint64, uint64(5)},
		{"uint max -> uint64", uint(math.MaxUint64), TypeUint64, uint64(math.MaxUint64)},
		{"uint 5 -> int16", uint(5), TypeInt16, int16(5)},
		{"uint -> string", uint(9), TypeString, "9"},
		{"uint 0 -> bool", uint(0), TypeBool, false},

		{"int8 -> int16", int8(-128), TypeInt16, int16(-128)},
		{"int8 -> int64", int8(127), TypeInt64, int64(127)},
		{"int8 -> uint16", int8(7), TypeUint16, uint16(7)},
		{"int8 -> double", int8(-3), TypeDouble, float64(-3)},
		{"int8 -> string", int8(-3), TypeString, "-3"},
		{"int8 -> bool", int8(-1), TypeBool, true},

		{"uint8 -> uint16", uint8(255), TypeUint16, uint16(255)},
		{"uint8 -> int16", uint8(255), TypeInt16, int16(255)},
		{"uint8 -> uint64", uint8(1), TypeUint64, uint64(1)},
		{"uint8 -> float", uint8(2), TypeFloat, float32(2)},
		{"uint8 -> string", uint8(2), TypeString, "2"},
		{"uint8 0 -> bool", uint8(0), TypeBool, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConvertToTypedValue(tc.value, tc.destType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestConvertToTypedValueNativeGoIntegerKindsRangeChecked pins that widening
// the accepted INPUT kinds did not widen the tag types: the new kinds get the
// same range checking as the sized ones, so int 70000 into an int16 tag still
// fails rather than wrapping to 4464.
func TestConvertToTypedValueNativeGoIntegerKindsRangeChecked(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		destType DataType
	}{
		{"int 70000 -> int16", int(70000), TypeInt16},
		{"int -40000 -> int16", int(-40000), TypeInt16},
		{"int -1 -> uint16", int(-1), TypeUint16},
		{"int -1 -> uint64", int(-1), TypeUint64},
		{"int 1e10 -> int32", int(10000000000), TypeInt32},
		{"uint max -> int64", uint(math.MaxUint64), TypeInt64},
		{"uint 70000 -> uint16", uint(70000), TypeUint16},
		{"int8 -> raw", int8(1), TypeRaw},
		{"uint8 -> raw", uint8(1), TypeRaw},
		{"int -> raw", int(1), TypeRaw},
		{"uint -> raw", uint(1), TypeRaw},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ConvertToTypedValue(tc.value, tc.destType); err == nil {
				t.Fatalf("expected rejection, got %#v", got)
			}
		})
	}

	// The boundary still passes on the same destination that rejects 70000.
	if got, err := ConvertToTypedValue(int(32767), TypeInt16); err != nil || got != int16(32767) {
		t.Fatalf("int 32767 -> int16: got %#v, err %v; want int16(32767), nil", got, err)
	}
}

// TestConvertAnyValueNativeGoIntegerKinds pins the stringify helper against
// the same widened domain, so a value the conversion path accepts can never
// blow up in the display/PortFieldData quadrant.
func TestConvertAnyValueNativeGoIntegerKinds(t *testing.T) {
	cases := []struct {
		value     any
		wantText  string
		wantsType DataType
	}{
		{int(-7), "-7", TypeInt64},
		{int(math.MaxInt64), "9223372036854775807", TypeInt64},
		{uint(9), "9", TypeUint64},
		{uint(math.MaxUint64), "18446744073709551615", TypeUint64},
		{int8(-128), "-128", TypeInt16},
		{uint8(255), "255", TypeUint16},
	}
	for _, tc := range cases {
		text, detected, err := ConvertAnyValue(tc.value)
		if err != nil {
			t.Fatalf("ConvertAnyValue(%#v) failed: %v", tc.value, err)
		}
		if text != tc.wantText || detected != tc.wantsType {
			t.Fatalf("ConvertAnyValue(%#v) = (%q, %q), want (%q, %q)", tc.value, text, detected, tc.wantText, tc.wantsType)
		}
		if detected != GetDataType(tc.value) {
			t.Fatalf("ConvertAnyValue and GetDataType disagree on %T: %q vs %q", tc.value, detected, GetDataType(tc.value))
		}
		// the reported type must parse its own text back
		if _, err := ConvertValueByType(text, detected); err != nil {
			t.Fatalf("round-trip %q as %q failed: %v", text, detected, err)
		}
	}

	// PortFieldData, the other consumer of the stringify helper.
	field, err := NewPortFieldDataWithAny(int(5), TypeInt64)
	if err != nil {
		t.Fatalf("NewPortFieldDataWithAny(int(5)) failed: %v", err)
	}
	if field.Type != TypeInt64 || field.Value != "5" {
		t.Fatalf("got %#v, want {Type:int64 Value:\"5\"}", field)
	}
}

// TestConvertToTypedValueValueDomain pins the D2/D3/D4 value-domain gates:
// containers, structs, time.Time, defined byte types, pointers and nil are all
// rejected — there is no container tag type in the v2 universe.
func TestConvertToTypedValueValueDomain(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"nil", nil},
		{"typed nil slice", []byte(nil)},
		{"map", map[string]any{"k": 1}},
		{"slice", []any{1, 2}},
		{"struct", struct{ X int }{1}},
		{"pointer to int", new(int)},
		{"defined byte type", json.RawMessage(`{"a":1}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, dest := range []DataType{TypeString, TypeInt64, TypeDouble} {
				if got, err := ConvertToTypedValue(tc.value, dest); err == nil {
					t.Fatalf("expected value-domain rejection for %s -> %s, got %#v", tc.name, dest, got)
				}
			}
		})
	}

	// time.Time: dedicated error guiding the app to Format() (D4).
	if _, err := ConvertToTypedValue(time.Now(), TypeString); err == nil {
		t.Fatal("expected time.Time rejection")
	} else if !strings.Contains(err.Error(), "Format") {
		t.Fatalf("time.Time error should guide toward Format, got: %v", err)
	}

	// unsupported destination type (undefined and removed containers).
	for _, dest := range []DataType{TypeUndefined, "jsonObject", "jsonArray"} {
		if got, err := ConvertToTypedValue(int64(1), dest); err == nil {
			t.Fatalf("expected unsupported-dest rejection for %q, got %#v", dest, got)
		}
	}
}
