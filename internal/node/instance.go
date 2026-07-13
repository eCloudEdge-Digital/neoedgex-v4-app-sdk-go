package node

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

type Instance struct {
	sdk         core.SDK
	logger      contract.Logger
	nodeConfig  contract.Node
	messageChan chan contract.Message
	ctx         context.Context
	cancel      context.CancelFunc
	useRawJson  bool
}

// NewInstance creates a node instance. When useRawJson is true, inbound
// jsonObject/jsonArray fields are delivered to the handler as json.RawMessage
// (validated original bytes) instead of parsed map[string]any/[]any.
func NewInstance(sdk core.SDK, nodeConfig contract.Node, useRawJson bool) (*Instance, error) {
	if sdk == nil {
		return nil, fmt.Errorf("sdk is nil")
	}

	logger := sdk.NewLogger(fmt.Sprintf("Node-%s", nodeConfig.Data.Name))
	logger.Debug("Initializing node instance")

	instanceCtx, instanceCancel := context.WithCancel(sdk.Context())
	return &Instance{
		sdk:         sdk,
		logger:      logger,
		nodeConfig:  nodeConfig,
		messageChan: make(chan contract.Message, 4096),
		ctx:         instanceCtx,
		cancel:      instanceCancel,
		useRawJson:  useRawJson,
	}, nil
}

func (instance *Instance) SDK() core.SDK {
	return instance.sdk
}

func (instance *Instance) Context() context.Context {
	return instance.ctx
}

// NodeConfig returns the raw platform node configuration.
func (instance *Instance) NodeConfig() contract.Node {
	return instance.nodeConfig
}

func (instance *Instance) Logger() contract.Logger {
	return instance.logger
}

// Messages returns a channel of incoming messages.
// The channel is closed when the node shuts down.
func (instance *Instance) Messages() <-chan contract.Message {
	return instance.messageChan
}

// Run starts the instance's MQTT subscription and heartbeat loop, then
// supervises the given handler function with automatic panic recovery and
// restart. It blocks until the instance's context is cancelled.
func (instance *Instance) Run(handler func()) {
	instance.logger.Info("Starting node instance")
	go instance.runLoop()
	instance.superviseHandler(handler)
}

