package node

// This file houses a two-node (upstream -> downstream) round-trip integration
// test. It proves that EVERY contract.DataType survives a real
// Publish -> wire (json) -> decode -> handler trip with full fidelity.
//
// Downstream-injection seam chosen: a real receiving *Instance* driven by a
// shared in-memory routing messenger (routingMessenger). The upstream app calls
// the real Instance.Publish (real NewPortFieldDataWithAny + real json.Marshal);
// the produced bytes are routed by topic -> the downstream instance's
// subscriber channel; the downstream instance's real runLoop performs the real
// json.Unmarshal + real decodeIncomingData and delivers a contract.Message on
// Messages(). This is strictly more faithful than a capture -> unmarshal ->
// decodeIncomingData boundary because it exercises the genuine receive loop and
// handler-dispatch path end to end, and it needs no new production harness --
// routingMessenger is test-only and implements the existing
// core.MessengerClient interface.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// routingMessenger is an in-memory core.MessengerClient that routes a Publish
// on topic "neoedgex/neoflow/out/<sourceID>/<handle>" into the subscriber
// channel registered by any downstream node, tagging the payload with the parsed
// <handle>. Error/heartbeat topics are ignored (they are not "out" messages).
type routingMessenger struct {
	mu          sync.Mutex
	subscribers map[string]chan core.RawMessengerPayload
	// subscribed, when non-nil, is closed as soon as a subscriber registers.
	subscribed chan struct{}
}

func newRoutingMessenger() *routingMessenger {
	return &routingMessenger{subscribers: make(map[string]chan core.RawMessengerPayload)}
}

// routingSDK is a testSDK whose Messenger() returns an interface-typed client,
// so the shared routingMessenger can back both instances. (testSDK.messenger is
// concretely *testMessenger and cannot hold a routingMessenger.)
type routingSDK struct {
	ctx       context.Context
	messenger core.MessengerClient
}

func (s *routingSDK) Context() context.Context         { return s.ctx }
func (s *routingSDK) NodeConfigs() []contract.Node     { return nil }
func (s *routingSDK) Messenger() core.MessengerClient  { return s.messenger }
func (s *routingSDK) NewLogger(string) contract.Logger { return testLogger{} }
func (s *routingSDK) Shutdown()                        {}

func (m *routingMessenger) Connect() error { return nil }
func (m *routingMessenger) Disconnect()    {}

func (m *routingMessenger) AddSubscriber(nodeID string) <-chan core.RawMessengerPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan core.RawMessengerPayload, 16)
	m.subscribers[nodeID] = ch
	if m.subscribed != nil {
		close(m.subscribed)
		m.subscribed = nil
	}
	return ch
}

// waitForSubscriber blocks until at least one downstream node has registered a
// subscriber, closing the startup race between `go down.runLoop()` (which calls
// AddSubscriber) and the upstream Publish.
func (m *routingMessenger) waitForSubscriber(t *testing.T) {
	t.Helper()
	m.mu.Lock()
	if len(m.subscribers) > 0 {
		m.mu.Unlock()
		return
	}
	done := make(chan struct{})
	m.subscribed = done
	m.mu.Unlock()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("downstream node never registered a subscriber")
	}
}

func (m *routingMessenger) RemoveSubscriber(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subscribers, nodeID)
}

// Publish routes an outbound neoflow message to every registered subscriber.
// Topic shape: neoedgex/neoflow/out/<sourceNodeID>/<handle>. Anything that is
// not an "out" message (heartbeat/error) is dropped, mirroring a downstream node
// that only subscribes to its wired input topics.
func (m *routingMessenger) Publish(topic string, _ byte, data []byte) error {
	parts := strings.Split(topic, "/")
	// neoedgex / neoflow / out / <id> / <handle>
	if len(parts) != 5 || parts[2] != "out" {
		return nil
	}
	handle := parts[4]

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subscribers {
		ch <- core.RawMessengerPayload{
			Handle: handle,
			// Copy the bytes so the downstream sees exactly the wire payload.
			Data: append([]byte(nil), data...),
		}
	}
	return nil
}

