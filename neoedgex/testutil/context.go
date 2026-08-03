// Package testutil provides helpers for unit-testing NodeHandler implementations.
package testutil

import (
	"context"
	"sync"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/logger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// MockNodeEnv is a test double for neoedgex.NodeEnv.
// Zero value is ready to use; set fields to control behaviour.
//
// The intended way to feed a handler is the NewMessage/Deliver pair: NewMessage
// builds an inbound message from the input schema in Config, so its fields
// decode with the same schema types the runtime would give them, and Deliver
// queues the built messages and closes the channel so the handler's receive
// loop returns and the recordings below can be read.
//
// Its methods are individually safe to call concurrently, but the type as a
// whole is not: the recording fields PublishedData, ReportedErrors and
// StopCalled are plain exported fields, read without the mutex the methods
// take. Reading them while a handler goroutine can still publish, report or
// stop is a data race. Shut the handler down first — close its message channel
// or DoneChan and wait for it to return — then assert.
type MockNodeEnv struct {
	// Config is returned by NodeConfig().
	Config contract.Node

	// MessageChan is returned by Messages(). If nil, a closed channel is used.
	MessageChan <-chan contract.Message

	// DoneChan drives Context() cancellation.
	// If nil, a never-closing channel is used.
	DoneChan <-chan struct{}

	// MockLogger is returned by Logger(). If nil, a no-op logger is used.
	MockLogger contract.Logger

	// PublishErr is returned by Publish, if non-nil.
	PublishErr error

	mu          sync.Mutex
	defaultDone chan struct{}
	defaultCtx  context.Context
	cancelCtx   context.CancelFunc

	// PublishedData is appended to each time Publish is called.
	PublishedData []PublishedMessage

	// ReportedErrors is appended to each time ReportError is called.
	ReportedErrors []ReportedError

	// StopCalled is set to true when Stop() is called.
	StopCalled bool
}

// PublishedMessage records a single Publish call's handle and payload.
type PublishedMessage struct {
	Handle string
	Data   map[string]any
}

// ReportedError holds the arguments passed to a single ReportError call.
type ReportedError struct {
	Code contract.ErrorCode
	Err  error
}

func (m *MockNodeEnv) NodeConfig() contract.Node { return m.Config }

func (m *MockNodeEnv) Messages() <-chan contract.Message {
	if m.MessageChan != nil {
		return m.MessageChan
	}
	ch := make(chan contract.Message)
	close(ch)
	return ch
}

func (m *MockNodeEnv) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.defaultCtx == nil {
		doneCh := m.DoneChan
		if doneCh == nil {
			if m.defaultDone == nil {
				m.defaultDone = make(chan struct{})
			}
			doneCh = m.defaultDone
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.defaultCtx = ctx
		m.cancelCtx = cancel
		go func() {
			<-doneCh
			cancel()
		}()
	}
	return m.defaultCtx
}

func (m *MockNodeEnv) Logger() contract.Logger {
	if m.MockLogger != nil {
		return m.MockLogger
	}
	return logger.NewNoopLogger()
}

func (m *MockNodeEnv) Publish(handle string, data map[string]any) error {
	m.mu.Lock()
	m.PublishedData = append(m.PublishedData, PublishedMessage{Handle: handle, Data: data})
	m.mu.Unlock()
	return m.PublishErr
}

func (m *MockNodeEnv) ReportError(code contract.ErrorCode, err error) {
	m.mu.Lock()
	m.ReportedErrors = append(m.ReportedErrors, ReportedError{Code: code, Err: err})
	m.mu.Unlock()
}

func (m *MockNodeEnv) Stop() {
	m.mu.Lock()
	m.StopCalled = true
	m.mu.Unlock()
}
