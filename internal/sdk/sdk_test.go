package sdk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/core"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/node"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock"
)

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

type fakeMessenger struct {
	connectErr       error
	connectCalled    bool
	disconnectCalled bool
}

func (m *fakeMessenger) Connect() error {
	m.connectCalled = true
	return m.connectErr
}

func (m *fakeMessenger) Disconnect() {
	m.disconnectCalled = true
}

func (m *fakeMessenger) AddSubscriber(string) <-chan core.RawMessengerPayload {
	return nil
}

func (m *fakeMessenger) RemoveSubscriber(string) {}

func (m *fakeMessenger) Publish(string, byte, []byte) error {
	return nil
}

func TestRunReturnsErrorWhenMessengerConnectFails(t *testing.T) {
	m := &fakeMessenger{connectErr: errors.New("connect failed")}
	s := &sdk{
		messenger: m,
		ctx:       context.Background(),
		logger:    noopLogger{},
	}

	err := s.Run(nil)
	if err == nil {
		t.Fatal("expected Run to return an error")
	}
	if !strings.Contains(err.Error(), "failed to connect messenger") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.connectCalled {
		t.Fatal("expected Connect to be called")
	}
	if m.disconnectCalled {
		t.Fatal("did not expect Disconnect to be called after failed Connect")
	}
}

func TestRunDisconnectsOnContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &fakeMessenger{}
	s := &sdk{
		messenger: m,
		ctx:       ctx,
		logger:    noopLogger{},
	}

	if err := s.Run(nil); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	if !m.connectCalled {
		t.Fatal("expected Connect to be called")
	}
	if !m.disconnectCalled {
		t.Fatal("expected Disconnect to be called")
	}
}

type recordingLogger struct {
	infos []string
}

func (recordingLogger) Debug(string, ...any) {}
func (l *recordingLogger) Info(format string, args ...any) {
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}
func (recordingLogger) Warn(string, ...any)  {}
func (recordingLogger) Error(string, ...any) {}

func TestMockMessengerPublishLogsOutboundPayload(t *testing.T) {
	logger := &recordingLogger{}
	m := newMockMessenger(logger)

	if err := m.Publish("topic/x", 0, []byte(`{"k":1}`)); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}
	if err := m.Publish("topic/y", 2, nil); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	foundX, foundY := false, false
	for _, line := range logger.infos {
		if strings.Contains(line, "[MOCK PUBLISH]") && strings.Contains(line, "topic/x") {
			foundX = true
		}
		if strings.Contains(line, "[MOCK PUBLISH]") && strings.Contains(line, "topic/y") {
			foundY = true
		}
	}
	if !foundX || !foundY {
		t.Fatalf("expected both publish calls to be logged, got: %v", logger.infos)
	}
}

// TestMockMessengerPublishLogsCBORPayloadHumanReadable pins that a CBOR data
// message is decoded for the mock log output (human-readable), while the
// error-topic JSON branch keeps working (both wire formats are attempted).
func TestMockMessengerPublishLogsCBORPayloadHumanReadable(t *testing.T) {
	logger := &recordingLogger{}
	m := newMockMessenger(logger)

	payload, err := cbor.Marshal(map[string]any{"temperature": 25.5})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if err := m.Publish("neoedgex/neoflow/out/n1/output1", 2, payload); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	found := false
	for _, line := range logger.infos {
		if strings.Contains(line, "[MOCK PUBLISH]") && strings.Contains(line, "temperature") && strings.Contains(line, "25.5") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected CBOR payload to be logged decoded, got: %v", logger.infos)
	}
}

