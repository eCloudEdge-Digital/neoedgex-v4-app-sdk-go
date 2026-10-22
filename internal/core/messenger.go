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
	// Host and Port are written by the agent only for host-network apps; zero values keep the defaults.
	Host string `json:"host"`
	Port int    `json:"port"`
}
