package contract_test

// The "Decoding into a Struct" section of docs/developer-guide.en.md quotes
// ExampleMessage_ToStruct's body verbatim, and docs/developer-guide.zh-tw.md
// quotes the same code with the comments translated; `go test` checks the body
// against its // Output: block, so neither guide can drift from the decoder.
// That is also why this file is package contract_test with the contract.
// prefixes spelled out: the sample has to compile in a reader's own package,
// out of nothing but the SDK's exported API.

import (
	"fmt"
	"math"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/testutil"
)

func ExampleMessage_ToStruct() {
	// msg is one message from ctx.Messages(); testutil builds the same thing in
	// a test. Next to each value is the type the receiving node's input schema
	// declares that key as — temperature and offset double, count int64, ratio
	// float, level double — while testutil.Undeclared marks the keys the schema
	// does not declare at all.
	msg := testutil.NewMessage("input1", testutil.Fields{
		"temperature": {Value: nil, Type: contract.TypeDouble},        // upstream produced no value
		"offset":      {Value: float64(0), Type: contract.TypeDouble}, // upstream produced a real 0
		"count":       {Value: nil, Type: contract.TypeInt64},         // upstream produced no value
		"ratio":       {Value: float32(25.34), Type: contract.TypeFloat},
		"level":       {Value: float64(25.34), Type: contract.TypeDouble},
		"widened":     {Value: float32(25.34), Type: testutil.Undeclared},
		"seq":         {Value: uint64(5), Type: testutil.Undeclared},
		"total":       {Value: uint64(math.MaxUint64), Type: testutil.Undeclared},
		"deviceName":  {Value: "sensor-1", Type: testutil.Undeclared},
		"running":     {Value: true, Type: testutil.Undeclared},
		"payload":     {Value: []byte{0x01, 0x02}, Type: testutil.Undeclared},
	})

	type Reading struct {
		Temperature *float64 `cbor:"temperature"` // pointer: no value -> nil
		Offset      *float64 `cbor:"offset"`      // pointer: real 0 -> pointer to 0
		Count       int64    `cbor:"count"`       // not a pointer: no value -> 0
		Ratio       any      `cbor:"ratio"`       // declared float -> float32
		Level       any      `cbor:"level"`       // declared double -> float64
		Widened     any      `cbor:"widened"`     // not declared -> float64
		Seq         any      `cbor:"seq"`         // not declared -> int64
		Total       any      `cbor:"total"`       // not declared, above int64 -> uint64
		DeviceName  any      `cbor:"deviceName"`  // not declared -> string
		Running     any      `cbor:"running"`     // not declared -> bool
		Payload     any      `cbor:"payload"`     // not declared -> []byte
	}

	var r Reading
	if err := msg.ToStruct(&r); err != nil {
		fmt.Println("cannot decode this message:", err)
		return
	}

	if r.Temperature == nil {
		fmt.Println("Temperature: no value")
	} else {
		fmt.Println("Temperature:", *r.Temperature)
	}
	if r.Offset == nil {
		fmt.Println("Offset: no value")
	} else {
		fmt.Println("Offset:", *r.Offset)
	}
	fmt.Println("Count:", r.Count, "<- no value and a real 0 look the same here")
	fmt.Printf("Ratio: %v (%T)\n", r.Ratio, r.Ratio)
	fmt.Printf("Level: %v (%T)\n", r.Level, r.Level)
	fmt.Printf("Widened: %v (%T)\n", r.Widened, r.Widened)
	fmt.Printf("Seq: %v (%T)\n", r.Seq, r.Seq)
	fmt.Printf("Total: %v (%T)\n", r.Total, r.Total)
	fmt.Printf("DeviceName: %v (%T)\n", r.DeviceName, r.DeviceName)
	fmt.Printf("Running: %v (%T)\n", r.Running, r.Running)
	fmt.Printf("Payload: %v (%T)\n", r.Payload, r.Payload)

	// Output:
	// Temperature: no value
	// Offset: 0
	// Count: 0 <- no value and a real 0 look the same here
	// Ratio: 25.34 (float32)
	// Level: 25.34 (float64)
	// Widened: 25.34000015258789 (float64)
	// Seq: 5 (int64)
	// Total: 18446744073709551615 (uint64)
	// DeviceName: sensor-1 (string)
	// Running: true (bool)
	// Payload: [1 2] ([]uint8)
}
