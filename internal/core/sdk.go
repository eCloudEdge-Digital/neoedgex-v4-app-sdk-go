package core

import (
	"context"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

type SDK interface {
	Context() context.Context
	NodeConfigs() []contract.Node
	Messenger() MessengerClient
	NewLogger(tag string) contract.Logger
	Shutdown()
}
