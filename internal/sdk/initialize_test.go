package sdk

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/messenger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock"
)

// useMountPath points initialize at dir for the test's duration. mountPath is a
// package global, so tests calling this must not use t.Parallel().
func useMountPath(t *testing.T, dir string) {
	t.Helper()
	original := mountPath
	mountPath = dir
	t.Cleanup(func() { mountPath = original })
}

// writeMount lays out <root>/config/{config.json,messenger.json}; a nil content skips that file.
func writeMount(t *testing.T, configJSON, messengerJSON *string) string {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range map[string]*string{"config.json": configJSON, "messenger.json": messengerJSON} {
		if content == nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(*content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func ptr(s string) *string { return &s }

// messengerSettings reads the broker address and credentials the real client will dial with.
// Client.options is unexported in another package, so reflection is the only way to observe
// them without opening a connection (connect() builds tcp://Broker:Port from exactly these).
func messengerSettings(t *testing.T, s *sdk) (addr, username, password string) {
	t.Helper()
	client, ok := s.messenger.(*messenger.Client)
	if !ok {
		t.Fatalf("expected *messenger.Client, got %T", s.messenger)
	}
	opts := reflect.ValueOf(client).Elem().FieldByName("options")
	if !opts.IsValid() {
		t.Fatal("messenger.Client has no options field; update messengerSettings")
	}
	cfg := opts.FieldByName("Config").Elem()
	addr = fmt.Sprintf("%s:%d", opts.FieldByName("Broker").String(), opts.FieldByName("Port").Int())
	return addr, cfg.FieldByName("Username").String(), cfg.FieldByName("Password").String()
}

func newInitializedSDK(t *testing.T) (*sdk, error) {
	t.Helper()
	s := NewSDK()
	s.DisableLog()
	err := s.initialize()
	if s.cancel != nil {
		t.Cleanup(s.cancel)
	}
	return s, err
}

func TestInitializeResolvesMessengerAddressFromMountedConfig(t *testing.T) {
	tests := []struct {
		name          string
		messengerJSON string
		wantAddr      string
	}{
		{
			name:          "host and port given",
			messengerJSON: `{"username":"u","password":"p","host":"127.0.0.1","port":55002}`,
			wantAddr:      "127.0.0.1:55002",
		},
		{
			name:          "host and port absent",
			messengerJSON: `{"username":"u","password":"p"}`,
			wantAddr:      "neoedgex-messenger:1883",
		},
		{
			name:          "host given, port zero",
			messengerJSON: `{"username":"u","password":"p","host":"127.0.0.1","port":0}`,
			wantAddr:      "127.0.0.1:1883",
		},
		{
			name:          "host empty, port given",
			messengerJSON: `{"username":"u","password":"p","host":"","port":55002}`,
			wantAddr:      "neoedgex-messenger:55002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useMountPath(t, writeMount(t, ptr(`[]`), ptr(tt.messengerJSON)))

			s, err := newInitializedSDK(t)
			if err != nil {
				t.Fatalf("unexpected initialize error: %v", err)
			}
			addr, username, password := messengerSettings(t, s)
			if addr != tt.wantAddr {
				t.Fatalf("broker address = %q, want %q", addr, tt.wantAddr)
			}
			if username != "u" || password != "p" {
				t.Fatalf("credentials = %q/%q, want u/p", username, password)
			}
		})
	}
}

func TestInitializeParsesNodeConfigsAlongsideMessengerConfig(t *testing.T) {
	useMountPath(t, writeMount(t,
		ptr(`[{"id":"n1","type":"demo"}]`),
		ptr(`{"username":"app-user","password":"s3cret","host":"127.0.0.1","port":55002}`)))

	s, err := newInitializedSDK(t)
	if err != nil {
		t.Fatalf("unexpected initialize error: %v", err)
	}
	if len(s.NodeConfigs()) != 1 || s.NodeConfigs()[0].ID != "n1" {
		t.Fatalf("unexpected node configs: %+v", s.NodeConfigs())
	}
	if _, username, password := messengerSettings(t, s); username != "app-user" || password != "s3cret" {
		t.Fatalf("credentials = %q/%q, want app-user/s3cret", username, password)
	}
}

func TestInitializeFailsOnUnreadableMount(t *testing.T) {
	tests := []struct {
		name          string
		configJSON    *string
		messengerJSON *string
		wantErr       string
	}{
		{name: "config.json missing", configJSON: nil, messengerJSON: ptr(`{}`), wantErr: "failed to read config file"},
		{name: "config.json invalid", configJSON: ptr(`{not json`), messengerJSON: ptr(`{}`), wantErr: "failed to unmarshal config file"},
		{name: "messenger.json missing", configJSON: ptr(`[]`), messengerJSON: nil, wantErr: "failed to read messenger config file"},
		{name: "messenger.json invalid", configJSON: ptr(`[]`), messengerJSON: ptr(`{"host":`), wantErr: "failed to unmarshal messenger config file"},
		{name: "messenger.json port not a number", configJSON: ptr(`[]`), messengerJSON: ptr(`{"port":"55002"}`), wantErr: "failed to unmarshal messenger config file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useMountPath(t, writeMount(t, tt.configJSON, tt.messengerJSON))

			s, err := newInitializedSDK(t)
			if err == nil {
				t.Fatal("expected initialize to return an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if s.messenger != nil {
				t.Fatalf("expected no messenger after a failed initialize, got %T", s.messenger)
			}
		})
	}
}

func TestInitializeInMockModeSkipsMountedFiles(t *testing.T) {
	// An empty mount: any file read would fail, so success proves mock mode reads nothing.
	useMountPath(t, t.TempDir())

	s := NewSDK()
	s.DisableLog()
	s.EnableMock(&mock.MockConfig{Nodes: []contract.Node{{ID: "n1", Type: "demo"}}})
	err := s.initialize()
	if s.cancel != nil {
		t.Cleanup(s.cancel)
	}
	if err != nil {
		t.Fatalf("unexpected initialize error: %v", err)
	}

	if _, ok := s.messenger.(*mockMessenger); !ok {
		t.Fatalf("expected the mock messenger to stay in place, got %T", s.messenger)
	}
	if len(s.NodeConfigs()) != 1 || s.NodeConfigs()[0].ID != "n1" {
		t.Fatalf("mock node configs were replaced: %+v", s.NodeConfigs())
	}
}
