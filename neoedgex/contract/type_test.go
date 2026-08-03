package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// TestDataTypeUniverseDriftGuard pins the v2 type universe: EXACTLY the 11
// scalar tag types (D2), plus TypeUndefined which is NOT a supported type.
// Adding or removing a DataType must fail this test until the change is a
// deliberate, four-quadrant-swept decision (inbound/outbound/validate/runtime).
func TestDataTypeUniverseDriftGuard(t *testing.T) {
	want := map[DataType]string{
		TypeBool:   "bool",
		TypeInt16:  "int16",
		TypeInt32:  "int32",
		TypeInt64:  "int64",
		TypeUint16: "uint16",
		TypeUint32: "uint32",
		TypeUint64: "uint64",
		TypeFloat:  "float",
		TypeDouble: "double",
		TypeString: "string",
		TypeRaw:    "raw",
	}

	if len(SupportedTypes) != len(want) {
		t.Fatalf("SupportedTypes has %d entries, want exactly %d (the 11 scalar tag types)", len(SupportedTypes), len(want))
	}
	for dt, literal := range want {
		if string(dt) != literal {
			t.Fatalf("constant for %q has wire literal %q, want %q", literal, string(dt), literal)
		}
		if _, exists := SupportedTypes[dt]; !exists {
			t.Fatalf("expected %q in SupportedTypes", dt)
		}
		if !dt.IsSupported() {
			t.Fatalf("expected %q.IsSupported() == true", dt)
		}
	}

	// undefined: empty literal, never supported.
	if TypeUndefined != "" {
		t.Fatalf("TypeUndefined literal changed: %q", TypeUndefined)
	}
	if TypeUndefined.IsSupported() {
		t.Fatal("TypeUndefined must not be supported")
	}

	// The removed json container types must NOT come back silently.
	for _, gone := range []DataType{"jsonObject", "jsonArray"} {
		if gone.IsSupported() {
			t.Fatalf("removed type %q is supported again; container types are a separate ticket", gone)
		}
	}
}

// TestIsNumberPartition pins the number/non-number split over the whole
// universe: 8 numeric types, 3 non-numeric, undefined non-numeric.
func TestIsNumberPartition(t *testing.T) {
	numeric := []DataType{TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble}
	nonNumeric := []DataType{TypeBool, TypeString, TypeRaw, TypeUndefined}

	for _, dt := range numeric {
		if !dt.IsNumber() {
			t.Fatalf("expected %q.IsNumber() == true", dt)
		}
	}
	for _, dt := range nonNumeric {
		if dt.IsNumber() {
			t.Fatalf("expected %q.IsNumber() == false", dt)
		}
	}
	if len(numeric)+len(nonNumeric)-1 != len(SupportedTypes) {
		t.Fatalf("IsNumber partition (%d numeric + %d non-numeric - undefined) does not cover SupportedTypes (%d)",
			len(numeric), len(nonNumeric), len(SupportedTypes))
	}
}

// TestGetDataTypeMapping pins native Go value -> tag type detection for all 11
// scalars plus the four Go integer kinds with no tag type of their own (int,
// uint, int8, uint8), which widen to the narrowest tag type holding them, and
// that everything outside the scalar universe (containers, time.Time, defined
// byte types, nil) maps to TypeUndefined.
func TestGetDataTypeMapping(t *testing.T) {
	scalar := []struct {
		value any
		want  DataType
	}{
		{true, TypeBool},
		{int16(1), TypeInt16},
		{int32(1), TypeInt32},
		{int64(1), TypeInt64},
		{uint16(1), TypeUint16},
		{uint32(1), TypeUint32},
		{uint64(1), TypeUint64},
		{float32(1), TypeFloat},
		{float64(1), TypeDouble},
		{"s", TypeString},
		{[]byte{1}, TypeRaw},
		// Go integer kinds with no tag type: accepted as INPUT, reported as
		// the narrowest tag type that holds their whole range.
		{int8(1), TypeInt16},
		{uint8(1), TypeUint16},
		{int(1), TypeInt64},
		{uint(1), TypeUint64},
	}
	for _, tc := range scalar {
		if got := GetDataType(tc.value); got != tc.want {
			t.Fatalf("GetDataType(%T) = %q, want %q", tc.value, got, tc.want)
		}
	}

	// Widening a Go integer kind must never leak a value: every reported tag
	// type has to hold the source kind's extremes.
	widened := []any{
		int8(math.MinInt8), int8(math.MaxInt8),
		uint8(0), uint8(math.MaxUint8),
		int(math.MinInt64), int(math.MaxInt64),
		uint(0), uint(math.MaxUint64),
	}
	for _, v := range widened {
		dt := GetDataType(v)
		if _, err := ConvertToTypedValue(v, dt); err != nil {
			t.Fatalf("GetDataType(%T) reported %q, which cannot hold %v: %v", v, dt, v, err)
		}
	}

	outside := []any{
		nil,
		map[string]any{"k": 1},
		[]any{1},
		struct{}{},
		time.Now(),
		json.RawMessage(`{"a":1}`), // defined byte type must not sniff as raw/json
	}
	for _, v := range outside {
		if got := GetDataType(v); got != TypeUndefined {
			t.Fatalf("GetDataType(%T) = %q, want TypeUndefined", v, got)
		}
	}

	// []byte stays raw: only the scalar uint8 became an integer.
	if got := GetDataType([]byte{1, 2}); got != TypeRaw {
		t.Fatalf("GetDataType([]byte) = %q, want %q — the byte slice must not follow uint8", got, TypeRaw)
	}
	if got := GetDataType([]uint8{1, 2}); got != TypeRaw {
		t.Fatalf("GetDataType([]uint8) = %q, want %q", got, TypeRaw)
	}
}