// twoNodeRig builds a shared messenger plus an upstream and downstream Instance.
// The downstream declares an input handle so the receive loop forwards messages;
// the upstream declares an output handle carrying exactly the given schema.
func twoNodeRig(t *testing.T, outputSchema []contract.PortFieldSchema, downstreamRawJson bool) (up *Instance, down *Instance, messenger *routingMessenger) {
	t.Helper()
	messenger = newRoutingMessenger()

	up, err := NewInstance(&routingSDK{ctx: context.Background(), messenger: messenger}, contract.Node{
		ID:   "upstream-node",
		Type: "producer",
		Data: contract.NodeData{
			Name:    "upstream-app",
			Outputs: map[string][]contract.PortFieldSchema{"out": outputSchema},
		},
	}, false)
	if err != nil {
		t.Fatalf("failed to build upstream instance: %v", err)
	}

	down, err = NewInstance(&routingSDK{ctx: context.Background(), messenger: messenger}, contract.Node{
		ID:   "downstream-node",
		Type: "consumer",
		Data: contract.NodeData{
			Name:   "downstream-app",
			Inputs: map[string][]contract.PortFieldSchema{"in": {}},
		},
	}, downstreamRawJson)
	if err != nil {
		t.Fatalf("failed to build downstream instance: %v", err)
	}

	go down.runLoop()
	t.Cleanup(down.Shutdown)
	messenger.waitForSubscriber(t)
	return up, down, messenger
}

// upstreamPublish is the "upstream app": it Publishes a payload on handle "out".
func upstreamPublish(t *testing.T, up *Instance, payload map[string]any) {
	t.Helper()
	if err := up.Publish("out", payload); err != nil {
		t.Fatalf("upstream Publish failed: %v", err)
	}
}

// downstreamReceive is the "downstream app": it waits for the decoded Message the
// real receive loop delivers and returns its Data map for assertion.
func downstreamReceive(t *testing.T, down *Instance) contract.Message {
	t.Helper()
	select {
	case msg := <-down.Messages():
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("downstream app did not receive the published message in time")
		return contract.Message{}
	}
}

// roundTrip runs one upstream Publish -> downstream receive using a single-field
// output schema, and returns the decoded downstream value for the field.
func roundTrip(t *testing.T, fieldType contract.DataType, value any, downstreamRawJson bool) any {
	t.Helper()
	up, down, _ := twoNodeRig(t, []contract.PortFieldSchema{{Key: "f", Type: fieldType}}, downstreamRawJson)
	upstreamPublish(t, up, map[string]any{"f": value})
	msg := downstreamReceive(t, down)
	return msg.Data["f"]
}

// -----------------------------------------------------------------------------
// Scalar matrix: exact typed value + concrete Go type must round-trip.
// -----------------------------------------------------------------------------

