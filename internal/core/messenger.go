package core

import "time"

type MessengerClient interface {
	Connect() error
	Disconnect()
	AddSubscriber(nodeID string) <-chan RawMessengerPayload
	RemoveSubscriber(nodeID string)
	Publish(topic string, qos byte, data []byte) error
}

type MessengerOptions struct {
	Config              *MessengerConfig
	Broker              string
	Port                int
	ResubscribeInterval time.Duration
}

type RawMessengerPayload struct {
	Handle string
	Data   []byte
}

type MessengerConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
