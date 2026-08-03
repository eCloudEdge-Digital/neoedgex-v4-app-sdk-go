package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

type testLogger struct{}

func (testLogger) Debug(string, ...any) {}
func (testLogger) Info(string, ...any)  {}
func (testLogger) Warn(string, ...any)  {}
func (testLogger) Error(string, ...any) {}

type publishRecord struct {
	topic string
	qos   byte
	data  []byte
}

type testMessenger struct {
	mu              sync.Mutex
	subscriber      chan core.RawMessengerPayload
	published       []publishRecord
	removedNodeID   string
	addedSubscriber string
}

func (m *testMessenger) Connect() error { return nil }

func (m *testMessenger) Disconnect() {}

func (m *testMessenger) AddSubscriber(nodeID string) <-chan core.RawMessengerPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedSubscriber = nodeID
	return m.subscriber
}

func (m *testMessenger) RemoveSubscriber(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedNodeID = nodeID
}

func (m *testMessenger) Publish(topic string, qos byte, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, publishRecord{
		topic: topic,
		qos:   qos,
		data:  append([]byte(nil), data...),
	})
	return nil
}

// last returns the most recent publish (topic, data); fails if none happened.
func (m *testMessenger) last(t *testing.T) publishRecord {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.published) == 0 {
		t.Fatal("expected at least one publish")
	}
	return m.published[len(m.published)-1]
}

// topics returns every published topic in order.
func (m *testMessenger) topics() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.published))
	for i, p := range m.published {
		out[i] = p.topic
	}
	return out
}

type testSDK struct {
	ctx       context.Context
	messenger *testMessenger
}

func (s *testSDK) Context() context.Context { return s.ctx }

func (s *testSDK) NodeConfigs() []contract.Node { return nil }

func (s *testSDK) Messenger() core.MessengerClient { return s.messenger }

func (s *testSDK) NewLogger(string) contract.Logger { return testLogger{} }

func (s *testSDK) NewHandlerLogger(string) contract.Logger { return testLogger{} }

func (s *testSDK) Shutdown() {}

// newTestInstance builds an Instance over a recording messenger.
func newTestInstance(t *testing.T, data contract.NodeData) (*Instance, *testMessenger) {
	t.Helper()
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload, 4)}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{ID: "node-1", Type: "demo", Data: data})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return instance, messenger
}

// decodeWire decodes a published CBOR wire payload into the envelope plus the
// per-field raw segments of the data map.
func decodeWire(t *testing.T, payload []byte) (contract.NeoFlowMessage, map[string]cbor.RawMessage) {
	t.Helper()
	var message contract.NeoFlowMessage
	if err := cbor.Unmarshal(payload, &message); err != nil {
		t.Fatalf("published payload is not a CBOR envelope: %v", err)
	}
	var fields map[string]cbor.RawMessage
	if err := cbor.Unmarshal(message.Data, &fields); err != nil {
		t.Fatalf("published data segment is not a CBOR map: %v", err)
	}
	return message, fields
}

func TestNewInstanceCreatesInstance(t *testing.T) {
	instance, _ := newTestInstance(t, contract.NodeData{Name: "demo-node"})
	if instance.NodeConfig().ID != "node-1" {
		t.Fatalf("unexpected node config ID: %s", instance.NodeConfig().ID)
	}
}

func TestPublishSendsCBOREnvelopeWithSchemaTypedValues(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "value", Type: contract.TypeString},
			},
		},
	})

	// int64 into a string field: converted by the matrix at Publish time.
	if err := instance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}

	rec := messenger.last(t)
	if rec.topic != "neoedgex/neoflow/out/node-1/output1" {
		t.Fatalf("unexpected published topic: %s", rec.topic)
	}
	if rec.qos != 2 {
		t.Fatalf("unexpected QoS: %d", rec.qos)
	}

	message, fields := decodeWire(t, rec.data)
	if message.SourceNodeID != "node-1" {
		t.Fatalf("unexpected source: %s", message.SourceNodeID)
	}
	if message.Timestamp == "" {
		t.Fatal("expected published message timestamp to be set")
	}
	if _, err := time.Parse(time.RFC3339, message.Timestamp); err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q: %v", message.Timestamp, err)
	}

	var value string
	if err := cbor.Unmarshal(fields["value"], &value); err != nil {
		t.Fatalf("value field is not a CBOR text string: %v", err)
	}
	if value != "7" {
		t.Fatalf("unexpected value: %q", value)
	}
}

