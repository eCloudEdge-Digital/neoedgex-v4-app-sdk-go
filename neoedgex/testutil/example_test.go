package testutil_test

import (
	"fmt"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/testutil"
)

// thermostat is the handler under test: it converts every reading it receives
// and publishes the result downstream.
type thermostat struct{}

func (app *thermostat) Handle(ctx neoedgex.NodeEnv) {
	for msg := range ctx.Messages() {
		reading := msg.ToMap()
		celsius, ok := reading["temperature"].(float64)
		if !ok {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no temperature in %s", msg.Handle))
			continue
		}
		if err := ctx.Publish("output1", map[string]any{"fahrenheit": celsius*9/5 + 32}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}

// The receiving node's input schema decides how each field decodes, so it is
// written next to the values: temperature is declared double and ratio float,
// while note is a tag the schema does not declare and therefore bypasses it.
func ExampleNewMessage() {
	msg := testutil.NewMessage("input1", testutil.Fields{
		"temperature": {Value: 25.5, Type: contract.TypeDouble},
		"ratio":       {Value: 25.34, Type: contract.TypeFloat},
		"note":        {Value: "calibrated", Type: testutil.Undeclared},
	})

	fmt.Printf("handle=%s source=%s timestamp=%s\n", msg.Handle, msg.Source, msg.Timestamp)
	data := msg.ToMap()
	for _, key := range []string{"temperature", "ratio", "note"} {
		fmt.Printf("%s: %v (%T)\n", key, data[key], data[key])
	}

	// Output:
	// handle=input1 source=upstream-node timestamp=2026-01-01T00:00:00.000Z
	// temperature: 25.5 (float64)
	// ratio: 25.34 (float32)
	// note: calibrated (string)
}

// With a node configuration at hand, the input schema is already there: build
// the message from the env, deliver it, run the handler, then read what it
// published.
func ExampleMockNodeEnv_NewMessage() {
	env := &testutil.MockNodeEnv{Config: neoedgex.Node{
		ID: "node-1",
		Data: contract.NodeData{
			Name: "thermostat",
			Inputs: map[string][]contract.PortFieldSchema{
				"input1": {{Key: "temperature", Type: contract.TypeDouble}},
			},
			Outputs: map[string][]contract.PortFieldSchema{
				"output1": {{Key: "fahrenheit", Type: contract.TypeDouble}},
			},
		},
	}}

	env.Deliver(env.NewMessage("input1", map[string]any{"temperature": 25.5}))

	(&thermostat{}).Handle(env)

	published := env.PublishedData[0]
	fmt.Printf("%s %v (reported errors: %d)\n", published.Handle, published.Data, len(env.ReportedErrors))

	// Output:
	// output1 map[fahrenheit:77.9] (reported errors: 0)
}
