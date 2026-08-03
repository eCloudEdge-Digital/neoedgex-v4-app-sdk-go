package neoedgex

import (
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// Node represents the raw NeoEdgeX node configuration.
type Node = contract.Node

// Message is an incoming NeoFlow message delivered to a handler.
type Message = contract.Message

// Logger is the logging interface provided to node handlers.
// Its methods take printf arguments — msg is a format string rendered with
// fmt.Sprintf, not a message followed by key/value pairs.
type Logger = contract.Logger

// ErrorCode classifies a node error passed to NodeEnv.ReportError.
type ErrorCode = contract.ErrorCode

// The error codes to pass to NodeEnv.ReportError.
const (
	// CodeInitializationError: the node cannot start at all. Report it, then
	// call NodeEnv.Stop.
	CodeInitializationError = contract.CodeInitializationError
	// CodeNetworkError: a call to an external endpoint failed; the node stays
	// alive and may recover.
	CodeNetworkError = contract.CodeNetworkError
	// CodeProcessError: processing one message or one unit of work failed.
	// The SDK reports this code itself too, for undecodable and dropped
	// messages and for handler crashes.
	CodeProcessError = contract.CodeProcessError
)
