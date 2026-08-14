package node

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// publishTimestampLayout is RFC3339 with a fixed three-digit fraction. Tags are
// polled on millisecond intervals, so a second-precision stamp collapses every
// sample taken within the same second into one indistinguishable time.
//
// The fraction is fixed-width on purpose: time.RFC3339Nano trims trailing
// zeros and drops the fraction entirely on a whole second, which would hand
// consumers a variable-length string. Fixed width also keeps the format
// backward compatible in both directions — a parser reading time.RFC3339
// accepts the fraction, and a second-precision stamp from an older publisher
// still parses here.
const publishTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

type Instance struct {
	sdk core.SDK
	// logger carries SDK machinery output and is silenced by App.DisableSDKLog;
	// handlerLogger is what the application writes through and is never
	// silenced. Keep the split when adding log calls: anything the SDK says
	// about itself goes to logger.
	logger        contract.Logger
	handlerLogger contract.Logger
	nodeConfig    contract.Node
	messageChan   chan contract.Message
	inputPlans    map[string]contract.DecodePlan
	outputKeys    map[string]map[string]struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewInstance(sdk core.SDK, nodeConfig contract.Node) (*Instance, error) {
	if sdk == nil {
		return nil, fmt.Errorf("sdk is nil")
	}

	tag := fmt.Sprintf("Node-%s", nodeConfig.Data.Name)
	logger := sdk.NewLogger(tag)
	logger.Debug("Initializing node instance")

	inputPlans := make(map[string]contract.DecodePlan, len(nodeConfig.Data.Inputs))
	for handle, schema := range nodeConfig.Data.Inputs {
		inputPlans[handle] = contract.NewDecodePlan(schema)
	}
	outputKeys := make(map[string]map[string]struct{}, len(nodeConfig.Data.Outputs))
	for handle, schema := range nodeConfig.Data.Outputs {
		keys := make(map[string]struct{}, len(schema))
		for _, fieldDef := range schema {
			keys[fieldDef.Key] = struct{}{}
		}
		outputKeys[handle] = keys
	}

	instanceCtx, instanceCancel := context.WithCancel(sdk.Context())
	return &Instance{
		sdk:           sdk,
		logger:        logger,
		handlerLogger: sdk.NewHandlerLogger(tag),
		nodeConfig:    nodeConfig,
		messageChan:   make(chan contract.Message, 4096),
		inputPlans:    inputPlans,
		outputKeys:    outputKeys,
		ctx:           instanceCtx,
		cancel:        instanceCancel,
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
	return instance.handlerLogger
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
	definedKeys := instance.outputKeys[handle]
	for key := range data {
		if _, ok := definedKeys[key]; !ok {
			instance.logger.Warn("Tag %q is not defined in the output schema; dropping", key)
		}
	}

	// Convert handler-facing Go values into schema-typed native values;
	// undefined (missing/nil/conversion-failed) fields go out as CBOR null.
	dataMap := make(map[string]any, len(desiredOutput))
	for _, fieldDef := range desiredOutput {
		rawValue, provided := data[fieldDef.Key]
		if !provided {
			instance.logger.Debug("Output field %q not provided, sending nil", fieldDef.Key)
			dataMap[fieldDef.Key] = nil
			continue
		} else if isNilAnyValue(rawValue) {
			instance.logger.Debug("Output field %q provided with nil value, sending nil", fieldDef.Key)
			dataMap[fieldDef.Key] = nil
			continue
		}

		typedValue, err := contract.ConvertToTypedValue(rawValue, fieldDef.Type)
		if err != nil {
			dataMap[fieldDef.Key] = nil
			instance.ReportError(contract.CodeProcessError, fmt.Errorf("field %q (value type '%T'): %w", fieldDef.Key, rawValue, err))
			continue
		}
		dataMap[fieldDef.Key] = typedValue
	}

	dataBytes, err := cbor.Marshal(dataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal neoflow data: %v", err)
	}

	message := contract.NeoFlowMessage{
		SourceNodeID: instance.nodeConfig.ID,
		Timestamp:    time.Now().Format(publishTimestampLayout),
		Data:         dataBytes,
	}

	bytes, err := cbor.Marshal(message)
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
			if err := cbor.Unmarshal(payload.Data, &neoflowMessage); err != nil {
				err = fmt.Errorf("failed to unmarshal neoflow message: %v", err)
				instance.logger.Error(err.Error())
				instance.ReportError(contract.CodeProcessError, err)
				continue
			}

			// O(1) wire gate: the data segment must be a CBOR map (major type
			// 5, incl. indefinite-length 0xbf); anything else is dropped whole.
			if len(neoflowMessage.Data) == 0 || neoflowMessage.Data[0]&0xe0 != 0xa0 {
				err := fmt.Errorf("neoflow message data segment is not a CBOR map, dropping message")
				instance.logger.Error(err.Error())
				instance.ReportError(contract.CodeProcessError, err)
				continue
			}

			select {
			case <-instance.ctx.Done():
				instance.logger.Info("Context done, exiting run loop")
			case instance.messageChan <- contract.NewMessage(
				neoflowMessage.SourceNodeID,
				neoflowMessage.Timestamp,
				payload.Handle,
				contract.RawMessage(neoflowMessage.Data),
				instance.inputPlans[payload.Handle],
				instance.logger,
			):
			default:
				err := fmt.Errorf("message channel is full, dropping incoming message")
				instance.logger.Warn(err.Error())
				instance.ReportError(contract.CodeProcessError, err)
			}
		}
	}
}
