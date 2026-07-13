package messenger

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"maps"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	sdk                 core.SDK
	logger              contract.Logger
	mutex               sync.Mutex
	options             core.MessengerOptions
	subscriptionManager subscriptionManager

	mqttClient        mqtt.Client
	resubscribeCtx    context.Context
	cancelResubscribe context.CancelFunc
}

var _ core.MessengerClient = (*Client)(nil)

func NewMessenger(sdk core.SDK, options *core.MessengerOptions) *Client {
	if options == nil {
		options = &core.MessengerOptions{}
	}
	if options.Broker == "" {
		options.Broker = "neoedgex-messenger"
	}
	if options.Port == 0 {
		options.Port = 1883
	}
	if options.ResubscribeInterval == 0 {
		options.ResubscribeInterval = 1 * time.Second
	}

	client := &Client{
		sdk:                 sdk,
		logger:              sdk.NewLogger("MessengerContext"),
		options:             *options,
		subscriptionManager: newSubscriptionManager(),
	}

	return client
}

// paho.mqtt
func (client *Client) onConnect(mqttClient mqtt.Client) {
	client.logger.Info("Connected to NeoEdgeX Messenger")
	// Restart resubscribe loop on every (re)connect so subscriptions recover after auto-reconnect.
	client.startResubscribeLoop()
}

func (client *Client) onConnectionLost(mqttClient mqtt.Client, err error) {
	client.logger.Error("Connection to NeoEdgeX Messenger lost: %v", err)
	client.mutex.Lock()
	if client.cancelResubscribe != nil {
		client.cancelResubscribe()
		client.cancelResubscribe = nil
	}
	client.mutex.Unlock()
	client.cleanup()
}

func (client *Client) createMessageHandler(subscriptionChannel chan core.RawMessengerPayload) mqtt.MessageHandler {
	return func(mqttClient mqtt.Client, msg mqtt.Message) {
		// Guard against sends to closed channels
		defer func() {
			if r := recover(); r != nil {
				client.logger.Warn("Subscriber channel closed; dropping incoming message")
			}
		}()
		// Parse topic to get input handle
		_, inputHandle, err := client.parseTopic(msg.Topic())
		if err != nil {
			client.logger.Error("Failed to parse topic %s: %v", msg.Topic(), err)
			return
		}

		select {
		case subscriptionChannel <- core.RawMessengerPayload{
			Handle: inputHandle,
			Data:   msg.Payload(),
		}:
		default:
			client.logger.Warn("Subscription channel is full, dropping incoming message")
		}

	}
}

// contract.NeoEdgeXMessenger
func (client *Client) Connect() error {
	return client.connect()
}

func (client *Client) Disconnect() {
	client.mutex.Lock()
	mqttClientSnapshot := client.mqttClient
	cancelResubscribe := client.cancelResubscribe
	client.cancelResubscribe = nil
	client.mutex.Unlock()

	if cancelResubscribe != nil {
		cancelResubscribe()
	}

	if mqttClientSnapshot != nil && mqttClientSnapshot.IsConnected() {
		mqttClientSnapshot.Disconnect(250)
		client.logger.Info("Disconnected from NeoEdgeX Messenger")
	}

	client.cleanup()
}

func (client *Client) AddSubscriber(nodeID string) <-chan core.RawMessengerPayload {
	client.mutex.Lock()
	defer client.mutex.Unlock()

	if existing, ok := client.subscriptionManager.desired[nodeID]; ok {
		return existing
	}

	subscriptionChannel := make(chan core.RawMessengerPayload, 32)
	client.subscriptionManager.desired[nodeID] = subscriptionChannel
	return subscriptionChannel
}

func (client *Client) RemoveSubscriber(nodeID string) {
	client.mutex.Lock()
	ch, exists := client.subscriptionManager.desired[nodeID]
	if !exists {
		client.mutex.Unlock()
		return
	}

	// Remove from tracking maps
	delete(client.subscriptionManager.desired, nodeID)
	delete(client.subscriptionManager.actual, nodeID)
	mqttClientSnapshot := client.mqttClient
	client.mutex.Unlock()

	if mqttClientSnapshot != nil && mqttClientSnapshot.IsConnected() {
		subscribedTopic := client.createInputTopic(nodeID)
		if token := mqttClientSnapshot.Unsubscribe(subscribedTopic); token.Wait() && token.Error() != nil {
			client.logger.Error("Failed to unsubscribe from topic %s: %v", subscribedTopic, token.Error())
		} else {
			client.logger.Info("Unsubscribed from topic %s successfully", subscribedTopic)
		}
	}

	// Close channel to signal lifecycle end to consumers
	close(ch)
}

func (client *Client) Publish(topic string, qos byte, data []byte) error {
	if client.mqttClient == nil || !client.mqttClient.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	if token := client.mqttClient.Publish(topic, qos, false, data); !token.WaitTimeout(5 * time.Second) {
		client.logger.Error("publish to topic %s timed out", topic)
		return fmt.Errorf("publish to topic %s timed out", topic)
	} else if token.Error() != nil {
		client.logger.Error("Failed to publish message to topic %s: %v", topic, token.Error())
		return fmt.Errorf("failed to publish message to topic %s: %w", topic, token.Error())
	} else {
		client.logger.Debug("Published message to topic %s: %s", topic, string(data))
	}

	return nil
}

