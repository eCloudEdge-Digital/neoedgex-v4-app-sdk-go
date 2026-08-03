package contract

// ErrorCode classifies a node error reported through NodeEnv.ReportError.
type ErrorCode string

const (
	// CodeInitializationError means the node cannot start at all: unusable
	// settings, a missing credential, a schema the app does not support.
	// Report it and then call NodeEnv.Stop.
	CodeInitializationError ErrorCode = "INITIALIZATION_ERROR"

	// CodeNetworkError means a call to an external endpoint (HTTP, database,
	// broker, device) failed. The node stays alive and may recover.
	CodeNetworkError ErrorCode = "NETWORK_ERROR"

	// CodeProcessError means processing one message or one unit of work
	// failed. The SDK also reports this code itself, for an undecodable
	// inbound message, a message dropped because the buffer was full, an
	// output value that does not fit its schema type, and a handler crash.
	CodeProcessError ErrorCode = "PROCESS_ERROR"
)