func TestRoundTripScalars(t *testing.T) {
	// want is asserted with %#v so both the concrete Go type and value are
	// compared -- e.g. int16(1) != int32(1) != int64(1).
	cases := []struct {
		name      string
		fieldType contract.DataType
		value     any
		want      any
	}{
		{"bool-true", contract.TypeBool, true, true},
		{"bool-false", contract.TypeBool, false, false},

		{"int16-neg", contract.TypeInt16, int16(-12345), int16(-12345)},
		{"int16-max", contract.TypeInt16, int16(math.MaxInt16), int16(math.MaxInt16)},
		{"int32-neg", contract.TypeInt32, int32(-2000000000), int32(-2000000000)},
		{"int32-max", contract.TypeInt32, int32(math.MaxInt32), int32(math.MaxInt32)},
		{"int64-neg", contract.TypeInt64, int64(-9000000000000000000), int64(-9000000000000000000)},
		{"int64-max", contract.TypeInt64, int64(math.MaxInt64), int64(9223372036854775807)},

		{"uint16-max", contract.TypeUint16, uint16(math.MaxUint16), uint16(math.MaxUint16)},
		{"uint32-max", contract.TypeUint32, uint32(math.MaxUint32), uint32(math.MaxUint32)},
		{"uint64-max", contract.TypeUint64, uint64(math.MaxUint64), uint64(18446744073709551615)},

		// float32 exactly representable, and one that exposes float32 precision.
		{"float-exact", contract.TypeFloat, float32(1.5), float32(1.5)},
		{"float-precision", contract.TypeFloat, float32(0.1), float32(0.1)},

		{"double", contract.TypeDouble, float64(3.141592653589793), float64(3.141592653589793)},

		{"string", contract.TypeString, "hello", "hello"},
		{"string-empty", contract.TypeString, "", ""},
		{"string-unicode", contract.TypeString, "héllo 世界 🌍", "héllo 世界 🌍"},
		// numeric-looking string must NOT be coerced to a number.
		{"string-numeric-like", contract.TypeString, "007", "007"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.fieldType, tc.value, false)
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.want) {
				t.Fatalf("round-trip mismatch: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// raw ([]byte): byte-identical round-trip through the base64 wire encoding.
// -----------------------------------------------------------------------------

func TestRoundTripRawBytes(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
	}{
		{"ascii", []byte("payload")},
		// Bytes that are not valid UTF-8 -- must survive base64 exactly.
		{"non-utf8", []byte{0x00, 0xff, 0xfe, 0x80, 0x01}},
		{"empty", []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, contract.TypeRaw, tc.value, false)
			gotBytes, ok := got.([]byte)
			if !ok {
				t.Fatalf("expected []byte downstream, got %T (%#v)", got, got)
			}
			if !bytes.Equal(gotBytes, tc.value) {
				t.Fatalf("raw bytes not byte-identical: got %v, want %v", gotBytes, tc.value)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// jsonObject / jsonArray -- default (parsed) mode: downstream gets map/[]any.
// -----------------------------------------------------------------------------

func TestRoundTripJsonObjectDefaultMode(t *testing.T) {
	input := json.RawMessage(`{"name":"widget","count":3,"nested":{"ok":true},"list":[1,2]}`)
	got := roundTrip(t, contract.TypeJsonObject, input, false)

	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any downstream, got %T (%#v)", got, got)
	}
	if obj["name"] != "widget" {
		t.Fatalf("unexpected name: %#v", obj["name"])
	}
	// Default mode uses encoding/json, so numbers land as float64.
	if obj["count"] != float64(3) {
		t.Fatalf("unexpected count: %#v", obj["count"])
	}
	nested, ok := obj["nested"].(map[string]any)
	if !ok || nested["ok"] != true {
		t.Fatalf("unexpected nested object: %#v", obj["nested"])
	}
	list, ok := obj["list"].([]any)
	if !ok || len(list) != 2 || list[0] != float64(1) || list[1] != float64(2) {
		t.Fatalf("unexpected nested list: %#v", obj["list"])
	}
}

func TestRoundTripJsonArrayDefaultMode(t *testing.T) {
	input := json.RawMessage(`["a",2,{"k":"v"},[9,8]]`)
	got := roundTrip(t, contract.TypeJsonArray, input, false)

	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("expected []any downstream, got %T (%#v)", got, got)
	}
	if len(arr) != 4 {
		t.Fatalf("unexpected array length: %#v", arr)
	}
	if arr[0] != "a" || arr[1] != float64(2) {
		t.Fatalf("unexpected scalar elements: %#v", arr)
	}
	if m, ok := arr[2].(map[string]any); !ok || m["k"] != "v" {
		t.Fatalf("unexpected object element: %#v", arr[2])
	}
	if inner, ok := arr[3].([]any); !ok || len(inner) != 2 || inner[0] != float64(9) {
		t.Fatalf("unexpected inner array: %#v", arr[3])
	}
}

func TestRoundTripEmptyJsonContainersDefaultMode(t *testing.T) {
	t.Run("empty-object", func(t *testing.T) {
		got := roundTrip(t, contract.TypeJsonObject, json.RawMessage(`{}`), false)
		obj, ok := got.(map[string]any)
		if !ok || len(obj) != 0 {
			t.Fatalf("expected empty map[string]any, got %T (%#v)", got, got)
		}
	})
	t.Run("empty-array", func(t *testing.T) {
		got := roundTrip(t, contract.TypeJsonArray, json.RawMessage(`[]`), false)
		arr, ok := got.([]any)
		if !ok || len(arr) != 0 {
			t.Fatalf("expected empty []any, got %T (%#v)", got, got)
		}
	})
}

// -----------------------------------------------------------------------------
// jsonObject / jsonArray -- raw mode: downstream gets json.RawMessage byte-exact,
// including a >2^53 integer that must NOT be rounded through float64.
// -----------------------------------------------------------------------------

func TestRoundTripJsonRawModeByteExactBigInt(t *testing.T) {
	const rounded = "9223372036854776000" // the float64-corrupted form we must NOT see

	t.Run("object-nested-big-int", func(t *testing.T) {
		input := json.RawMessage(`{"id":9223372036854775807,"nested":{"big":9223372036854775807}}`)
		got := roundTrip(t, contract.TypeJsonObject, input, true)

		raw, ok := got.(json.RawMessage)
		if !ok {
			t.Fatalf("expected json.RawMessage downstream in raw mode, got %T (%#v)", got, got)
		}
		if !bytes.Equal(raw, input) {
			t.Fatalf("raw jsonObject not byte-exact:\n got  %q\n want %q", string(raw), string(input))
		}
		if strings.Contains(string(raw), rounded) {
			t.Fatalf("big integer was rounded through float64: %q", string(raw))
		}
	})

	t.Run("array-big-int", func(t *testing.T) {
		input := json.RawMessage(`[9223372036854775807,1,9223372036854775807]`)
		got := roundTrip(t, contract.TypeJsonArray, input, true)

		raw, ok := got.(json.RawMessage)
		if !ok {
			t.Fatalf("expected json.RawMessage downstream in raw mode, got %T (%#v)", got, got)
		}
		if !bytes.Equal(raw, input) {
			t.Fatalf("raw jsonArray not byte-exact:\n got  %q\n want %q", string(raw), string(input))
		}
		if strings.Contains(string(raw), rounded) {
			t.Fatalf("big integer was rounded through float64: %q", string(raw))
		}
	})

	t.Run("empty-object", func(t *testing.T) {
		input := json.RawMessage(`{}`)
		got := roundTrip(t, contract.TypeJsonObject, input, true)
		raw, ok := got.(json.RawMessage)
		if !ok || !bytes.Equal(raw, input) {
			t.Fatalf("expected byte-exact empty object, got %T (%#v)", got, got)
		}
	})

	t.Run("empty-array", func(t *testing.T) {
		input := json.RawMessage(`[]`)
		got := roundTrip(t, contract.TypeJsonArray, input, true)
		raw, ok := got.(json.RawMessage)
		if !ok || !bytes.Equal(raw, input) {
			t.Fatalf("expected byte-exact empty array, got %T (%#v)", got, got)
		}
	})
}

// -----------------------------------------------------------------------------
// nil round-trips as nil: an explicit nil field, and a field defined in the
// schema but absent from the published data, both arrive as decoded nil.
// -----------------------------------------------------------------------------

func TestRoundTripNilFieldDecodesAsNil(t *testing.T) {
	schema := []contract.PortFieldSchema{
		{Key: "present", Type: contract.TypeInt64},
		{Key: "explicitNil", Type: contract.TypeInt64},
		{Key: "absent", Type: contract.TypeString},
	}
	up, down, _ := twoNodeRig(t, schema, false)

	// "present" carries a real value; "explicitNil" is published as nil;
	// "absent" is defined in the schema but omitted from the data map.
	upstreamPublish(t, up, map[string]any{
		"present":     int64(42),
		"explicitNil": nil,
	})
	msg := downstreamReceive(t, down)

	if got, ok := msg.Data["present"].(int64); !ok || got != 42 {
		t.Fatalf("present field: got %#v, want int64(42)", msg.Data["present"])
	}
	if v, exists := msg.Data["explicitNil"]; !exists || v != nil {
		t.Fatalf("explicit nil did not round-trip as nil: exists=%v value=%#v", exists, v)
	}
	if v, exists := msg.Data["absent"]; !exists || v != nil {
		t.Fatalf("absent-but-declared field did not round-trip as nil: exists=%v value=%#v", exists, v)
	}
}

// -----------------------------------------------------------------------------
// Matrix drift guard: every entry in contract.SupportedTypes must be exercised
// by this file. If a new DataType is added, this test fails until a round-trip
// case is added for it above.
// -----------------------------------------------------------------------------

func TestRoundTripCoversEverySupportedType(t *testing.T) {
	// Types with dedicated round-trip cases in this file.
	covered := map[contract.DataType]struct{}{
		contract.TypeBool:       {},
		contract.TypeInt16:      {},
		contract.TypeInt32:      {},
		contract.TypeInt64:      {},
		contract.TypeUint16:     {},
		contract.TypeUint32:     {},
		contract.TypeUint64:     {},
		contract.TypeFloat:      {},
		contract.TypeDouble:     {},
		contract.TypeString:     {},
		contract.TypeRaw:        {},
		contract.TypeJsonObject: {},
		contract.TypeJsonArray:  {},
	}

	for dt := range contract.SupportedTypes {
		if _, ok := covered[dt]; !ok {
			t.Fatalf("DataType %q is in contract.SupportedTypes but has no round-trip case; add one to roundtrip_test.go", dt)
		}
	}
	if len(covered) != len(contract.SupportedTypes) {
		t.Fatalf("coverage set (%d) and SupportedTypes (%d) disagree; a covered type may have been removed",
			len(covered), len(contract.SupportedTypes))
	}
}
