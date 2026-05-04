package messenger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/contract"
)

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

type stubSDK struct {
	ctx context.Context
}

func (s stubSDK) Context() context.Context         { return s.ctx }
func (s stubSDK) NodeConfigs() []contract.Node     { return nil }
func (s stubSDK) Messenger() core.MessengerClient  { return nil }
func (s stubSDK) NewLogger(string) contract.Logger { return noopLogger{} }
func (s stubSDK) Shutdown()                        {}

func TestConnectReturnsErrorWhenConfigIsNil(t *testing.T) {
	client := NewMessenger(stubSDK{ctx: context.Background()}, &core.MessengerOptions{
		Config: nil,
	})

	err := client.Connect()
	if err == nil {
		t.Fatal("expected Connect to return an error when config is nil")
	}
}

func TestOnConnectionLostCancelsResubscribeContext(t *testing.T) {
	client := NewMessenger(stubSDK{ctx: context.Background()}, &core.MessengerOptions{
		Config: &core.MessengerConfig{},
	})

	resubscribeCtx, cancel := context.WithCancel(context.Background())
	client.resubscribeCtx = resubscribeCtx
	client.cancelResubscribe = cancel

	client.onConnectionLost(nil, errors.New("lost"))

	if client.cancelResubscribe != nil {
		t.Fatal("expected cancelResubscribe to be cleared")
	}

	select {
	case <-resubscribeCtx.Done():
	default:
		t.Fatal("expected resubscribe context to be canceled")
	}
}

func TestOnConnectStartsResubscribeLoop(t *testing.T) {
	client := NewMessenger(stubSDK{ctx: context.Background()}, &core.MessengerOptions{
		Config: &core.MessengerConfig{},
	})
	defer client.Disconnect()

	client.onConnect(nil)

	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		client.mutex.Lock()
		cancel := client.cancelResubscribe
		client.mutex.Unlock()
		if cancel != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected onConnect to start resubscribe loop")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDisconnectAndConnectionLostAreRaceSafe(t *testing.T) {
	client := NewMessenger(stubSDK{ctx: context.Background()}, &core.MessengerOptions{
		Config: &core.MessengerConfig{},
	})

	for i := 0; i < 100; i++ {
		_, cancel := context.WithCancel(context.Background())
		client.mutex.Lock()
		client.cancelResubscribe = cancel
		client.mutex.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			client.Disconnect()
		}()
		go func() {
			defer wg.Done()
			client.onConnectionLost(nil, errors.New("lost"))
		}()
	}
	wg.Wait()
}

func TestAddSubscriberReturnsExistingChannelForSameNodeID(t *testing.T) {
	client := NewMessenger(stubSDK{ctx: context.Background()}, &core.MessengerOptions{
		Config: &core.MessengerConfig{},
	})

	ch1 := client.AddSubscriber("node-1")
	ch2 := client.AddSubscriber("node-1")

	if ch1 != ch2 {
		t.Fatal("expected AddSubscriber to return existing channel for same node ID")
	}
}
