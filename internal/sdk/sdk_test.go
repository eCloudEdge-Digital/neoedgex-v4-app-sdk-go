package sdk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/core"
)

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

type fakeMessenger struct {
	connectErr       error
	connectCalled    bool
	disconnectCalled bool
}

func (m *fakeMessenger) Connect() error {
	m.connectCalled = true
	return m.connectErr
}

func (m *fakeMessenger) Disconnect() {
	m.disconnectCalled = true
}

func (m *fakeMessenger) AddSubscriber(string) <-chan core.RawMessengerPayload {
	return nil
}

func (m *fakeMessenger) RemoveSubscriber(string) {}

func (m *fakeMessenger) Publish(string, byte, []byte) error {
	return nil
}

func TestRunReturnsErrorWhenMessengerConnectFails(t *testing.T) {
	m := &fakeMessenger{connectErr: errors.New("connect failed")}
	s := &sdk{
		messenger: m,
		ctx:       context.Background(),
		logger:    noopLogger{},
	}

	err := s.Run(nil)
	if err == nil {
		t.Fatal("expected Run to return an error")
	}
	if !strings.Contains(err.Error(), "failed to connect messenger") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.connectCalled {
		t.Fatal("expected Connect to be called")
	}
	if m.disconnectCalled {
		t.Fatal("did not expect Disconnect to be called after failed Connect")
	}
}

func TestRunDisconnectsOnContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &fakeMessenger{}
	s := &sdk{
		messenger: m,
		ctx:       ctx,
		logger:    noopLogger{},
	}

	if err := s.Run(nil); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	if !m.connectCalled {
		t.Fatal("expected Connect to be called")
	}
	if !m.disconnectCalled {
		t.Fatal("expected Disconnect to be called")
	}
}

type recordingLogger struct {
	infos []string
}

func (recordingLogger) Debug(string, ...any) {}
func (l *recordingLogger) Info(format string, args ...any) {
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}
func (recordingLogger) Warn(string, ...any)  {}
func (recordingLogger) Error(string, ...any) {}

func TestMockMessengerPublishLogsOutboundPayload(t *testing.T) {
	logger := &recordingLogger{}
	m := newMockMessenger(logger)

	if err := m.Publish("topic/x", 0, []byte(`{"k":1}`)); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}
	if err := m.Publish("topic/y", 2, nil); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	foundX, foundY := false, false
	for _, line := range logger.infos {
		if strings.Contains(line, "[MOCK PUBLISH]") && strings.Contains(line, "topic/x") {
			foundX = true
		}
		if strings.Contains(line, "[MOCK PUBLISH]") && strings.Contains(line, "topic/y") {
			foundY = true
		}
	}
	if !foundX || !foundY {
		t.Fatalf("expected both publish calls to be logged, got: %v", logger.infos)
	}
}
