package node

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/contract"
)

type testLogger struct{}

func (testLogger) Debug(string, ...any) {}
func (testLogger) Info(string, ...any)  {}
func (testLogger) Warn(string, ...any)  {}
func (testLogger) Error(string, ...any) {}

type testMessenger struct {
	subscriber      chan core.RawMessengerPayload
	publishedTopic  string
	publishedQoS    byte
	publishedData   []byte
	removedNodeID   string
	addedSubscriber string
}

func (m *testMessenger) Connect() error { return nil }

func (m *testMessenger) Disconnect() {}

func (m *testMessenger) AddSubscriber(nodeID string) <-chan core.RawMessengerPayload {
	m.addedSubscriber = nodeID
	return m.subscriber
}

func (m *testMessenger) RemoveSubscriber(nodeID string) {
	m.removedNodeID = nodeID
}

func (m *testMessenger) Publish(topic string, qos byte, data []byte) error {
	m.publishedTopic = topic
	m.publishedQoS = qos
	m.publishedData = append([]byte(nil), data...)
	return nil
}

type testSDK struct {
	ctx       context.Context
	messenger *testMessenger
}

func (s *testSDK) Context() context.Context { return s.ctx }

func (s *testSDK) NodeConfigs() []contract.Node { return nil }

func (s *testSDK) Messenger() core.MessengerClient { return s.messenger }

func (s *testSDK) NewLogger(string) contract.Logger { return testLogger{} }

func (s *testSDK) Shutdown() {}

func TestNewInstanceCreatesInstanceWithoutOptions(t *testing.T) {
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: &testMessenger{subscriber: make(chan core.RawMessengerPayload)},
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instance.NodeConfig().ID != "node-1" {
		t.Fatalf("unexpected node config ID: %s", instance.NodeConfig().ID)
	}
}

func TestPublishSkipsOutputValidation(t *testing.T) {
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Outputs: map[string][]contract.PortFieldSchema{
				"output1": {
					{Key: "value", Type: contract.TypeString},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish to succeed without output validation, got: %v", err)
	}

	if messenger.publishedTopic != "neoedgex/neoflow/out/node-1/output1" {
		t.Fatalf("unexpected published topic: %s", messenger.publishedTopic)
	}

	var message contract.NeoFlowMessage
	if err := json.Unmarshal(messenger.publishedData, &message); err != nil {
		t.Fatalf("unexpected marshal output: %v", err)
	}
	if message.Timestamp == "" {
		t.Fatal("expected published message timestamp to be set")
	}
	if _, err := time.Parse(time.RFC3339, message.Timestamp); err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q: %v", message.Timestamp, err)
	}
	if got := message.Data["value"].Type; got != contract.TypeString {
		t.Fatalf("unexpected published field type: %s", got)
	}
}

