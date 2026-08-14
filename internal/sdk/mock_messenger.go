package sdk

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

var mockDecMode, _ = cbor.DecOptions{
	DefaultMapType: reflect.TypeOf(map[string]any(nil)),
}.DecMode()

// mockMessenger 是記憶體內的 MessengerClient 實作，用於 mock 模式。
type mockMessenger struct {
	mu          sync.Mutex
	subscribers map[string]chan core.RawMessengerPayload
	logger      contract.Logger
}

var _ core.MessengerClient = (*mockMessenger)(nil)

func newMockMessenger(logger contract.Logger) *mockMessenger {
	return &mockMessenger{
		subscribers: make(map[string]chan core.RawMessengerPayload),
		logger:      logger,
	}
}

func (m *mockMessenger) Connect() error {
	return nil
}

func (m *mockMessenger) Disconnect() {}

func (m *mockMessenger) AddSubscriber(nodeID string) <-chan core.RawMessengerPayload {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, exists := m.subscribers[nodeID]; exists {
		return ch
	}
	ch := make(chan core.RawMessengerPayload, 32)
	m.subscribers[nodeID] = ch
	return ch
}

func (m *mockMessenger) RemoveSubscriber(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, exists := m.subscribers[nodeID]; exists {
		close(ch)
		delete(m.subscribers, nodeID)
	}
}

func (m *mockMessenger) Publish(topic string, qos byte, data []byte) error {
	// No real broker to forward to; surface the outbound payload through the
	// SDK logger so local development can observe what handlers emit.
	if m.logger != nil {
		// Data messages are CBOR, the error topic stays JSON; try both so the
		// human-readable mock output covers either wire format.
		var payload any
		if len(data) == 0 {
			payload = ""
		} else if err := json.Unmarshal(data, &payload); err != nil {
			if err := mockDecMode.Unmarshal(data, &payload); err != nil {
				payload = string(data)
			}
		}
		m.logger.Info("[MOCK PUBLISH] topic=%s qos=%d payload=%v", topic, qos, payload)
	}
	return nil
}

// injectNeoFlowMessage 將 PortFieldData map 轉為原生值、包裝為 CBOR NeoFlowMessage
// 並送入指定 nodeID 的 subscriber channel。
func (m *mockMessenger) injectNeoFlowMessage(nodeID, handle string, data map[string]contract.PortFieldData) error {
	dataMap := make(map[string]any, len(data))
	for key, field := range data {
		if field.Type == contract.TypeUndefined {
			dataMap[key] = nil
			continue
		}
		value, err := field.GetAnyValue()
		if err != nil {
			return fmt.Errorf("mock field %q: %w", key, err)
		}
		dataMap[key] = value
	}

	dataBytes, err := cbor.Marshal(dataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal mock data: %w", err)
	}

	msg := contract.NeoFlowMessage{
		SourceNodeID: "mock",
		Timestamp:    time.Now().UTC().Format(contract.PublishTimestampLayout),
		Data:         dataBytes,
	}
	msgBytes, err := cbor.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal mock message: %w", err)
	}

	payload := core.RawMessengerPayload{
		Handle: handle,
		Data:   msgBytes,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ch, exists := m.subscribers[nodeID]
	if !exists {
		return fmt.Errorf("no subscriber for node %q", nodeID)
	}
	select {
	case ch <- payload:
		return nil
	default:
		return fmt.Errorf("subscriber channel full for node %q", nodeID)
	}
}