func TestPublishUnknownHandleFails(t *testing.T) {
	instance, _ := newTestInstance(t, contract.NodeData{Name: "demo-node"})
	if err := instance.Publish("nope", map[string]any{"v": 1}); err == nil {
		t.Fatal("expected error for unknown output handle")
	}
}

func TestPublishFillsMissingOutputFieldWithCBORNull(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "value", Type: contract.TypeInt64},
				{Key: "status", Type: contract.TypeString},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish to succeed with missing field, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	raw, exists := fields["status"]
	if !exists {
		t.Fatal("expected declared-but-missing field to be present on the wire")
	}
	if len(raw) != 1 || raw[0] != 0xf6 {
		t.Fatalf("expected CBOR null (0xf6) for missing field, got % x", []byte(raw))
	}
}

func TestPublishTreatsMissingAndNilFieldsEquivalently(t *testing.T) {
	outputs := map[string][]contract.PortFieldSchema{
		"output1": {
			{Key: "value", Type: contract.TypeInt64},
			{Key: "status", Type: contract.TypeString},
		},
	}

	missingInstance, missingMessenger := newTestInstance(t, contract.NodeData{Name: "demo-node", Outputs: outputs})
	if err := missingInstance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish with missing field to succeed, got: %v", err)
	}

	nilInstance, nilMessenger := newTestInstance(t, contract.NodeData{Name: "demo-node", Outputs: outputs})
	if err := nilInstance.Publish("output1", map[string]any{
		"value":  int64(7),
		"status": nil,
	}); err != nil {
		t.Fatalf("expected Publish with explicit nil field to succeed, got: %v", err)
	}

	_, missingFields := decodeWire(t, missingMessenger.last(t).data)
	_, nilFields := decodeWire(t, nilMessenger.last(t).data)

	if string(missingFields["status"]) != string(nilFields["status"]) {
		t.Fatalf("missing vs nil field wire bytes differ: % x vs % x",
			[]byte(missingFields["status"]), []byte(nilFields["status"]))
	}
	if len(nilFields["status"]) != 1 || nilFields["status"][0] != 0xf6 {
		t.Fatalf("expected CBOR null for nil field, got % x", []byte(nilFields["status"]))
	}
}

func TestPublishConversionFailureSendsNullAndReportsError(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "small", Type: contract.TypeInt16},
				{Key: "ok", Type: contract.TypeInt64},
			},
		},
	})

	// 70000 does not fit int16: field goes out as null, an error is reported
	// on the error topic, and the data message is STILL published.
	if err := instance.Publish("output1", map[string]any{
		"small": int64(70000),
		"ok":    int64(1),
	}); err != nil {
		t.Fatalf("expected Publish to continue after field failure, got: %v", err)
	}

	topics := messenger.topics()
	if len(topics) != 2 {
		t.Fatalf("expected error report + data publish, got topics %v", topics)
	}
	if topics[0] != "neoedgex/neoflow/error/node-1" {
		t.Fatalf("expected first publish on error topic, got %q", topics[0])
	}
	if topics[1] != "neoedgex/neoflow/out/node-1/output1" {
		t.Fatalf("expected data publish last, got %q", topics[1])
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	if len(fields["small"]) != 1 || fields["small"][0] != 0xf6 {
		t.Fatalf("expected null for failed field, got % x", []byte(fields["small"]))
	}
	var ok int64
	if err := cbor.Unmarshal(fields["ok"], &ok); err != nil || ok != 1 {
		t.Fatalf("healthy field must be unaffected: %v %d", err, ok)
	}
}