// utcMillisecondTimestamp is the shape contract.PublishTimestampLayout renders a
// UTC time in: RFC3339 with exactly three fractional digits and a "Z" zone. The
// node package pins publish output against the same pattern; it is restated
// rather than shared because declarations in a test file are not importable and
// the alternative would be exporting a test-only symbol from the SDK.
var utcMillisecondTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// TestInjectNeoFlowMessageBuildsCBOREnvelopeWithNativeValues pins the mock
// injection seam (deviation (g)): the mock config keeps PortFieldData JSON
// form, but the injected wire message is a CBOR envelope whose data map holds
// NATIVE values (double -> float64, raw -> []byte, undefined -> null) and whose
// timestamp is stamped the same way a real publish stamps it.
func TestInjectNeoFlowMessageBuildsCBOREnvelopeWithNativeValues(t *testing.T) {
	// Mock injection owes the handler the publish-side timestamp contract, so
	// the local zone is moved off UTC for the test: an unforced stamp then reads
	// "+08:00" and only the production .UTC() conversion can satisfy the "Z"
	// asserted below. On a host that is already UTC — a CI container with TZ
	// unset — the assertion would hold either way and pin nothing.
	// t.Setenv("TZ", ...) cannot do this: time.Local is resolved once per
	// process and cached, while time.Now() reads time.Local on every call. The
	// write is global and therefore sound only while this package's tests stay
	// sequential; no test here calls t.Parallel().
	originalLocal := time.Local
	time.Local = time.FixedZone("CST", 8*3600)
	defer func() { time.Local = originalLocal }()
	if unforced := time.Now().Format(contract.PublishTimestampLayout); !strings.HasSuffix(unforced, "+08:00") {
		t.Fatalf("local zone override did not take effect: an unforced stamp reads %q, want a +08:00 offset", unforced)
	}

	m := newMockMessenger(noopLogger{})
	ch := m.AddSubscriber("n1")

	err := m.injectNeoFlowMessage("n1", "input1", map[string]contract.PortFieldData{
		"temperature": {Type: contract.TypeDouble, Value: "25.5"},
		"count":       {Type: contract.TypeInt64, Value: "9223372036854775807"},
		"blob":        {Type: contract.TypeRaw, Value: "AAECAP8="}, // base64 in config only
		"empty":       *contract.NewEmptyField(),
	})
	if err != nil {
		t.Fatalf("unexpected inject error: %v", err)
	}

	var payload core.RawMessengerPayload
	select {
	case payload = <-ch:
	default:
		t.Fatal("expected an injected payload on the subscriber channel")
	}
	if payload.Handle != "input1" {
		t.Fatalf("unexpected handle: %q", payload.Handle)
	}

	var env contract.NeoFlowMessage
	if err := cbor.Unmarshal(payload.Data, &env); err != nil {
		t.Fatalf("injected payload is not a CBOR envelope: %v", err)
	}
	if env.SourceNodeID != "mock" {
		t.Fatalf("unexpected source: %q", env.SourceNodeID)
	}

	// An injected message used to go out with an empty timestamp, which made
	// mock traffic the one case where a handler reading msg.Timestamp saw
	// something it can never see in production.
	if env.Timestamp == "" {
		t.Fatal("injected envelope carries no timestamp")
	}
	if !utcMillisecondTimestamp.MatchString(env.Timestamp) {
		t.Fatalf("injected timestamp %q is not UTC RFC3339 with a three-digit fraction, the shape publish output has", env.Timestamp)
	}

	var fields map[string]cbor.RawMessage
	if err := cbor.Unmarshal(env.Data, &fields); err != nil {
		t.Fatalf("data segment is not a CBOR map: %v", err)
	}

	var temperature float64
	if err := cbor.Unmarshal(fields["temperature"], &temperature); err != nil || temperature != 25.5 {
		t.Fatalf("temperature not a native double: %v %v", err, temperature)
	}
	var count int64
	if err := cbor.Unmarshal(fields["count"], &count); err != nil || count != 9223372036854775807 {
		t.Fatalf("count corrupted: %v %d", err, count)
	}
	var blob []byte
	if err := cbor.Unmarshal(fields["blob"], &blob); err != nil || len(blob) != 5 || blob[4] != 0xff {
		t.Fatalf("blob not a native byte string: %v % x", err, blob)
	}
	// the raw field must be a CBOR byte string on the wire, not base64 text
	if fields["blob"][0]&0xe0 != 0x40 {
		t.Fatalf("blob wire encoding is not a byte string: 0x%02x", fields["blob"][0])
	}
	if len(fields["empty"]) != 1 || fields["empty"][0] != 0xf6 {
		t.Fatalf("undefined mock field must inject as CBOR null, got % x", []byte(fields["empty"]))
	}
}

// TestInjectNeoFlowMessageRejectsUnknownSubscriber pins the error path.
func TestInjectNeoFlowMessageRejectsUnknownSubscriber(t *testing.T) {
	m := newMockMessenger(noopLogger{})
	if err := m.injectNeoFlowMessage("ghost", "input1", nil); err == nil {
		t.Fatal("expected error for missing subscriber")
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written. The loggers bind os.Stderr at construction time, so fn must
// build them, not just use them.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer

	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		captured <- buf.String()
	}()

	fn()

	os.Stderr = original
	_ = writer.Close()
	out := <-captured
	_ = reader.Close()
	return out
}

// TestDisableLogSilencesSDKOutputOnlyDrives the whole DisableSDKLog chain on
// real stderr: App.DisableSDKLog -> sdk.DisableLog -> node instance. Only the
// SDK's own output goes quiet; the lines an app writes through
// NodeEnv.Logger() must still reach stderr, which is what the option's name
// has always promised.
func TestDisableLogSilencesSDKOutputOnly(t *testing.T) {
	t.Setenv("NEOEDGEX_LOG_LEVEL", "debug")

	nodeConfig := contract.Node{ID: "n1", Type: "demo", Data: contract.NodeData{Name: "demo-node"}}

	run := func(disable bool) string {
		return captureStderr(t, func() {
			s := NewSDK()
			if disable {
				s.DisableLog()
			}
			s.EnableMock(&mock.MockConfig{Nodes: []contract.Node{nodeConfig}})
			if err := s.Initialize(); err != nil {
				t.Errorf("unexpected initialize error: %v", err)
				return
			}
			instance, err := node.NewInstance(s, s.NodeConfigs()[0])
			if err != nil {
				t.Errorf("unexpected instance error: %v", err)
				return
			}
			s.NewLogger("SDK").Warn("sdk-machinery-line")
			instance.Logger().Info("app-written-line")
		})
	}

	silenced := run(true)
	if strings.Contains(silenced, "sdk-machinery-line") {
		t.Fatalf("DisableLog did not silence SDK output:\n%s", silenced)
	}
	if strings.Contains(silenced, "Initializing node instance") {
		t.Fatalf("DisableLog did not silence the node instance's internal log:\n%s", silenced)
	}
	if !strings.Contains(silenced, "app-written-line") {
		t.Fatalf("DisableLog swallowed the app's own log line:\n%s", silenced)
	}
	if !strings.Contains(silenced, "[Node-demo-node]") {
		t.Fatalf("the app's log line lost its node tag:\n%s", silenced)
	}

	// Negative control: without DisableLog both kinds of line show up, so the
	// assertions above cannot pass merely because nothing was logged at all.
	audible := run(false)
	for _, want := range []string{"sdk-machinery-line", "app-written-line", "Initializing node instance"} {
		if !strings.Contains(audible, want) {
			t.Fatalf("expected %q in default (non-silenced) output:\n%s", want, audible)
		}
	}
}
