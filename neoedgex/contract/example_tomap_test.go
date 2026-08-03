package contract

// Runnable example for ToMap. `go test` executes it and compares its stdout
// with the // Output: block, so it doubles as the drift guard for the decode
// tables in docs/developer-guide.en.md and docs/developer-guide.zh-tw.md.

import (
	"fmt"
	"math"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// incomingMessage stands in for the SDK runtime: it packs payload the way an
// upstream node sends it and attaches the receiving node's input schema,
// producing the Message a handler reads off ctx.Messages().
//
// testutil.NewMessage is the reader-facing way to build such a message, but
// testutil imports this package, so a test inside package contract cannot use
// it. It also cannot express a key the schema declares and the wire omits,
// which the neverSent case below needs.
func incomingMessage(payload map[string]any, schema []PortFieldSchema) Message {
	data, err := cbor.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return NewMessage("upstream-node", "2026-03-31T09:10:11Z", "input1", RawMessage(data), NewDecodePlan(schema), nil)
}

func ExampleMessage_ToMap() {
	msg := incomingMessage(
		map[string]any{
			"flag":               true,
			"i16":                int16(-12345),
			"i32":                int32(-2000000000),
			"i64":                int64(math.MinInt64),
			"u16":                uint16(math.MaxUint16),
			"u32":                uint32(math.MaxUint32),
			"u64":                uint64(math.MaxUint64),
			"name":               "sensor-1",
			"blob":               []byte{0x01, 0x02},
			"singleIntoFloat":    float32(25.34),
			"doubleIntoDouble":   float64(25.34),
			"singleIntoDouble":   float32(25.34),
			"doubleIntoFloat":    float64(25.34),
			"tooBigForFloat":     float64(1e300),
			"undeclared":         float32(25.34),
			"undeclaredNeg":      int64(-1),
			"undeclaredMinInt64": int64(math.MinInt64),
			// 2^64 — no SDK type can hold it, so it arrives as undefined.
			"undeclaredBeyondUint64": new(big.Int).Lsh(big.NewInt(1), 64),
			"undeclaredUnusable":     map[int64]string{1: "a"},
			// Containers are not part of the delivered type universe: the
			// whole value arrives as undefined, never element by element.
			"undeclaredList":   []any{1, 2},
			"undeclaredNested": map[string]any{"k": 1},
		},
		[]PortFieldSchema{
			{Key: "flag", Type: TypeBool},
			{Key: "i16", Type: TypeInt16},
			{Key: "i32", Type: TypeInt32},
			{Key: "i64", Type: TypeInt64},
			{Key: "u16", Type: TypeUint16},
			{Key: "u32", Type: TypeUint32},
			{Key: "u64", Type: TypeUint64},
			{Key: "name", Type: TypeString},
			{Key: "blob", Type: TypeRaw},
			{Key: "singleIntoFloat", Type: TypeFloat},
			{Key: "doubleIntoDouble", Type: TypeDouble},
			{Key: "singleIntoDouble", Type: TypeDouble},
			{Key: "doubleIntoFloat", Type: TypeFloat},
			{Key: "tooBigForFloat", Type: TypeFloat},
			{Key: "neverSent", Type: TypeDouble},
		},
	)

	data := msg.ToMap()

	// ToMap returns a map, so print a fixed key order to keep the output stable.
	for _, key := range []string{
		"flag", "i16", "i32", "i64", "u16", "u32", "u64", "name", "blob",
		"singleIntoFloat", "doubleIntoDouble", "singleIntoDouble", "doubleIntoFloat",
		"tooBigForFloat", "neverSent", "undeclared", "undeclaredNeg",
		"undeclaredMinInt64", "undeclaredBeyondUint64", "undeclaredUnusable",
		"undeclaredList", "undeclaredNested",
	} {
		fmt.Printf("%s: %v (%T)\n", key, data[key], data[key])
	}

	// Output:
	// flag: true (bool)
	// i16: -12345 (int16)
	// i32: -2000000000 (int32)
	// i64: -9223372036854775808 (int64)
	// u16: 65535 (uint16)
	// u32: 4294967295 (uint32)
	// u64: 18446744073709551615 (uint64)
	// name: sensor-1 (string)
	// blob: [1 2] ([]uint8)
	// singleIntoFloat: 25.34 (float32)
	// doubleIntoDouble: 25.34 (float64)
	// singleIntoDouble: 25.34 (float64)
	// doubleIntoFloat: 25.34 (float32)
	// tooBigForFloat: <nil> (<nil>)
	// neverSent: <nil> (<nil>)
	// undeclared: 25.34000015258789 (float64)
	// undeclaredNeg: -1 (int64)
	// undeclaredMinInt64: -9223372036854775808 (int64)
	// undeclaredBeyondUint64: <nil> (<nil>)
	// undeclaredUnusable: <nil> (<nil>)
	// undeclaredList: <nil> (<nil>)
	// undeclaredNested: <nil> (<nil>)
}