// TestPublishAcceptsNativeGoIntegerKinds pins the motivating case end to end:
// an untyped constant defaults to int, so ctx.Publish(h, map[string]any{
// "count": 5}) is the most natural Go code there is. It used to send CBOR null
// plus a node error while returning nil; now every Go integer kind converts to
// the declared tag type and reaches the wire.
func TestPublishAcceptsNativeGoIntegerKinds(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "count", Type: contract.TypeInt64},
				{Key: "plainUint", Type: contract.TypeUint32},
				{Key: "tiny", Type: contract.TypeInt16},
				{Key: "aByte", Type: contract.TypeUint16},
				{Key: "blob", Type: contract.TypeRaw},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{
		"count":     5, // untyped constant -> int
		"plainUint": uint(7),
		"tiny":      int8(-9),
		"aByte":     uint8(200),
		"blob":      []byte{0x01, 0x02}, // []byte must still map to raw
	}); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	if topics := messenger.topics(); len(topics) != 1 || topics[0] != "neoedgex/neoflow/out/node-1/output1" {
		t.Fatalf("expected only the data publish and no error report, got %v", topics)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	for _, key := range []string{"count", "plainUint", "tiny", "aByte", "blob"} {
		if len(fields[key]) == 1 && fields[key][0] == 0xf6 {
			t.Fatalf("field %q went out as CBOR null", key)
		}
	}
	var count int64
	if err := cbor.Unmarshal(fields["count"], &count); err != nil || count != 5 {
		t.Fatalf("count: %v %d", err, count)
	}
	var plainUint uint32
	if err := cbor.Unmarshal(fields["plainUint"], &plainUint); err != nil || plainUint != 7 {
		t.Fatalf("plainUint: %v %d", err, plainUint)
	}
	var tiny int16
	if err := cbor.Unmarshal(fields["tiny"], &tiny); err != nil || tiny != -9 {
		t.Fatalf("tiny: %v %d", err, tiny)
	}
	var aByte uint16
	if err := cbor.Unmarshal(fields["aByte"], &aByte); err != nil || aByte != 200 {
		t.Fatalf("aByte: %v %d", err, aByte)
	}
	var blob []byte
	if err := cbor.Unmarshal(fields["blob"], &blob); err != nil || len(blob) != 2 {
		t.Fatalf("blob: %v % x", err, blob)
	}
	if fields["blob"][0]&0xe0 != 0x40 {
		t.Fatalf("[]byte must stay a CBOR byte string, got head 0x%02x", fields["blob"][0])
	}
}

// TestPublishRangeChecksNativeGoIntegerKinds pins that the widened input
// domain kept the range checking: an int too large for the declared tag still
// goes out as null with a reported error, never wrapped.
func TestPublishRangeChecksNativeGoIntegerKinds(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {{Key: "small", Type: contract.TypeInt16}},
		},
	})

	if err := instance.Publish("output1", map[string]any{"small": 70000}); err != nil {
		t.Fatalf("expected Publish to continue after field failure, got: %v", err)
	}

	first := messenger.published[0]
	if first.topic != "neoedgex/neoflow/error/node-1" {
		t.Fatalf("expected an error report for the out-of-range int, got %q", first.topic)
	}
	_, fields := decodeWire(t, messenger.last(t).data)
	if len(fields["small"]) != 1 || fields["small"][0] != 0xf6 {
		t.Fatalf("expected CBOR null for the out-of-range int, got % x", []byte(fields["small"]))
	}
}

func TestPublishRejectsTimeTimeWithFormatGuidance(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {{Key: "ts", Type: contract.TypeString}},
		},
	})

	if err := instance.Publish("output1", map[string]any{"ts": time.Now()}); err != nil {
		t.Fatalf("expected Publish to continue, got: %v", err)
	}

	// error topic first (JSON, C3), with guidance toward Format().
	first := messenger.published[0]
	if first.topic != "neoedgex/neoflow/error/node-1" {
		t.Fatalf("expected error topic first, got %q", first.topic)
	}
	var nodeError contract.Error
	if err := json.Unmarshal(first.data, &nodeError); err != nil {
		t.Fatalf("error topic payload must stay JSON: %v", err)
	}
	if !strings.Contains(nodeError.Detail, "Format") {
		t.Fatalf("expected Format guidance in error detail, got %q", nodeError.Detail)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	if len(fields["ts"]) != 1 || fields["ts"][0] != 0xf6 {
		t.Fatalf("expected null for time.Time field, got % x", []byte(fields["ts"]))
	}
}

type recordingLogger struct {
	mu    sync.Mutex
	infos []string
	warns []string
}

func (*recordingLogger) Debug(string, ...any) {}
func (l *recordingLogger) Info(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Warn(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}
func (*recordingLogger) Error(string, ...any) {}

func (l *recordingLogger) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append(append([]string(nil), l.infos...), l.warns...)
}

type recordingSDK struct {
	*testSDK
	logger *recordingLogger
}

func (s *recordingSDK) NewLogger(string) contract.Logger { return s.logger }

