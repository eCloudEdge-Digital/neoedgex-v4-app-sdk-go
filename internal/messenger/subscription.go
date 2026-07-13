package messenger

import (
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
)

type subscriptionManager struct {
	desired map[string]chan core.RawMessengerPayload
	actual  map[string]struct{}
}

func newSubscriptionManager() subscriptionManager {
	return subscriptionManager{
		desired: make(map[string]chan core.RawMessengerPayload),
		actual:  make(map[string]struct{}),
	}
}