// quadrantSamples holds one native Go sample value per supported tag type. The
// float samples are exactly representable so the string quadrant round-trips
// bit-exactly.
var quadrantSamples = map[DataType]any{
	TypeBool:   true,
	TypeInt16:  int16(-2),
	TypeInt32:  int32(-3),
	TypeInt64:  int64(-4),
	TypeUint16: uint16(5),
	TypeUint32: uint32(6),
	TypeUint64: uint64(7),
	TypeFloat:  float32(0.5),
	TypeDouble: float64(0.25),
	TypeString: "s",
	TypeRaw:    []byte{0x01, 0xff},
}

// TestSupportedTypeFourQuadrantSweep is the "saved fine, exploded at runtime"
// guard: every entry of SupportedTypes must be wired through all four
// quadrants, so a newly added tag type cannot pass validation while a decode
// or conversion path silently rejects it.
//
//	quadrant 1 (outbound / Publish)      ConvertToTypedValue identity
//	quadrant 2 (inbound / wire decode)   decodeTyped over the CBOR encoding
//	quadrant 3 (validation matrix)       CanConvertTo self + GetDataType
//	quadrant 4 (runtime PortFieldData)   ConvertAnyValue -> ConvertValueByType
func TestSupportedTypeFourQuadrantSweep(t *testing.T) {
	for dt := range SupportedTypes {
		sample, ok := quadrantSamples[dt]
		if !ok {
			t.Fatalf("DataType %q is supported but has no sample in quadrantSamples; sweep the four quadrants for it", dt)
		}

		t.Run(string(dt), func(t *testing.T) {
			wantRepr := fmt.Sprintf("%#v", sample)

			// quadrant 3: type detection and self-conversion must agree.
			if got := GetDataType(sample); got != dt {
				t.Fatalf("GetDataType(%#v) = %q, want %q", sample, got, dt)
			}
			if !dt.CanConvertTo(dt) {
				t.Fatalf("%q.CanConvertTo(%q) == false; the matrix rejects its own type", dt, dt)
			}

			// quadrant 1: the Publish-side matrix keeps the concrete Go type.
			outbound, err := ConvertToTypedValue(sample, dt)
			if err != nil {
				t.Fatalf("ConvertToTypedValue(%#v, %q) failed: %v", sample, dt, err)
			}
			if got := fmt.Sprintf("%#v", outbound); got != wantRepr {
				t.Fatalf("outbound quadrant changed the value: got %s, want %s", got, wantRepr)
			}

			// quadrant 2: the schema-driven decode path must know the type.
			wire, err := cbor.Marshal(sample)
			if err != nil {
				t.Fatalf("cbor.Marshal(%#v) failed: %v", sample, err)
			}
			inbound, err := decodeTyped(wire, dt)
			if err != nil {
				t.Fatalf("decodeTyped(%q) failed: %v", dt, err)
			}
			if got := fmt.Sprintf("%#v", inbound); got != wantRepr {
				t.Fatalf("inbound quadrant changed the value: got %s, want %s", got, wantRepr)
			}

			// quadrant 4: the retained PortFieldData string form round-trips.
			text, detected, err := ConvertAnyValue(sample)
			if err != nil {
				t.Fatalf("ConvertAnyValue(%#v) failed: %v", sample, err)
			}
			if detected != dt {
				t.Fatalf("ConvertAnyValue detected %q, want %q", detected, dt)
			}
			back, err := ConvertValueByType(text, dt)
			if err != nil {
				t.Fatalf("ConvertValueByType(%q, %q) failed: %v", text, dt, err)
			}
			if got := fmt.Sprintf("%#v", back); got != wantRepr {
				t.Fatalf("string quadrant round-trip lost fidelity: %q -> %s, want %s", text, got, wantRepr)
			}
		})
	}
}
