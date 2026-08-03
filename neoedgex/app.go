// Package neoedgex is the public SDK for building NeoEdgeX node applications
// such as drivers, protocol adapters, forwarders and processors.
//
// An application implements NodeHandler and starts the SDK with New(...).Run().
// Run reads the node configuration the platform supplies, connects to the
// message transport, and runs Handle in its own goroutine for every configured
// node, supervising each until the process is signalled.
//
//	type Forwarder struct{}
//
//	func (Forwarder) Handle(ctx neoedgex.NodeEnv) {
//		for msg := range ctx.Messages() {
//			if err := ctx.Publish("output1", msg.ToMap()); err != nil {
//				ctx.ReportError(neoedgex.CodeProcessError, err)
//			}
//		}
//	}
//
//	func main() {
//		if err := neoedgex.New(Forwarder{}).Run(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// Everything a handler needs arrives through its NodeEnv: NodeConfig for the
// node's settings and port schemas, Messages for the inbound stream, Publish
// for outbound data, ReportError for failures, Logger for node-scoped logs,
// Context for cancellation and Stop to shut the node down.
//
// The types a handler touches — Node, Message, Logger and ErrorCode — are
// aliased here; schema typing (contract.DataType and the Type* constants)
// lives in neoedgex/contract. For local development without the platform, see
// EnableMock and package neoedgex/mock; for unit tests, package
// neoedgex/testutil provides a NodeEnv double.
package neoedgex

import (
	"fmt"
	"sync"

	internalNode "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/node"
	internalSDK "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/sdk"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock"
)

// MockConfig is an alias for mock.MockConfig, so a caller need not import the
// mock package.
type MockConfig = mock.MockConfig

// MockSection is an alias for mock.MockSection.
type MockSection = mock.MockSection

// MockMessage is an alias for mock.MockMessage.
type MockMessage = mock.MockMessage

// LoadMockConfig reads and parses a mock configuration file. It is a
// convenience entry point for mock.LoadConfig and shares its rules, including
// the requirement that the file declare at least one node.
func LoadMockConfig(path string) (*MockConfig, error) {
	return mock.LoadConfig(path)
}

// App is the entry point of a NeoEdgeX node application: New builds one around
// a handler and Run drives it. Platform-facing configuration — the MQTT
// connection and the config file locations — comes from the runtime and is
// deliberately not settable here.
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

// DisableSDKLog silences the log output the SDK produces about itself:
// startup, the message loop, publish and decode diagnostics, and the mock
// machinery. SDK logging is on by default; call this before Run to turn it
// off.
//
// It does NOT silence the application's own logging. Lines written through
// NodeEnv.Logger() keep coming out, so an app can quiet the SDK without losing
// its own trace.
func (app *App) DisableSDKLog() *App {
	app.disableSDKLog = true
	return app
}

// EnableMock switches the App into mock mode: node configurations and injected
// fake messages come from config instead of the platform, and no broker
// connection is made. Add the call during development and remove it before
// deployment.
//
// Mock is strictly opt-in, and passing a nil config is a silent no-op — the
// App then behaves exactly as if EnableMock had never been called, which means
// Run goes looking for the platform's config files.
//
//	config, _ := mock.LoadConfig("./mock-config.json")
//	app.EnableMock(config)
func (app *App) EnableMock(config *mock.MockConfig) *App {
	app.mockConfig = config
	return app
}

// Run initializes the SDK, starts one handler goroutine per configured node,
// and blocks until the process receives SIGTERM, SIGINT or SIGQUIT and every
// handler has exited. There is no matching or filtering: every node in the
// configuration gets its own Handle call on the same handler value.
//
// Each handler is supervised. One that panics, or that returns while its
// node's context is still alive, counts as a crash: the SDK reports a
// CodeProcessError and calls Handle again after a backoff starting at 1s and
// doubling up to 30s (reset once a run lasts longer than 30s). Only a handler
// returning after its context is cancelled — by shutdown or by NodeEnv.Stop —
// is left stopped.
//
// It returns an error if SDK initialization or the transport connection fails,
// and nil after a clean shutdown.
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
