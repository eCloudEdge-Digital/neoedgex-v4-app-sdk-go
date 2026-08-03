package core

import (
	"context"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

type SDK interface {
	Context() context.Context
	NodeConfigs() []contract.Node
	Messenger() MessengerClient

	// NewLogger builds a logger for SDK machinery. App.DisableSDKLog silences
	// everything it returns.
	NewLogger(tag string) contract.Logger

	// NewHandlerLogger builds the logger handed to application code through
	// NodeEnv.Logger(). App.DisableSDKLog must never silence it: the app's own
	// lines are not SDK output.
	NewHandlerLogger(tag string) contract.Logger

	Shutdown()
}