func (instance *Instance) superviseHandler(handler func()) {
	const initialBackoff = 1 * time.Second
	const maxBackoff = 30 * time.Second
	const resetThreshold = 30 * time.Second

	backoff := initialBackoff
	for {
		if instance.ctx.Err() != nil {
			return
		}

		startedAt := time.Now()
		crashed := instance.runHandlerOnce(handler)

		if !crashed {
			return
		}

		if time.Since(startedAt) > resetThreshold {
			backoff = initialBackoff
		}

		instance.logger.Warn("Handler crashed, restarting in %v", backoff)
		instance.ReportError(contract.CodeProcessError, fmt.Errorf("handler crashed, restarting"))

		select {
		case <-instance.ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (instance *Instance) runHandlerOnce(handler func()) (crashed bool) {
	defer func() {
		if r := recover(); r != nil {
			crashed = true
			instance.logger.Error("Handler panicked: %v", r)
		}
	}()

	handler()

	// Handler returned but context still alive → treat as crash
	return instance.ctx.Err() == nil
}

func (instance *Instance) Shutdown() {
	if instance.cancel != nil {
		instance.cancel()
	}
}

// Stop signals the node to shut down cleanly.
// Implements NodeEnv.Stop — allows a handler to declare a fatal error.
func (instance *Instance) Stop() {
	instance.Shutdown()
}

func (instance *Instance) Publish(handle string, data map[string]any) error {
	// Look up the expected output fields from the node config
	desiredOutput, exists := instance.nodeConfig.Data.Outputs[handle]
	if !exists {
		return fmt.Errorf("output handle %q does not exist for node %s", handle, instance.nodeConfig.Data.Name)
	}

	// Warn on tags supplied by the handler that the schema does not define.
	definedKeys := make(map[string]struct{}, len(desiredOutput))
	for _, fieldDef := range desiredOutput {
		definedKeys[fieldDef.Key] = struct{}{}
	}
	for key := range data {
		if _, ok := definedKeys[key]; !ok {
			instance.logger.Warn("Tag %q is not defined in the output schema; dropping", key)
		}
	}

	// Convert handler-facing Go values into the typed NeoFlow output payload.
	portFields := make(map[string]contract.PortFieldData, len(desiredOutput))
	for _, fieldDef := range desiredOutput {
		rawValue, provided := data[fieldDef.Key]
		if !provided {
			instance.logger.Debug("Output field %q not provided, sending nil", fieldDef.Key)
			portFields[fieldDef.Key] = *contract.NewEmptyField()
			continue
		} else if isNilAnyValue(rawValue) {
			instance.logger.Debug("Output field %q provided with nil value, sending nil", fieldDef.Key)
			portFields[fieldDef.Key] = *contract.NewEmptyField()
			continue
		}

		pf, err := contract.NewPortFieldDataWithAny(rawValue, fieldDef.Type)
		if err != nil {
			portFields[fieldDef.Key] = *contract.NewEmptyField()
			instance.ReportError(contract.CodeProcessError, fmt.Errorf("field %q: %w", fieldDef.Key, err))
			continue
		}
		portFields[fieldDef.Key] = *pf
	}

	message := contract.NeoFlowMessage{
		SourceNodeID: instance.nodeConfig.ID,
		Timestamp:    time.Now().Format(time.RFC3339),
		Data:         portFields,
	}

	bytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal neoflow message: %v", err)
	}

	topic := fmt.Sprintf("neoedgex/neoflow/out/%s/%s", instance.nodeConfig.ID, handle)
	return instance.sdk.Messenger().Publish(topic, 2, bytes)
}

func (instance *Instance) PublishNodeError(code contract.ErrorCode, detail error) error {
	nodeError := contract.Error{
		Code:      string(code),
		UpdatedAt: time.Now().Unix(),
	}

	if detail != nil {
		nodeError.Detail = detail.Error()
	}

	bytes, err := json.Marshal(nodeError)
	if err != nil {
		return fmt.Errorf("failed to marshal node error: %v", err)
	}
	topic := fmt.Sprintf("neoedgex/neoflow/error/%s", instance.nodeConfig.ID)
	return instance.sdk.Messenger().Publish(topic, 0, bytes)
}

// ReportError publishes a node error to the platform.
func (instance *Instance) ReportError(code contract.ErrorCode, err error) {
	if publishErr := instance.PublishNodeError(code, err); publishErr != nil {
		instance.logger.Warn("Failed to publish node error: %v", publishErr)
	}
}

func (instance *Instance) PublishHeartbeat() error {
	topic := fmt.Sprintf("neoedgex/neoflow/heartbeat/%s", instance.nodeConfig.ID)
	return instance.sdk.Messenger().Publish(topic, 0, []byte{})
}

func decodeIncomingData(data map[string]contract.PortFieldData, useRawJson bool) map[string]any {
	decoded := make(map[string]any, len(data))
	for key, field := range data {
		if field.Type == contract.TypeUndefined {
			decoded[key] = nil
			continue
		}
		var value any
		var err error
		if useRawJson {
			// Raw mode: json types are validated then returned as json.RawMessage;
			// all other types decode exactly as the default path.
			value, err = contract.ConvertValueByTypeRaw(field.Value, field.Type)
		} else {
			value, err = field.GetAnyValue()
		}
		if err != nil {
			decoded[key] = nil
			continue
		}
		decoded[key] = value
	}
	return decoded
}

func isNilAnyValue(anyValue any) bool {
	if anyValue == nil {
		return true
	}

	value := reflect.ValueOf(anyValue)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (instance *Instance) runLoop() {
	defer func() {
		close(instance.messageChan)
		instance.sdk.Messenger().RemoveSubscriber(instance.nodeConfig.ID)
	}()

	subscriptionChannel := instance.sdk.Messenger().AddSubscriber(instance.nodeConfig.ID)
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-instance.ctx.Done():
			instance.logger.Info("Context done, exiting run loop")
			return
		case <-heartbeatTicker.C:
			if err := instance.PublishHeartbeat(); err != nil {
				instance.logger.Warn("Failed to publish heartbeat: %v", err)
			}
		case payload, ok := <-subscriptionChannel:
			if !ok {
				instance.logger.Info("Subscription channel closed, exiting run loop")
				return
			}

			var neoflowMessage contract.NeoFlowMessage
			if err := json.Unmarshal(payload.Data, &neoflowMessage); err != nil {
				instance.logger.Error("Failed to unmarshal neoflow message: %v", err)
				continue
			}

			select {
			case <-instance.ctx.Done():
				instance.logger.Info("Context done, exiting run loop")
			case instance.messageChan <- contract.Message{
				Handle:    payload.Handle,
				Data:      decodeIncomingData(neoflowMessage.Data, instance.useRawJson),
				Source:    neoflowMessage.SourceNodeID,
				Timestamp: neoflowMessage.Timestamp,
			}:
			default:
				err := fmt.Errorf("message channel is full, dropping incoming message")
				instance.logger.Warn(err.Error())
				instance.ReportError(contract.CodeProcessError, err)
			}
		}
	}
}