func TestPublishDropsAndWarnsTagNotInOutputSchema(t *testing.T) {
	logger := &recordingLogger{}
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
	instance, err := NewInstance(&recordingSDK{
		testSDK: &testSDK{ctx: context.Background(), messenger: messenger},
		logger:  logger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Outputs: map[string][]contract.PortFieldSchema{
				"output1": {{Key: "value", Type: contract.TypeString}},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.Publish("output1", map[string]any{"value": "x", "extra": "y"}); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	// undeclared key never reaches the wire (Publish-side gate for bypass chains)
	_, fields := decodeWire(t, messenger.last(t).data)
	if _, exists := fields["extra"]; exists {
		t.Fatal("undeclared tag must be dropped from the wire")
	}

	found := false
	for _, line := range logger.warns {
		if line == `Tag "extra" is not defined in the output schema; dropping` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warn about undefined tag %q, got warns=%v", "extra", logger.warns)
	}
}

// splitLoggerSDK hands out two distinguishable loggers so a test can tell
// which factory a given line came from — NewLogger is what DisableSDKLog
// silences, NewHandlerLogger is what it must not.
type splitLoggerSDK struct {
	*testSDK
	sdkLog     *recordingLogger
	handlerLog *recordingLogger
}

func (s *splitLoggerSDK) NewLogger(string) contract.Logger        { return s.sdkLog }
func (s *splitLoggerSDK) NewHandlerLogger(string) contract.Logger { return s.handlerLog }

// TestInstanceLoggerIsSeparateFromSDKLogger pins the seam DisableSDKLog acts
// on: everything the SDK says about itself goes to the silenceable NewLogger,
// while NodeEnv.Logger() hands back the NewHandlerLogger one untouched. Before
// the split both were the same logger, so DisableSDKLog also swallowed
// ctx.Logger() lines the app wrote itself.
func TestInstanceLoggerIsSeparateFromSDKLogger(t *testing.T) {
	sdkLog := &recordingLogger{}
	handlerLog := &recordingLogger{}
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload, 4)}
	instance, err := NewInstance(&splitLoggerSDK{
		testSDK:    &testSDK{ctx: context.Background(), messenger: messenger},
		sdkLog:     sdkLog,
		handlerLog: handlerLog,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name:    "demo-node",
			Outputs: map[string][]contract.PortFieldSchema{"output1": {{Key: "value", Type: contract.TypeString}}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instance.Logger() != contract.Logger(handlerLog) {
		t.Fatal("Logger() must return the handler-facing logger, not the SDK one")
	}

	instance.Logger().Info("app-written-line")

	// an SDK-machinery line: the undeclared-tag warning Publish emits
	if err := instance.Publish("output1", map[string]any{"value": "x", "extra": "y"}); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	handlerLines := strings.Join(handlerLog.lines(), "\n")
	if !strings.Contains(handlerLines, "app-written-line") {
		t.Fatalf("app line did not reach the handler logger: %v", handlerLog.lines())
	}
	if strings.Contains(handlerLines, "extra") {
		t.Fatalf("SDK machinery leaked into the handler logger: %v", handlerLog.lines())
	}

	sdkLines := strings.Join(sdkLog.lines(), "\n")
	if !strings.Contains(sdkLines, "extra") {
		t.Fatalf("SDK machinery must keep using the SDK logger: %v", sdkLog.lines())
	}
	if strings.Contains(sdkLines, "app-written-line") {
		t.Fatalf("app line leaked into the SDK logger: %v", sdkLog.lines())
	}
}

func TestPublishNodeErrorStaysJSONOnErrorTopic(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{Name: "demo-node"})

	if err := instance.PublishNodeError(contract.CodeProcessError, fmt.Errorf("something went wrong")); err != nil {
		t.Fatalf("unexpected error from PublishNodeError: %v", err)
	}

	rec := messenger.last(t)
	if rec.topic != "neoedgex/neoflow/error/node-1" {
		t.Fatalf("unexpected published topic: %s", rec.topic)
	}

	// C3: the error topic payload is JSON, not CBOR.
	var nodeError contract.Error
	if err := json.Unmarshal(rec.data, &nodeError); err != nil {
		t.Fatalf("error payload must decode as JSON: %v", err)
	}
	if nodeError.Code != string(contract.CodeProcessError) {
		t.Fatalf("unexpected error code: %s", nodeError.Code)
	}
	if nodeError.Detail != "something went wrong" {
		t.Fatalf("unexpected error detail: %s", nodeError.Detail)
	}
}

func TestPublishHeartbeatSendsEmptyPayload(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{Name: "demo-node"})
	if err := instance.PublishHeartbeat(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec := messenger.last(t)
	if rec.topic != "neoedgex/neoflow/heartbeat/node-1" {
		t.Fatalf("unexpected topic: %s", rec.topic)
	}
	if len(rec.data) != 0 {
		t.Fatalf("heartbeat payload must stay empty, got % x", rec.data)
	}
}

// --- runLoop (inbound quadrant) ---

func runLoopInstance(t *testing.T, inputs map[string][]contract.PortFieldSchema) (*Instance, chan core.RawMessengerPayload, *testMessenger) {
	t.Helper()
	subscriber := make(chan core.RawMessengerPayload, 4)
	messenger := &testMessenger{subscriber: subscriber}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{Name: "demo-node", Inputs: inputs},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	go instance.runLoop()
	t.Cleanup(instance.Shutdown)
	return instance, subscriber, messenger
}

func receiveMessage(t *testing.T, instance *Instance) contract.Message {
	t.Helper()
	select {
	case message := <-instance.Messages():
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message to be delivered")
		return contract.Message{}
	}
}

func TestRunLoopDeliversSchemaTypedMessage(t *testing.T) {
	instance, subscriber, _ := runLoopInstance(t, map[string][]contract.PortFieldSchema{
		"input1": {{Key: "value", Type: contract.TypeInt64}},
	})

	dataBytes, err := cbor.Marshal(map[string]any{"value": 42})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	payload, err := cbor.Marshal(contract.NeoFlowMessage{
		SourceNodeID: "source-node",
		Timestamp:    "2026-03-31T09:10:11Z",
		Data:         dataBytes,
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	subscriber <- core.RawMessengerPayload{Handle: "input1", Data: payload}

	message := receiveMessage(t, instance)
	if message.Source != "source-node" {
		t.Fatalf("unexpected message source: %s", message.Source)
	}
	if message.Timestamp != "2026-03-31T09:10:11Z" {
		t.Fatalf("unexpected message timestamp: %s", message.Timestamp)
	}
	if message.Handle != "input1" {
		t.Fatalf("unexpected handle: %s", message.Handle)
	}

	// schema injection happened: the input schema types the decoded field.
	decoded := message.ToMap()
	if got, ok := decoded["value"].(int64); !ok || got != 42 {
		t.Fatalf("unexpected field value: %#v (%T)", decoded["value"], decoded["value"])
	}
}

func TestRunLoopLeavesTimestampEmptyWhenInboundPayloadOmitsIt(t *testing.T) {
	instance, subscriber, _ := runLoopInstance(t, map[string][]contract.PortFieldSchema{
		"input1": {{Key: "value", Type: contract.TypeInt64}},
	})

	dataBytes, err := cbor.Marshal(map[string]any{"value": 42})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	payload, err := cbor.Marshal(struct {
		SourceNodeID string          `cbor:"source"`
		Data         cbor.RawMessage `cbor:"data"`
	}{
		SourceNodeID: "source-node",
		Data:         dataBytes,
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	subscriber <- core.RawMessengerPayload{Handle: "input1", Data: payload}

	message := receiveMessage(t, instance)
	if message.Timestamp != "" {
		t.Fatalf("expected empty timestamp for timestampless inbound payload, got %q", message.Timestamp)
	}
}

func TestRunLoopRejectsMalformedPayloadAndReportsError(t *testing.T) {
	instance, subscriber, messenger := runLoopInstance(t, map[string][]contract.PortFieldSchema{
		"input1": {{Key: "value", Type: contract.TypeInt64}},
	})

	// 1) a payload that is not a CBOR envelope at all (legacy JSON app, D1
	//    explicit-failure path).
	subscriber <- core.RawMessengerPayload{Handle: "input1", Data: []byte(`{"source":"legacy-json"}`)}

	// 2) an envelope whose data segment is not a CBOR map (O(1) gate).
	badData, err := cbor.Marshal(contract.NeoFlowMessage{
		SourceNodeID: "source-node",
		Data:         cbor.RawMessage{0x05}, // CBOR unsigned 5, not a map
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	subscriber <- core.RawMessengerPayload{Handle: "input1", Data: badData}

	// 3) a valid message afterwards: the loop must still be alive.
	dataBytes, _ := cbor.Marshal(map[string]any{"value": 1})
	good, err := cbor.Marshal(contract.NeoFlowMessage{SourceNodeID: "ok", Data: dataBytes})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	subscriber <- core.RawMessengerPayload{Handle: "input1", Data: good}

	message := receiveMessage(t, instance)
	if message.Source != "ok" {
		t.Fatalf("expected the valid message to arrive, got source %q", message.Source)
	}

	// both rejects reported on the error topic (JSON), zero messages delivered
	// for them (the only delivery observed was the valid one).
	errorReports := 0
	for _, topic := range messenger.topics() {
		if topic == "neoedgex/neoflow/error/node-1" {
			errorReports++
		}
	}
	if errorReports != 2 {
		t.Fatalf("expected 2 error reports (bad envelope + non-map data), got %d: %v", errorReports, messenger.topics())
	}
}
