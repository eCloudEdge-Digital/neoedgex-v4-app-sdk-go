package neoedgex

import (
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// Node represents the raw NeoEdgeX node configuration.
type Node = contract.Node

// Message is an incoming NeoFlow message delivered to a handler.
type Message = contract.Message

// Logger is the logging interface provided to node handlers.
type Logger = contract.Logger

type ErrorCode = contract.ErrorCode

const (
	CodeInitializationError = contract.CodeInitializationError
	CodeNetworkError        = contract.CodeNetworkError
	CodeProcessError        = contract.CodeProcessError
)
