package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/logger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/messenger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock"
)

// mountPath is the container mount root written by the platform, not an SDK choice:
// the platform provides config/config.json (node configs) and config/messenger.json
// (MQTT credentials) underneath it.
const mountPath = "/opt/neoedgex"

type sdk struct {
	nodeConfigs []contract.Node
	messenger   core.MessengerClient
	logger      contract.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.Mutex
	isRunning   bool
	disableLog  bool

	// mock 狀態
	mockMessenger *mockMessenger
	mockMessages  []mock.MockMessage
	mockInterval  time.Duration
}

var _ core.SDK = (*sdk)(nil)

// NewSDK creates a new SDK instance.
// Call EnableMock() before Initialize() to use mock mode.
func NewSDK() *sdk {
	return &sdk{}
}

// DisableLog silences every logger the SDK uses for its own machinery,
// including each node instance's internal log. Loggers handed to application
// code by NewHandlerLogger are not affected. Must be called before
// Initialize().
func (s *sdk) DisableLog() {
	s.disableLog = true
}

// Initialize 初始化 SDK（context、logger、設定讀取）。
// 若已呼叫 EnableMock，會跳過檔案讀取。
func (s *sdk) Initialize() error {
	return s.initialize()
}

// EnableMock 啟用 mock 模式：使用記憶體 messenger 和提供的 node 設定。
// 必須在 Initialize() 之前呼叫。
func (s *sdk) EnableMock(config *mock.MockConfig) {
	s.nodeConfigs = config.Nodes
	s.mockMessenger = newMockMessenger(s.NewLogger("Mock"))
	s.messenger = s.mockMessenger
	s.mockMessages = config.Mock.Messages

	if d, err := time.ParseDuration(config.Mock.MessageInterval); err == nil && d > 0 {
		s.mockInterval = d
	}
}

// StartMessageInjection 啟動定期訊息注入（mock 模式）。
func (s *sdk) StartMessageInjection() {
	if s.mockMessenger == nil || len(s.mockMessages) == 0 {
		return
	}

	interval := s.mockInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}

	go func() {
		// 等待一小段時間讓 subscriber 註冊完成
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		idx := 0
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				msg := s.mockMessages[idx]
				s.logger.Info("[MOCK INJECT] → node=%s handle=%s", msg.NodeID, msg.Handle)
				if err := s.mockMessenger.injectNeoFlowMessage(msg.NodeID, msg.Handle, msg.Data); err != nil {
					s.logger.Warn("[MOCK INJECT] error: %v", err)
				}
				idx = (idx + 1) % len(s.mockMessages)
			}
		}
	}()
}

// core.SDK
func (s *sdk) Context() context.Context {
	return s.ctx
}

func (s *sdk) NodeConfigs() []contract.Node {
	return s.nodeConfigs
}

func (s *sdk) Messenger() core.MessengerClient {
	return s.messenger
}

func (s *sdk) NewLogger(tag string) contract.Logger {
	if s.disableLog {
		return logger.NewNoopLogger()
	}
	return logger.NewLogger(tag)
}

func (s *sdk) NewHandlerLogger(tag string) contract.Logger {
	return logger.NewLogger(tag)
}

// Run connects the messenger, calls fn (if non-nil) once the connection is
// established, then blocks until the context is cancelled.
func (s *sdk) Run(fn func()) error {
	// Prevent multiple runs
	s.mutex.Lock()
	if s.isRunning {
		s.mutex.Unlock()
		return fmt.Errorf("sdk is already running")
	}

	s.isRunning = true
	s.mutex.Unlock()
	defer func() {
		s.mutex.Lock()
		s.isRunning = false
		s.mutex.Unlock()
	}()

	// Connect messenger
	if err := s.messenger.Connect(); err != nil {
		return fmt.Errorf("failed to connect messenger: %w", err)
	}
	defer s.messenger.Disconnect()

	if fn != nil {
		fn()
	}

	// Wait for context done
	<-s.ctx.Done()
	s.logger.Info("Context done, exiting run loop")

	return nil
}

func (s *sdk) Shutdown() {
	s.cancel()
}

func (s *sdk) initialize() error {
	// Root context
	s.ctx, s.cancel = signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	// Logger
	s.logger = s.NewLogger("SDK")

	// 若已由 EnableMock 設定，跳過檔案讀取
	if s.messenger != nil {
		s.logger.Info("[MOCK] Mock mode enabled with %d node(s), %d mock message(s)",
			len(s.nodeConfigs), len(s.mockMessages))
		return nil
	}

	// Node Configs — 從檔案讀取
	rawNodeConfigs, err := os.ReadFile(mountPath + "/config/config.json")
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}
	if err = json.Unmarshal(rawNodeConfigs, &s.nodeConfigs); err != nil {
		return fmt.Errorf("failed to unmarshal config file: %v", err)
	}

	// Messenger — 從檔案讀取 MQTT 設定
	messengerOption := &core.MessengerOptions{Config: &core.MessengerConfig{}}
	rawMessengerConfig, err := os.ReadFile(mountPath + "/config/messenger.json")
	if err != nil {
		return fmt.Errorf("failed to read messenger config file: %v", err)
	}
	if err = json.Unmarshal(rawMessengerConfig, messengerOption.Config); err != nil {
		return fmt.Errorf("failed to unmarshal messenger config file: %v", err)
	}
	s.messenger = messenger.NewMessenger(s, messengerOption)

	return nil
}
