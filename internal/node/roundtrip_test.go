package node

// Two-node (upstream -> downstream) round-trip integration tests over the
// CBOR wire. They prove every contract.DataType survives a real
// Instance.Publish -> CBOR envelope -> real runLoop decode -> Message accessor
// trip with full fidelity, and pin the four-quadrant behaviors end to end:
// undefined propagation (D15), the shared cross-type conversion matrix on
// schema mismatch (D13), unknown-tag bypass, and integer-precision guards
// (no 9.22e+18-style float64 corruption).
//
// The rig: a shared in-memory routingMessenger routes a Publish on topic
// "neoedgex/neoflow/out/<sourceID>/<handle>" into every registered downstream
// subscriber channel, tagging the payload with the parsed <handle>. The
// downstream instance's real runLoop performs the real CBOR unmarshal, the
// O(1) map gate and the input-schema injection, then delivers a
// contract.Message on Messages().

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

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
// so the shared routingMessenger can back both instances.
type routingSDK struct {
	ctx       context.Context
	messenger core.MessengerClient
}

func (s *routingSDK) Context() context.Context                { return s.ctx }
func (s *routingSDK) NodeConfigs() []contract.Node            { return nil }
func (s *routingSDK) Messenger() core.MessengerClient         { return s.messenger }
func (s *routingSDK) NewLogger(string) contract.Logger        { return testLogger{} }
func (s *routingSDK) NewHandlerLogger(string) contract.Logger { return testLogger{} }
func (s *routingSDK) Shutdown()                               {}

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
// subscriber, closing the startup race between `go down.runLoop()` (which
// calls AddSubscriber) and the upstream Publish.
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

// Publish routes an outbound neoflow data message to every registered
// subscriber. Topic shape: neoedgex/neoflow/out/<sourceNodeID>/<handle>.
// Heartbeat/error topics are dropped, mirroring a downstream node that only
// subscribes to its wired input topics.
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
			Data:   append([]byte(nil), data...),
		}
	}
	return nil
}

// twoNodeRig builds a shared messenger plus an upstream and downstream
// Instance. Both sides use handle "port": the upstream declares it as an
// output with outputSchema, the downstream as an input with inputSchema —
// letting tests model matched schemas, schema drift and unknown tags.
func twoNodeRig(t *testing.T, outputSchema, inputSchema []contract.PortFieldSchema) (up *Instance, down *Instance) {
	t.Helper()
	messenger := newRoutingMessenger()

	up, err := NewInstance(&routingSDK{ctx: context.Background(), messenger: messenger}, contract.Node{
		ID:   "upstream-node",
		Type: "producer",
		Data: contract.NodeData{
			Name:    "upstream-app",
			Outputs: map[string][]contract.PortFieldSchema{"port": outputSchema},
		},
	})
	if err != nil {
		t.Fatalf("failed to build upstream instance: %v", err)
	}

	down, err = NewInstance(&routingSDK{ctx: context.Background(), messenger: messenger}, contract.Node{
		ID:   "downstream-node",
		Type: "consumer",
		Data: contract.NodeData{
			Name:   "downstream-app",
			Inputs: map[string][]contract.PortFieldSchema{"port": inputSchema},
		},
	})
	if err != nil {
		t.Fatalf("failed to build downstream instance: %v", err)
	}

	go down.runLoop()
	t.Cleanup(down.Shutdown)
	messenger.waitForSubscriber(t)
	return up, down
}

func upstreamPublish(t *testing.T, up *Instance, payload map[string]any) {
	t.Helper()
	if err := up.Publish("port", payload); err != nil {
		t.Fatalf("upstream Publish failed: %v", err)
	}
}

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

