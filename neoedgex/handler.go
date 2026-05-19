package neoedgex

import "context"

// NodeHandler is the interface that users must implement.
// The SDK calls Handle() in a dedicated goroutine for each matched node.
type NodeHandler interface {
	// Handle is invoked once per matched node in its own goroutine.
	// When the node shuts down, ctx.Messages() is closed and Handle should return.
	Handle(ctx NodeEnv)
}

// NodeEnv is the interface provided to node handlers.
type NodeEnv interface {
	// NodeConfig returns the raw platform node configuration,
	// including settings (Data.Settings) and output schema (Data.Outputs).
	NodeConfig() Node

	// Messages returns a channel of incoming messages.
	// The channel is closed when the node shuts down.
	Messages() <-chan Message

	// Context returns the context for this node instance.
	// Use this to propagate cancellation to standard library calls (HTTP, DB, gRPC, etc.).
	Context() context.Context

	// Logger returns the SDK logger for this node, tagged with the node name.
	Logger() Logger

	// Publish sends output data to downstream nodes via the specified output handle.
	// The handle must be defined in the node configuration's Outputs schema;
	// values in data are automatically converted to the types declared there.
	Publish(handle string, data map[string]any) error

	// ReportError publishes a node error to the platform.
	ReportError(code ErrorCode, err error)

	// Stop signals the node to shut down cleanly.
	// Use this to declare a fatal, unrecoverable error from within the handler.
	Stop()
}
