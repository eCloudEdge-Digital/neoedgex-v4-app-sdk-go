package node

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/contract"
)

func newJsonOutputInstance(t *testing.T) (*Instance, *testMessenger) {
	t.Helper()
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
					{Key: "obj", Type: contract.TypeJsonObject},
					{Key: "arr", Type: contract.TypeJsonArray},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return instance, messenger
}

// TestPublishJsonFieldsEndToEnd proves Publish of json fields produces the
// expected typed PortFieldData: a RawMessage is carried verbatim (big-int safe),
// and a map[string]any is SDK-marshaled.
func TestPublishJsonFieldsEndToEnd(t *testing.T) {
	instance, messenger := newJsonOutputInstance(t)

	if err := instance.Publish("output1", map[string]any{
		"obj": json.RawMessage(`{"id":9223372036854775807}`),
		"arr": []any{float64(1), float64(2)},
	}); err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}

	var message contract.NeoFlowMessage
	if err := json.Unmarshal(messenger.publishedData, &message); err != nil {
		t.Fatalf("unexpected marshal output: %v", err)
	}

	obj := message.Data["obj"]
	if obj.Type != contract.TypeJsonObject {
		t.Fatalf("expected obj type jsonObject, got %q", obj.Type)
	}
	if obj.Value != `{"id":9223372036854775807}` {
		t.Fatalf("expected obj value verbatim, got %q", obj.Value)
	}

	arr := message.Data["arr"]
	if arr.Type != contract.TypeJsonArray {
		t.Fatalf("expected arr type jsonArray, got %q", arr.Type)
	}
	if arr.Value != `[1,2]` {
		t.Fatalf("expected arr value marshaled, got %q", arr.Value)
	}
}

// TestPublishJsonFieldFailureSendsEmptyField proves a failing json field keeps
// the existing Publish semantics: the message is still sent and the offending
// field is published as an empty field (shape mismatch: array into jsonObject).
func TestPublishJsonFieldFailureSendsEmptyField(t *testing.T) {
	instance, messenger := newJsonOutputInstance(t)

	if err := instance.Publish("output1", map[string]any{
		"obj": json.RawMessage(`[1,2,3]`), // wrong shape for a jsonObject field
	}); err != nil {
		t.Fatalf("expected Publish to still succeed, got: %v", err)
	}

	if len(messenger.publishedData) == 0 {
		t.Fatal("expected the message to still be sent")
	}

	var message contract.NeoFlowMessage
	if err := json.Unmarshal(messenger.publishedData, &message); err != nil {
		t.Fatalf("unexpected marshal output: %v", err)
	}

	obj := message.Data["obj"]
	if obj.Type != contract.TypeUndefined || obj.Value != "" {
		t.Fatalf("expected empty field for failed json conversion, got type %q value %q", obj.Type, obj.Value)
	}
}