// roundTrip publishes one single-field payload through matched schemas and
// returns the downstream ToMap value for the field.
func roundTrip(t *testing.T, fieldType contract.DataType, value any) any {
	t.Helper()
	schema := []contract.PortFieldSchema{{Key: "f", Type: fieldType}}
	up, down := twoNodeRig(t, schema, schema)
	upstreamPublish(t, up, map[string]any{"f": value})
	msg := downstreamReceive(t, down)
	return msg.ToMap()["f"]
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
		{"int64-min", contract.TypeInt64, int64(math.MinInt64), int64(math.MinInt64)},
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
			got := roundTrip(t, tc.fieldType, tc.value)
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.want) {
				t.Fatalf("round-trip mismatch: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestRoundTripIntegerExtremesNoFloatCorruption is the dedicated precision
// guard: int64/uint64 extremes must arrive as exact integers, never as a
// float64 rendered like 9.223372036854776e+18.
func TestRoundTripIntegerExtremesNoFloatCorruption(t *testing.T) {
	schema := []contract.PortFieldSchema{
		{Key: "imax", Type: contract.TypeInt64},
		{Key: "imin", Type: contract.TypeInt64},
		{Key: "umax", Type: contract.TypeUint64},
	}
	up, down := twoNodeRig(t, schema, schema)
	upstreamPublish(t, up, map[string]any{
		"imax": int64(math.MaxInt64),
		"imin": int64(math.MinInt64),
		"umax": uint64(math.MaxUint64),
	})
	got := downstreamReceive(t, down).ToMap()

	if v, ok := got["imax"].(int64); !ok || v != math.MaxInt64 {
		t.Fatalf("imax: got %#v (%T)", got["imax"], got["imax"])
	}
	if v, ok := got["imin"].(int64); !ok || v != math.MinInt64 {
		t.Fatalf("imin: got %#v (%T)", got["imin"], got["imin"])
	}
	if v, ok := got["umax"].(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("umax: got %#v (%T)", got["umax"], got["umax"])
	}
	for k, v := range got {
		if _, isFloat := v.(float64); isFloat {
			t.Fatalf("field %q arrived as float64: %v", k, v)
		}
		if s := fmt.Sprintf("%v", v); strings.Contains(s, "e+") {
			t.Fatalf("field %q rendered in float notation: %s", k, s)
		}
	}
}

// -----------------------------------------------------------------------------
// raw ([]byte): byte-identical round-trip as a native CBOR byte string.
// -----------------------------------------------------------------------------

func TestRoundTripRawBytes(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
	}{
		{"ascii", []byte("payload")},
		// Bytes that are not valid UTF-8 -- must survive the wire exactly.
		{"non-utf8", []byte{0x00, 0xff, 0xfe, 0x80, 0x01}},
		{"empty", []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, contract.TypeRaw, tc.value)
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
// undefined propagation (D15): explicit nil, absent-but-declared, and
// Publish-side conversion failure all arrive as nil downstream.
// -----------------------------------------------------------------------------

func TestRoundTripUndefinedArrivesAsNil(t *testing.T) {
	schema := []contract.PortFieldSchema{
		{Key: "present", Type: contract.TypeInt64},
		{Key: "explicitNil", Type: contract.TypeInt64},
		{Key: "absent", Type: contract.TypeString},
		{Key: "rejected", Type: contract.TypeInt16},
	}
	up, down := twoNodeRig(t, schema, schema)

	upstreamPublish(t, up, map[string]any{
		"present":     int64(42),
		"explicitNil": nil,
		// out of int16 range -> nulled at Publish time (conversion failure)
		"rejected": int64(70000),
	})
	got := downstreamReceive(t, down).ToMap()

	if v, ok := got["present"].(int64); !ok || v != 42 {
		t.Fatalf("present field: got %#v, want int64(42)", got["present"])
	}
	for _, key := range []string{"explicitNil", "absent", "rejected"} {
		v, exists := got[key]
		if !exists {
			t.Fatalf("undefined field %q must be present with nil value", key)
		}
		if v != nil {
			t.Fatalf("field %q did not arrive as nil: %#v", key, v)
		}
	}
}

// -----------------------------------------------------------------------------
// Cross-end schema drift (D13): the downstream input schema differs from the
// upstream output schema — values are normalized through the SAME conversion
// matrix; denied/failed conversions arrive as undefined (nil).
// -----------------------------------------------------------------------------

func TestRoundTripCrossTypeSchemaDrift(t *testing.T) {
	outputSchema := []contract.PortFieldSchema{
		{Key: "trunc", Type: contract.TypeDouble},  // double -> int16: truncated
		{Key: "coerce", Type: contract.TypeString}, // string -> int32: parsed
		{Key: "broken", Type: contract.TypeInt64},  // 70000 -> int16: undefined
		{Key: "widen", Type: contract.TypeInt16},   // int16 -> int64: widened
		{Key: "strRaw", Type: contract.TypeString}, // string -> raw: denied
	}
	inputSchema := []contract.PortFieldSchema{
		{Key: "trunc", Type: contract.TypeInt16},
		{Key: "coerce", Type: contract.TypeInt32},
		{Key: "broken", Type: contract.TypeInt16},
		{Key: "widen", Type: contract.TypeInt64},
		{Key: "strRaw", Type: contract.TypeRaw},
	}
	up, down := twoNodeRig(t, outputSchema, inputSchema)

	upstreamPublish(t, up, map[string]any{
		"trunc":  float64(2.9),
		"coerce": "42",
		"broken": int64(70000),
		"widen":  int16(-5),
		"strRaw": "hi",
	})
	got := downstreamReceive(t, down).ToMap()

	want := map[string]any{
		"trunc":  int16(2),
		"coerce": int32(42),
		"broken": nil,
		"widen":  int64(-5),
		"strRaw": nil,
	}
	for k, w := range want {
		if fmt.Sprintf("%#v", got[k]) != fmt.Sprintf("%#v", w) {
			t.Fatalf("field %q: got %#v, want %#v", k, got[k], w)
		}
	}
}

// -----------------------------------------------------------------------------
// Unknown-tag bypass: keys the downstream schema does not declare pass through
// in the natural domain (positive int <= MaxInt64 -> int64, larger -> uint64).
// -----------------------------------------------------------------------------

func TestRoundTripUnknownTagBypassesInNaturalDomain(t *testing.T) {
	outputSchema := []contract.PortFieldSchema{
		{Key: "known", Type: contract.TypeInt64},
		{Key: "ghost", Type: contract.TypeUint64},
		{Key: "ghostBig", Type: contract.TypeUint64},
	}
	inputSchema := []contract.PortFieldSchema{
		{Key: "known", Type: contract.TypeInt64},
	}
	up, down := twoNodeRig(t, outputSchema, inputSchema)

	upstreamPublish(t, up, map[string]any{
		"known":    int64(1),
		"ghost":    uint64(5),
		"ghostBig": uint64(math.MaxUint64),
	})
	got := downstreamReceive(t, down).ToMap()

	if v, ok := got["known"].(int64); !ok || v != 1 {
		t.Fatalf("known: got %#v", got["known"])
	}
	if v, ok := got["ghost"].(int64); !ok || v != 5 {
		t.Fatalf("ghost must bypass as natural-domain int64: got %#v (%T)", got["ghost"], got["ghost"])
	}
	if v, ok := got["ghostBig"].(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("ghostBig must stay uint64: got %#v (%T)", got["ghostBig"], got["ghostBig"])
	}
}

// -----------------------------------------------------------------------------
// ToStruct across the wire: declared type wins, any takes the schema type,
// pointer keeps the undefined distinction.
// -----------------------------------------------------------------------------

func TestRoundTripToStruct(t *testing.T) {
	schema := []contract.PortFieldSchema{
		{Key: "temp", Type: contract.TypeInt16},
		{Key: "ratio", Type: contract.TypeDouble},
		{Key: "name", Type: contract.TypeString},
		{Key: "absent", Type: contract.TypeDouble},
	}
	up, down := twoNodeRig(t, schema, schema)

	upstreamPublish(t, up, map[string]any{
		"temp":  int16(1234),
		"ratio": float64(2.5),
		"name":  "sensor-A",
	})
	msg := downstreamReceive(t, down)

	type myInput struct {
		Temp   int32    `cbor:"temp"`  // declared wider than schema -> declaration wins
		Ratio  any      `cbor:"ratio"` // any -> schema-typed (double)
		Name   string   `cbor:"name"`
		Absent *float64 `cbor:"absent"` // pointer keeps undefined as nil
	}
	var in myInput
	if err := msg.ToStruct(&in); err != nil {
		t.Fatalf("unexpected ToStruct error: %v", err)
	}
	if in.Temp != 1234 {
		t.Fatalf("Temp: got %v, want 1234 as int32", in.Temp)
	}
	if v, ok := in.Ratio.(float64); !ok || v != 2.5 {
		t.Fatalf("Ratio: got %#v (%T), want float64(2.5)", in.Ratio, in.Ratio)
	}
	if in.Name != "sensor-A" {
		t.Fatalf("Name: got %q", in.Name)
	}
	if in.Absent != nil {
		t.Fatalf("Absent: got %v, want nil pointer", in.Absent)
	}
}

// -----------------------------------------------------------------------------
// Drift guard: every entry in contract.SupportedTypes must be exercised by
// this file. If a new DataType is added, this test fails until a round-trip
// case is added for it above.
// -----------------------------------------------------------------------------

func TestRoundTripCoversEverySupportedType(t *testing.T) {
	covered := map[contract.DataType]struct{}{
		contract.TypeBool:   {},
		contract.TypeInt16:  {},
		contract.TypeInt32:  {},
		contract.TypeInt64:  {},
		contract.TypeUint16: {},
		contract.TypeUint32: {},
		contract.TypeUint64: {},
		contract.TypeFloat:  {},
		contract.TypeDouble: {},
		contract.TypeString: {},
		contract.TypeRaw:    {},
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
