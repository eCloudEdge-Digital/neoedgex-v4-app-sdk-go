package neoedgex

import "context"

// NodeHandler is the interface that users must implement.
// The SDK calls Handle in a dedicated goroutine for every node in the
// configuration — there is no matching or filtering — so one handler value
// serves all of them and must be safe for concurrent use.
type NodeHandler interface {
	// Handle is invoked once per configured node in its own goroutine.
	// When the node shuts down, ctx.Messages() is closed and Handle should return.
	//
	// Returning while ctx.Context() is still alive counts as a crash instead:
	// the SDK reports a CodeProcessError and calls Handle again after a
	// backoff starting at 1s and doubling up to 30s. A panic is recovered and
	// treated the same way. To stop for good, call ctx.Stop() before
	// returning.
	Handle(ctx NodeEnv)
}

// NodeEnv is the interface provided to node handlers.
type NodeEnv interface {
	// NodeConfig returns the raw platform node configuration,
	// including settings (Data.Settings) and output schema (Data.Outputs).
	NodeConfig() Node

	// Messages returns a channel of incoming messages.
	// The channel is closed when the node shuts down.
	//
	// The channel is buffered at 4096. Once it is full — a handler consuming
	// slower than messages arrive — further messages are dropped, not queued
	// or blocked on. Each drop is reported as a CodeProcessError, but the
	// message itself is gone.
	Messages() <-chan Message

	// Context returns the context for this node instance.
	// Use this to propagate cancellation to standard library calls (HTTP, DB, gRPC, etc.).
	Context() context.Context

	// Logger returns this node's logger, tagged with the node name. Its
	// methods take printf arguments, not key/value pairs.
	//
	// It is the application's own log channel: App.DisableSDKLog silences the
	// SDK's output but never this.
	Logger() Logger

	// Publish sends output data to downstream nodes via the specified output handle.
	// The handle must be defined in the node configuration's Outputs schema;
	// values in data are automatically converted to the types declared there.
	Publish(handle string, data map[string]any) error

	// ReportError publishes a node error to the platform.
	//
	// It reports nothing back: there is no return value, and a failed publish
	// is only written to the node log, so a caller cannot tell whether the
	// platform received the error.
	ReportError(code ErrorCode, err error)

	// Stop signals the node to shut down cleanly.
	// Use this to declare a fatal, unrecoverable error from within the handler.
	//
	// It cancels this node's Context() and nothing else: sibling nodes keep
	// running and App.Run does not return, so the process still exits only on
	// a signal.
	Stop()
}
