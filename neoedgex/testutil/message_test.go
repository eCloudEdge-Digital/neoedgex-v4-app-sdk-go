package testutil

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/logger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// runtimeInboundMessage reproduces the production round trip a delivered
// message goes through: the sending node marshals the data map and the
// envelope, the receiving node unmarshals the envelope, gates the data section
// and assembles the Message with the receiving handle's decode plan.
func runtimeInboundMessage(t *testing.T, handle string, schema []contract.PortFieldSchema, payload map[string]any) contract.Message {
	t.Helper()

	dataBytes, err := cbor.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal data map: %v", err)
	}
	envelope, err := cbor.Marshal(contract.NeoFlowMessage{
		SourceNodeID: messageSource,
		Timestamp:    messageTimestamp,
		Data:         dataBytes,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var decoded contract.NeoFlowMessage
	if err := cbor.Unmarshal(envelope, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if len(decoded.Data) == 0 || decoded.Data[0]&0xe0 != 0xa0 {
		t.Fatalf("data section is not a CBOR map")
	}

	return contract.NewMessage(
		decoded.SourceNodeID,
		decoded.Timestamp,
		handle,
		contract.RawMessage(decoded.Data),
		contract.NewDecodePlan(schema),
		logger.NewNoopLogger(),
	)
}

func rawFields(t *testing.T, data contract.RawMessage) map[string]cbor.RawMessage {
	t.Helper()
	var fields map[string]cbor.RawMessage
	if err := cbor.Unmarshal([]byte(data), &fields); err != nil {
		t.Fatalf("decode data section: %v", err)
	}
	return fields
}

func TestNewMessageMatchesRuntimeInboundPath(t *testing.T) {
	payload := map[string]any{
		"temperature": float32(25.34),
		"level":       float64(25.34),
		"count":       int16(-12345),
		"total":       uint64(math.MaxUint64),
		"name":        "sensor-1",
		"blob":        []byte{0x01, 0x02},
		"running":     true,
		"missing":     nil,
		"extra":       float32(25.34),
	}
	schema := []contract.PortFieldSchema{
		{Key: "temperature", Type: contract.TypeDouble},
		{Key: "level", Type: contract.TypeFloat},
		{Key: "count", Type: contract.TypeInt16},
		{Key: "total", Type: contract.TypeUint64},
		{Key: "name", Type: contract.TypeString},
		{Key: "blob", Type: contract.TypeRaw},
		{Key: "running", Type: contract.TypeBool},
		{Key: "missing", Type: contract.TypeDouble},
	}

	want := runtimeInboundMessage(t, "input1", schema, payload)
	got := NewMessage("input1", Fields{
		"temperature": {Value: float32(25.34), Type: contract.TypeDouble},
		"level":       {Value: float64(25.34), Type: contract.TypeFloat},
		"count":       {Value: int16(-12345), Type: contract.TypeInt16},
		"total":       {Value: uint64(math.MaxUint64), Type: contract.TypeUint64},
		"name":        {Value: "sensor-1", Type: contract.TypeString},
		"blob":        {Value: []byte{0x01, 0x02}, Type: contract.TypeRaw},
		"running":     {Value: true, Type: contract.TypeBool},
		"missing":     {Value: nil, Type: contract.TypeDouble},
		"extra":       {Value: float32(25.34), Type: Undeclared},
	})

	if got.Source != want.Source || got.Timestamp != want.Timestamp || got.Handle != want.Handle {
		t.Fatalf("envelope mismatch: got %+v, want source=%q timestamp=%q handle=%q",
			got, want.Source, want.Timestamp, want.Handle)
	}
	if got.Timestamp == "" || got.Source == "" {
		t.Fatalf("envelope fields must be populated, got source=%q timestamp=%q", got.Source, got.Timestamp)
	}

	// Per-key wire bytes, because CBOR map key order is not deterministic.
	gotRaw, wantRaw := rawFields(t, got.Data), rawFields(t, want.Data)
	if len(gotRaw) != len(wantRaw) {
		t.Fatalf("wire keys mismatch: got %v, want %v", gotRaw, wantRaw)
	}
	for key, wantBytes := range wantRaw {
		if !reflect.DeepEqual(gotRaw[key], wantBytes) {
			t.Errorf("wire bytes for %q: got % x, want % x", key, gotRaw[key], wantBytes)
		}
	}

	if !reflect.DeepEqual(got.ToMap(), want.ToMap()) {
		t.Fatalf("ToMap mismatch:\n got %#v\nwant %#v", got.ToMap(), want.ToMap())
	}
}

func TestNewMessageDeliversSchemaTypes(t *testing.T) {
	msg := NewMessage("input1", Fields{
		"asFloat":  {Value: 25.5, Type: contract.TypeFloat},
		"asDouble": {Value: 25.5, Type: contract.TypeDouble},
		"asInt16":  {Value: 25, Type: contract.TypeInt16},
		"asUint64": {Value: uint64(5), Type: contract.TypeUint64},
		"asString": {Value: "sensor-1", Type: contract.TypeString},
		"asBool":   {Value: true, Type: contract.TypeBool},
		"asRaw":    {Value: []byte{0x01}, Type: contract.TypeRaw},
	})

	data := msg.ToMap()
	want := map[string]any{
		"asFloat":  float32(25.5),
		"asDouble": float64(25.5),
		"asInt16":  int16(25),
		"asUint64": uint64(5),
		"asString": "sensor-1",
		"asBool":   true,
		"asRaw":    []byte{0x01},
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("ToMap mismatch:\n got %#v\nwant %#v", data, want)
	}
	if _, ok := data["asFloat"].(float32); !ok {
		t.Fatalf("a value declared float must arrive as float32, got %T", data["asFloat"])
	}
}

// TestNewMessageAttachesThePlan is the regression guard for the trap the
// builder exists to prevent: the same wire bytes decode differently without a
// decode plan, so a message built plan-less would test the handler against
// types production never delivers.
func TestNewMessageAttachesThePlan(t *testing.T) {
	fields := Fields{
		"ratio": {Value: float32(25.34), Type: contract.TypeFloat},
		"seq":   {Value: uint64(5), Type: contract.TypeUint64},
	}
	planned := NewMessage("input1", fields)

	// Same bytes, no plan: exactly what a composite-literal Message gives.
	planless := contract.NewMessage(planned.Source, planned.Timestamp, planned.Handle, planned.Data, nil, nil)

	plannedMap, planlessMap := planned.ToMap(), planless.ToMap()
	if reflect.DeepEqual(plannedMap, planlessMap) {
		t.Fatalf("plan is not attached: plan-less decode is identical: %#v", plannedMap)
	}
	if _, ok := plannedMap["ratio"].(float32); !ok {
		t.Errorf("declared float must decode as float32, got %T", plannedMap["ratio"])
	}
	if _, ok := planlessMap["ratio"].(float64); !ok {
		t.Errorf("plan-less decode must widen to float64, got %T", planlessMap["ratio"])
	}
	if _, ok := plannedMap["seq"].(uint64); !ok {
		t.Errorf("declared uint64 must decode as uint64, got %T", plannedMap["seq"])
	}
	if _, ok := planlessMap["seq"].(int64); !ok {
		t.Errorf("plan-less decode must narrow to int64, got %T", planlessMap["seq"])
	}

	// Undeclared reproduces the plan-less behaviour for a single key.
	bypassed := NewMessage("input1", Fields{
		"ratio": {Value: float32(25.34), Type: Undeclared},
		"seq":   {Value: uint64(5), Type: Undeclared},
	})
	if !reflect.DeepEqual(bypassed.ToMap(), planlessMap) {
		t.Fatalf("Undeclared must match the bypass path:\n got %#v\nwant %#v", bypassed.ToMap(), planlessMap)
	}
}

func TestNewMessageNilValueIsUndefined(t *testing.T) {
	msg := NewMessage("input1", Fields{"temperature": {Value: nil, Type: contract.TypeDouble}})

	raw := rawFields(t, msg.Data)
	if len(raw["temperature"]) != 1 || raw["temperature"][0] != 0xf6 {
		t.Fatalf("nil must be encoded as CBOR null, got % x", raw["temperature"])
	}

	data := msg.ToMap()
	value, present := data["temperature"]
	if !present || value != nil {
		t.Fatalf("expected the key present and nil, got %#v (present=%v)", value, present)
	}
}

func TestNewMessageDataSectionPassesTheRuntimeWireGate(t *testing.T) {
	for name, msg := range map[string]contract.Message{
		"empty": NewMessage("input1", nil),
		"populated": NewMessage("input1", Fields{
			"temperature": {Value: 25.5, Type: contract.TypeDouble},
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if len(msg.Data) == 0 || msg.Data[0]&0xe0 != 0xa0 {
				t.Fatalf("data section must be a CBOR map, got % x", msg.Data)
			}
		})
	}
}

func TestNewMessageEnvelopeFieldsStayAssignable(t *testing.T) {
	msg := NewMessage("input1", Fields{"ratio": {Value: float32(25.34), Type: contract.TypeFloat}})
	msg.Source = "node-42"
	msg.Timestamp = "2026-03-31T09:10:11Z"

	if msg.Source != "node-42" || msg.Timestamp != "2026-03-31T09:10:11Z" {
		t.Fatalf("envelope not assignable: %+v", msg)
	}
	if _, ok := msg.ToMap()["ratio"].(float32); !ok {
		t.Fatalf("reassigning the envelope must not drop the plan, got %T", msg.ToMap()["ratio"])
	}
}

func TestNewMessagePanics(t *testing.T) {
	tests := map[string]struct {
		build func()
		want  string
	}{
		"missing type": {
			build: func() { NewMessage("input1", Fields{"temperature": {Value: 25.5}}) },
			want:  `field "temperature": no Type declared`,
		},
		"unsupported type": {
			build: func() {
				NewMessage("input1", Fields{"temperature": {Value: 25.5, Type: contract.DataType("float128")}})
			},
			want: `field "temperature": type "float128" is not a declarable schema type`,
		},
		"unencodable value": {
			build: func() {
				NewMessage("input1", Fields{
					"ok":      {Value: 1, Type: contract.TypeInt16},
					"broken":  {Value: func() {}, Type: contract.TypeString},
					"broken2": {Value: make(chan int), Type: contract.TypeString},
				})
			},
			want: `field "broken": value of type func() cannot be CBOR-encoded`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertPanics(t, test.want, test.build)
		})
	}
}

func TestWithLoggerReceivesDecodeFailures(t *testing.T) {
	recorder := &recordingLogger{}
	msg := NewMessage("input1",
		Fields{"count": {Value: "not a number", Type: contract.TypeInt16}},
		WithLogger(recorder))

	if value := msg.ToMap()["count"]; value != nil {
		t.Fatalf("unconvertible value must be delivered as undefined, got %#v", value)
	}
	if len(recorder.warnings) != 1 || !strings.Contains(recorder.warnings[0], `Field "count"`) {
		t.Fatalf("expected one warning naming the field, got %#v", recorder.warnings)
	}
}

func TestMockNodeEnvNewMessageUsesTheConfiguredSchema(t *testing.T) {
	env := &MockNodeEnv{Config: nodeConfig()}

	msg := env.NewMessage("input1", map[string]any{
		"temperature": 25.5,
		"ratio":       25.34,
		"extra":       float32(25.34),
	})

	if msg.Handle != "input1" {
		t.Fatalf("handle mismatch: %q", msg.Handle)
	}
	data := msg.ToMap()
	want := map[string]any{
		"temperature": float64(25.5),
		"ratio":       float32(25.34),
		"pressure":    nil,                     // declared, never sent
		"extra":       float64(float32(25.34)), // sent, not declared: bypass widens
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("ToMap mismatch:\n got %#v\nwant %#v", data, want)
	}
}

func TestMockNodeEnvNewMessageMatchesTheStandaloneBuilder(t *testing.T) {
	env := &MockNodeEnv{Config: nodeConfig()}

	fromConfig := env.NewMessage("input1", map[string]any{"temperature": 25.5, "ratio": 25.34})
	spelledOut := NewMessage("input1", Fields{
		"temperature": {Value: 25.5, Type: contract.TypeDouble},
		"ratio":       {Value: 25.34, Type: contract.TypeFloat},
		"pressure":    {Value: nil, Type: contract.TypeDouble},
	})

	if !reflect.DeepEqual(fromConfig.ToMap(), spelledOut.ToMap()) {
		t.Fatalf("ToMap mismatch:\n got %#v\nwant %#v", fromConfig.ToMap(), spelledOut.ToMap())
	}
}

func TestMockNodeEnvNewMessageUsesTheEnvLogger(t *testing.T) {
	recorder := &recordingLogger{}
	env := &MockNodeEnv{Config: nodeConfig(), MockLogger: recorder}

	msg := env.NewMessage("input1", map[string]any{"temperature": "not a number"})

	if value := msg.ToMap()["temperature"]; value != nil {
		t.Fatalf("unconvertible value must be delivered as undefined, got %#v", value)
	}
	if len(recorder.warnings) != 1 || !strings.Contains(recorder.warnings[0], `Field "temperature"`) {
		t.Fatalf("expected one warning naming the field, got %#v", recorder.warnings)
	}
}

func TestMockNodeEnvNewMessagePanicsOnAnUndeclaredHandle(t *testing.T) {
	env := &MockNodeEnv{Config: nodeConfig()}

	assertPanics(t, `handle "input9" is not declared in Config.Data.Inputs (declared: [input1 input2])`, func() {
		env.NewMessage("input9", map[string]any{"temperature": 25.5})
	})
}

func TestMockNodeEnvDeliverQueuesInOrderAndCloses(t *testing.T) {
	env := &MockNodeEnv{Config: nodeConfig()}

	env.Deliver(
		env.NewMessage("input1", map[string]any{"temperature": 1.5}),
		env.NewMessage("input2", map[string]any{"running": true}),
	)

	var handles []string
	for msg := range env.Messages() {
		handles = append(handles, msg.Handle)
	}
	if !reflect.DeepEqual(handles, []string{"input1", "input2"}) {
		t.Fatalf("expected both messages in order, got %v", handles)
	}
}

func TestMockNodeEnvDeliverNothingClosesImmediately(t *testing.T) {
	env := &MockNodeEnv{}

	env.Deliver()

	for range env.Messages() {
		t.Fatal("expected no messages")
	}
}

func nodeConfig() contract.Node {
	return contract.Node{
		ID:   "node-1",
		Type: "application",
		Data: contract.NodeData{
			Name: "example",
			Inputs: map[string][]contract.PortFieldSchema{
				"input1": {
					{Key: "temperature", Type: contract.TypeDouble},
					{Key: "ratio", Type: contract.TypeFloat},
					{Key: "pressure", Type: contract.TypeDouble},
				},
				"input2": {{Key: "running", Type: contract.TypeBool}},
			},
		},
	}
}

func assertPanics(t *testing.T, want string, build func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected a panic containing %q, got none", want)
		}
		if got := fmt.Sprint(recovered); !strings.Contains(got, want) {
			t.Fatalf("panic %q does not contain %q", got, want)
		}
	}()
	build()
}

type recordingLogger struct {
	warnings []string
}

func (l *recordingLogger) Debug(string, ...any) {}
func (l *recordingLogger) Info(string, ...any)  {}
func (l *recordingLogger) Error(string, ...any) {}
func (l *recordingLogger) Warn(msg string, args ...any) {
	l.warnings = append(l.warnings, fmt.Sprintf(msg, args...))
}
