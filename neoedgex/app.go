package neoedgex

import (
	"fmt"
	"sync"

	internalNode "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/node"
	internalSDK "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/internal/sdk"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/mock"
)

// MockConfig 是 mock.MockConfig 的別名，方便使用者不需額外 import mock package。
type MockConfig = mock.MockConfig

// MockSection 是 mock.MockSection 的別名。
type MockSection = mock.MockSection

// MockMessage 是 mock.MockMessage 的別名。
type MockMessage = mock.MockMessage

// LoadMockConfig 從檔案讀取並解析 mock 設定，是 mock.LoadConfig 的便捷入口。
func LoadMockConfig(path string) (*MockConfig, error) {
	return mock.LoadConfig(path)
}

// App is the main entry point for a NeoEdgeX node application.
type App struct {
	handler       NodeHandler
	wg            sync.WaitGroup // tracks running instance goroutines
	mockConfig    *mock.MockConfig
	disableSDKLog bool
}

// New creates a new App with the given handler.
// Panics if handler is nil.
func New(handler NodeHandler) *App {
	if handler == nil {
		panic("neoedgex: handler must not be nil")
	}
	return &App{
		handler: handler,
	}
}

// DisableSDKLog 停用 SDK 內部所有 log 輸出（含 node instance）。
// 預設為開啟；呼叫此方法可讓開發者自行決定是否需要 SDK 內部 log。
func (app *App) DisableSDKLog() *App {
	app.disableSDKLog = true
	return app
}

// EnableMock 開啟 mock 模式，使用提供的 MockConfig 設定節點和假訊息。
// 開發時加上這行，正式部署時移除即可。
//
//	config, _ := mock.LoadConfig("./mock-config.json")
//	app.EnableMock(config)
func (app *App) EnableMock(config *mock.MockConfig) *App {
	app.mockConfig = config
	return app
}

// Run initializes the SDK, starts all matched node handlers,
// and blocks until the process receives SIGTERM / SIGINT and all handlers exit.
func (app *App) Run() error {
	s := internalSDK.NewSDK()

	if app.disableSDKLog {
		s.DisableLog()
	}

	// mock 模式：設定 node configs、messenger、訊息注入
	if app.mockConfig != nil {
		s.EnableMock(app.mockConfig)
	}

	if err := s.Initialize(); err != nil {
		return fmt.Errorf("neoedgex: failed to initialize SDK: %w", err)
	}

	// 啟動定期訊息注入（非 mock 模式為 no-op）
	s.StartMessageInjection()

	appLogger := s.NewLogger("App")

	// RunWithReady connects the messenger first, then calls the callback.
	// Instances are started inside the callback so that any publish on startup
	// (e.g. ReportError after an immediate handler return) happens only after
	// the connection is fully established.
	err := s.Run(func() {
		for _, nodeConfig := range s.NodeConfigs() {
			instance, err := internalNode.NewInstance(s, nodeConfig)
			if err != nil {
				appLogger.Warn("Skipping node %s: %v", nodeConfig.Data.Name, err)
				continue
			}

			app.wg.Add(1)
			go func() {
				defer app.wg.Done()
				instance.Run(func() {
					app.handler.Handle(instance)
				})
			}()
		}
	})
	app.wg.Wait() // wait for all supervisor goroutines to exit after shutdown
	return err
}