func TestPublishFillsMissingOutputFieldWithEmptyField(t *testing.T) {
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Outputs: map[string][]contract.PortFieldSchema{
				"output1": {
					{Key: "value", Type: contract.TypeInt64},
					{Key: "status", Type: contract.TypeString},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish to succeed with missing field, got: %v", err)
	}

	var message contract.NeoFlowMessage
	if err := json.Unmarshal(messenger.publishedData, &message); err != nil {
		t.Fatalf("unexpected marshal output: %v", err)
	}

	if got := message.Data["status"]; got.Type != contract.TypeUndefined || got.Value != "" {
		t.Fatalf("unexpected empty field: %#v", got)
	}
}

func TestPublishTreatsNilFieldValueAsEmptyField(t *testing.T) {
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Outputs: map[string][]contract.PortFieldSchema{
				"output1": {
					{Key: "value", Type: contract.TypeInt64},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.Publish("output1", map[string]any{"value": nil}); err != nil {
		t.Fatalf("expected Publish to treat nil as empty field, got: %v", err)
	}

	var message contract.NeoFlowMessage
	if err := json.Unmarshal(messenger.publishedData, &message); err != nil {
		t.Fatalf("unexpected marshal output: %v", err)
	}

	if got := message.Data["value"]; got.Type != contract.TypeUndefined || got.Value != "" {
		t.Fatalf("unexpected empty field for explicit nil: %#v", got)
	}
}

func TestPublishTreatsMissingAndNilFieldsEquivalently(t *testing.T) {
	instanceWithMessenger := func() (*Instance, *testMessenger) {
		messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
		instance, err := NewInstance(&testSDK{
			ctx:       context.Background(),
			messenger: messenger,
		}, contract.Node{
			ID:   "node-1",
			Type: "demo",
			Data: contract.NodeData{
				Name: "demo-node",
				Outputs: map[string][]contract.PortFieldSchema{
					"output1": {
						{Key: "value", Type: contract.TypeInt64},
						{Key: "status", Type: contract.TypeString},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return instance, messenger
	}

	missingInstance, missingMessenger := instanceWithMessenger()
	if err := missingInstance.Publish("output1", map[string]any{"value": int64(7)}); err != nil {
		t.Fatalf("expected Publish with missing field to succeed, got: %v", err)
	}

	nilInstance, nilMessenger := instanceWithMessenger()
	if err := nilInstance.Publish("output1", map[string]any{
		"value":  int64(7),
		"status": nil,
	}); err != nil {
		t.Fatalf("expected Publish with explicit nil field to succeed, got: %v", err)
	}

	var missingMessage contract.NeoFlowMessage
	if err := json.Unmarshal(missingMessenger.publishedData, &missingMessage); err != nil {
		t.Fatalf("unexpected marshal output for missing field: %v", err)
	}

	var nilMessage contract.NeoFlowMessage
	if err := json.Unmarshal(nilMessenger.publishedData, &nilMessage); err != nil {
		t.Fatalf("unexpected marshal output for nil field: %v", err)
	}

	if missingMessage.Data["status"] != nilMessage.Data["status"] {
		t.Fatalf("missing field and nil field produced different outputs: missing=%#v nil=%#v", missingMessage.Data["status"], nilMessage.Data["status"])
	}
}

func TestRunLoopSkipsInputValidation(t *testing.T) {
	subscriber := make(chan core.RawMessengerPayload, 1)
	messenger := &testMessenger{subscriber: subscriber}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Inputs: map[string][]contract.PortFieldSchema{
				"input1": {
					{Key: "value", Type: contract.TypeString},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	go instance.runLoop()
	t.Cleanup(instance.Shutdown)

	bytes, err := json.Marshal(contract.NeoFlowMessage{
		SourceNodeID: "source-node",
		Timestamp:    "2026-03-31T09:10:11Z",
		Data: map[string]contract.PortFieldData{
			"value": {
				Type:  contract.TypeInt64,
				Value: "42",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	subscriber <- core.RawMessengerPayload{
		Handle: "input1",
		Data:   bytes,
	}

	select {
	case message := <-instance.Messages():
		if message.Source != "source-node" {
			t.Fatalf("unexpected message source: %s", message.Source)
		}
		if message.Timestamp != "2026-03-31T09:10:11Z" {
			t.Fatalf("unexpected message timestamp: %s", message.Timestamp)
		}
		if got, ok := message.Data["value"].(int64); !ok || got != 42 {
			t.Fatalf("unexpected field value: %#v", message.Data["value"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected message to be forwarded without input validation")
	}
}

func TestRunLoopLeavesTimestampEmptyWhenInboundPayloadOmitsIt(t *testing.T) {
	subscriber := make(chan core.RawMessengerPayload, 1)
	messenger := &testMessenger{subscriber: subscriber}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{
			Name: "demo-node",
			Inputs: map[string][]contract.PortFieldSchema{
				"input1": {
					{Key: "value", Type: contract.TypeString},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	go instance.runLoop()
	t.Cleanup(instance.Shutdown)

	bytes, err := json.Marshal(struct {
		SourceNodeID string                            `json:"source"`
		Data         map[string]contract.PortFieldData `json:"data"`
	}{
		SourceNodeID: "source-node",
		Data: map[string]contract.PortFieldData{
			"value": {
				Type:  contract.TypeInt64,
				Value: "42",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	subscriber <- core.RawMessengerPayload{
		Handle: "input1",
		Data:   bytes,
	}

	select {
	case message := <-instance.Messages():
		if message.Timestamp != "" {
			t.Fatalf("expected empty timestamp for legacy inbound payload, got %q", message.Timestamp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected legacy message to be forwarded")
	}
}

func TestPublishNodeErrorPublishesToErrorTopic(t *testing.T) {
	messenger := &testMessenger{subscriber: make(chan core.RawMessengerPayload)}
	instance, err := NewInstance(&testSDK{
		ctx:       context.Background(),
		messenger: messenger,
	}, contract.Node{
		ID:   "node-1",
		Type: "demo",
		Data: contract.NodeData{Name: "demo-node"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.PublishNodeError(contract.CodeProcessError, fmt.Errorf("something went wrong")); err != nil {
		t.Fatalf("unexpected error from PublishNodeError: %v", err)
	}

	if messenger.publishedTopic != "neoedgex/neoflow/error/node-1" {
		t.Fatalf("unexpected published topic: %s", messenger.publishedTopic)
	}

	var nodeError contract.Error
	if err := json.Unmarshal(messenger.publishedData, &nodeError); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if nodeError.Code != string(contract.CodeProcessError) {
		t.Fatalf("unexpected error code: %s", nodeError.Code)
	}
	if nodeError.Detail != "something went wrong" {
		t.Fatalf("unexpected error detail: %s", nodeError.Detail)
	}
}

func TestDecodeIncomingDataSetsNilForUndefinedType(t *testing.T) {
	decoded := decodeIncomingData(map[string]contract.PortFieldData{
		"undefined-type": {
			Type:  contract.TypeUndefined,
			Value: "unexpected",
		},
		"empty": *contract.NewEmptyField(),
	})

	for _, key := range []string{"undefined-type", "empty"} {
		if value, exists := decoded[key]; !exists || value != nil {
			t.Fatalf("decoded %s field = %#v", key, decoded)
		}
	}
}

func TestDecodeIncomingDataSetsNilForMalformedField(t *testing.T) {
	decoded := decodeIncomingData(map[string]contract.PortFieldData{
		"broken": {
			Type:  contract.TypeInt64,
			Value: "not-a-number",
		},
	})

	if value, exists := decoded["broken"]; !exists || value != nil {
		t.Fatalf("decoded malformed field = %#v", decoded)
	}
}

type recordingLogger struct {
	warns []string
}

func (recordingLogger) Debug(string, ...any) {}
func (recordingLogger) Info(string, ...any)  {}
func (l *recordingLogger) Warn(format string, args ...any) {
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}
func (recordingLogger) Error(string, ...any) {}

type recordingSDK struct {
	*testSDK
	logger *recordingLogger
}

func (s *recordingSDK) NewLogger(string) contract.Logger { return s.logger }

func TestPublishWarnsWhenDataContainsTagNotInOutputSchema(t *testing.T) {
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
				"output1": {
					{Key: "value", Type: contract.TypeString},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := instance.Publish("output1", map[string]any{"value": int64(7), "extra": "x"}); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
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
