package sdk

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/contract"
)

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
		var payload any
		if len(data) == 0 {
			payload = ""
		} else if err := json.Unmarshal(data, &payload); err != nil {
			payload = string(data)
		}
		m.logger.Info("[MOCK PUBLISH] topic=%s qos=%d payload=%v", topic, qos, payload)
	}
	return nil
}

// injectNeoFlowMessage 將 PortFieldData map 包裝為 NeoFlowMessage 並送入指定 nodeID 的 subscriber channel。
func (m *mockMessenger) injectNeoFlowMessage(nodeID, handle string, data map[string]contract.PortFieldData) error {
	msg := contract.NeoFlowMessage{
		SourceNodeID: "mock",
		Data:         data,
	}
	msgBytes, err := json.Marshal(msg)
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
