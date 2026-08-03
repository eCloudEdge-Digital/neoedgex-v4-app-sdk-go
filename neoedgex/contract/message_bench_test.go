package contract

// Cost guard for the ToStruct concrete-field path. The schema-agreement fix
// adds a head-byte comparison per concrete field; BenchmarkMessageToStruct*
// must stay in the same ballpark as before it, and only the mismatching cases
// may pay the plan lookup plus the conversion matrix.

import (
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

var benchSchema = []PortFieldSchema{
	{Key: "flag", Type: TypeBool},
	{Key: "i16", Type: TypeInt16},
	{Key: "i32", Type: TypeInt32},
	{Key: "i64", Type: TypeInt64},
	{Key: "u16", Type: TypeUint16},
	{Key: "u32", Type: TypeUint32},
	{Key: "f", Type: TypeFloat},
	{Key: "d", Type: TypeDouble},
	{Key: "s", Type: TypeString},
	{Key: "r", Type: TypeRaw},
}

// benchMatching is wire data whose every value already has the schema's own
// representation: the fast path must handle all ten fields.
var benchMatching = map[string]any{
	"flag": true,
	"i16":  int16(-1234),
	"i32":  int32(-200000),
	"i64":  int64(-9000000000),
	"u16":  uint16(65535),
	"u32":  uint32(4000000000),
	"f":    float32(25.34),
	"d":    float64(25.34),
	"s":    "sensor-A",
	"r":    []byte{1, 2, 3, 4},
}

type benchTarget struct {
	Flag bool    `cbor:"flag"`
	I16  int16   `cbor:"i16"`
	I32  int32   `cbor:"i32"`
	I64  int64   `cbor:"i64"`
	U16  uint16  `cbor:"u16"`
	U32  uint32  `cbor:"u32"`
	F    float32 `cbor:"f"`
	D    float64 `cbor:"d"`
	S    string  `cbor:"s"`
	R    []byte  `cbor:"r"`
}

// benchPointerTarget is benchTarget declared with pointer fields, the shape
// the guide recommends for undefined discrimination.
type benchPointerTarget struct {
	Flag *bool    `cbor:"flag"`
	I16  *int16   `cbor:"i16"`
	I32  *int32   `cbor:"i32"`
	I64  *int64   `cbor:"i64"`
	U16  *uint16  `cbor:"u16"`
	U32  *uint32  `cbor:"u32"`
	F    *float32 `cbor:"f"`
	D    *float64 `cbor:"d"`
	S    *string  `cbor:"s"`
	R    *[]byte  `cbor:"r"`
}

func benchMessage(b *testing.B, data map[string]any) Message {
	b.Helper()
	raw, err := cbor.Marshal(data)
	if err != nil {
		b.Fatalf("cbor.Marshal failed: %v", err)
	}
	return NewMessage("src", "ts", "input1", RawMessage(raw), NewDecodePlan(benchSchema), nil)
}

func BenchmarkMessageToStructMatching(b *testing.B) {
	msg := benchMessage(b, benchMatching)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var target benchTarget
		if err := msg.ToStruct(&target); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkMessageToStructPointers repeats the matching case with pointer
// fields, the shape the guide recommends for undefined discrimination.
func BenchmarkMessageToStructPointers(b *testing.B) {
	msg := benchMessage(b, benchMatching)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var target benchPointerTarget
		if err := msg.ToStruct(&target); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkMessageToStructWidthMismatch is the diverted case: every float
// arrives at the wrong precision, so each field pays the plan lookup and the
// conversion matrix.
func BenchmarkMessageToStructWidthMismatch(b *testing.B) {
	mismatched := make(map[string]any, len(benchMatching))
	for k, v := range benchMatching {
		mismatched[k] = v
	}
	mismatched["f"] = float64(25.34)
	mismatched["d"] = float32(25.34)
	msg := benchMessage(b, mismatched)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var target benchTarget
		if err := msg.ToStruct(&target); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkWireMatchesFieldGuard isolates what the agreement fix adds to the
// fast path: one head-byte comparison per concrete field, no plan lookup and
// no allocation. Divide by len(benchSchema) for the per-field figure and
// compare it against BenchmarkMessageToStructMatching.
func BenchmarkWireMatchesFieldGuard(b *testing.B) {
	raw, err := cbor.Marshal(benchMatching)
	if err != nil {
		b.Fatalf("cbor.Marshal failed: %v", err)
	}
	var fields map[string]cbor.RawMessage
	if err := decMode.Unmarshal(raw, &fields); err != nil {
		b.Fatalf("decode failed: %v", err)
	}

	for _, variant := range []struct {
		name       string
		targetType reflect.Type
	}{
		{"plain", reflect.TypeOf(benchTarget{})},
		{"pointer", reflect.TypeOf(benchPointerTarget{})},
	} {
		heads := make([]byte, 0, variant.targetType.NumField())
		types := make([]reflect.Type, 0, variant.targetType.NumField())
		for i := 0; i < variant.targetType.NumField(); i++ {
			field := variant.targetType.Field(i)
			heads = append(heads, fields[field.Tag.Get("cbor")][0])
			types = append(types, field.Type)
		}

		b.Run(variant.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range heads {
					if !wireMatchesField(heads[j], types[j]) {
						b.Fatalf("field %d must take the fast path", j)
					}
				}
			}
		})
	}
}

func BenchmarkMessageToMap(b *testing.B) {
	msg := benchMessage(b, benchMatching)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := msg.ToMap(); got == nil {
			b.Fatal("ToMap returned nil")
		}
	}
}