// Member Functions
func (client *Client) createInputTopic(nodeID string) string {
	return fmt.Sprintf("neoedgex/neoflow/in/%s/+", nodeID)
}

func (client *Client) connect() error {
	if client.options.Config == nil {
		return fmt.Errorf("messenger config is nil")
	}

	client.mutex.Lock()
	if client.mqttClient == nil {
		mqttOptions := mqtt.NewClientOptions()
		mqttOptions.AddBroker(fmt.Sprintf("tcp://%s:%d", client.options.Broker, client.options.Port))
		mqttOptions.SetUsername(client.options.Config.Username)
		mqttOptions.SetPassword(client.options.Config.Password)
		mqttOptions.SetConnectRetry(true)
		mqttOptions.SetConnectRetryInterval(1 * time.Second)
		mqttOptions.SetConnectTimeout(5 * time.Second)
		mqttOptions.SetOnConnectHandler(client.onConnect)
		mqttOptions.SetConnectionLostHandler(client.onConnectionLost)

		client.mqttClient = mqtt.NewClient(mqttOptions)
	} else if client.mqttClient.IsConnected() {
		client.mutex.Unlock()
		return nil
	}
	mqttClientSnapshot := client.mqttClient
	client.mutex.Unlock()

	client.logger.Debug("NeoEdgeX Messenger is not connected, attempting to connect")
	if token := mqttClientSnapshot.Connect(); token.Wait() && token.Error() != nil {
		err := fmt.Errorf("failed to connect to NeoEdgeX Messenger: %w", token.Error())
		client.logger.Error("Connection attempt failed: %v", err)
		return err
	}

	return nil
}

func (client *Client) startResubscribeLoop() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.cancelResubscribe != nil {
		client.cancelResubscribe()
		client.cancelResubscribe = nil
	}
	client.resubscribeCtx, client.cancelResubscribe = context.WithCancel(client.sdk.Context())
	go func() {
		task := func() {
			// Snapshot state
			client.mutex.Lock()
			mqttClientSnapshot := client.mqttClient
			desiredCount := len(client.subscriptionManager.desired)
			actualCount := len(client.subscriptionManager.actual)
			client.mutex.Unlock()

			if mqttClientSnapshot == nil || !mqttClientSnapshot.IsConnected() {
				client.logger.Debug("MQTT client is not connected, skipping resubscribe")
			} else if desiredCount == actualCount {
				// All nodes are already subscribed
			} else {
				client.resubscribeAll()
			}
		}
		task()

		timer := time.NewTimer(client.options.ResubscribeInterval)
		defer timer.Stop()

		for {
			select {
			case <-client.resubscribeCtx.Done():
				client.logger.Info("Resubscribing monitoring stopped")
				return
			case <-timer.C:
				task()
				timer.Reset(client.options.ResubscribeInterval)
			}
		}
	}()
}

func (client *Client) cleanup() {
	client.mutex.Lock()
	defer client.mutex.Unlock()

	client.subscriptionManager.actual = make(map[string]struct{})
}

func (client *Client) resubscribeAll() {
	// Clone maps and snapshot client to avoid holding lock during network operations
	client.mutex.Lock()

	mqttClientSnapshot := client.mqttClient
	desiredClone := make(map[string]chan core.RawMessengerPayload)
	maps.Copy(desiredClone, client.subscriptionManager.desired)
	actualClone := make(map[string]struct{})
	maps.Copy(actualClone, client.subscriptionManager.actual)
	client.mutex.Unlock()

	// Resubscribe to all nodes
	for nodeID, subscriptionChannel := range desiredClone {
		if _, subscribed := actualClone[nodeID]; subscribed {
			continue
		}

		subscribedTopic := client.createInputTopic(nodeID)
		if mqttClientSnapshot == nil || !mqttClientSnapshot.IsConnected() {
			client.logger.Debug("MQTT client snapshot disconnected; aborting resubscribe")
			break
		}
		if token := mqttClientSnapshot.Subscribe(subscribedTopic, 2, client.createMessageHandler(subscriptionChannel)); token.Wait() && token.Error() != nil {
			client.logger.Error("Failed to subscribe to topic %s: %v", subscribedTopic, token.Error())
			continue
		} else {
			client.logger.Info("Subscribed to topic %s successfully", subscribedTopic)
			actualClone[nodeID] = struct{}{}
		}
	}

	// Update subscribed nodes map
	client.mutex.Lock()
	client.subscriptionManager.actual = actualClone
	client.mutex.Unlock()
}

func (client *Client) parseTopic(topic string) (string, string, error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 5 {
		return "", "", fmt.Errorf("invalid topic format: %s", topic)
	}

	nodeID := parts[3]
	inputHandle := parts[4]
	return nodeID, inputHandle, nil
}
