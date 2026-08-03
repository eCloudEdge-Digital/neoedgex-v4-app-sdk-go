package node

// Replaces instance_json_outbound_test.go: outbound wire-shape assertions for
// the CBOR data message. Pins the C1 byte-level facts a consumer relies on:
// raw fields ride as native CBOR byte strings (no base64 text), integer
// extremes keep full width (no float64 corruption), undefined is CBOR null,
// and container values are value-domain rejects (D2).

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

func TestPublishRawFieldIsNativeByteStringOnWire(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {{Key: "blob", Type: contract.TypeRaw}},
		},
	})

	blob := []byte{0x00, 0xff, 0xfe, 0x80, 0x01} // not valid UTF-8, not base64-able as-is
	if err := instance.Publish("output1", map[string]any{"blob": blob}); err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	raw := fields["blob"]
	if len(raw) == 0 {
		t.Fatal("blob field missing from wire")
	}
	// CBOR major type 2 (byte string) — NOT major type 3 (text string / base64).
	if raw[0]&0xe0 != 0x40 {
		t.Fatalf("blob field is not a CBOR byte string: first byte 0x%02x", raw[0])
	}

	var decoded []byte
	if err := cbor.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("cannot decode blob as []byte: %v", err)
	}
	if !bytes.Equal(decoded, blob) {
		t.Fatalf("blob not byte-identical: got % x, want % x", decoded, blob)
	}
}

func TestPublishIntegerExtremesKeepFullWidthOnWire(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "imax", Type: contract.TypeInt64},
				{Key: "imin", Type: contract.TypeInt64},
				{Key: "umax", Type: contract.TypeUint64},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{
		"imax": int64(math.MaxInt64),
		"imin": int64(math.MinInt64),
		"umax": uint64(math.MaxUint64),
	}); err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)

	var imax, imin int64
	var umax uint64
	if err := cbor.Unmarshal(fields["imax"], &imax); err != nil || imax != math.MaxInt64 {
		t.Fatalf("imax corrupted: %v %d", err, imax)
	}
	if err := cbor.Unmarshal(fields["imin"], &imin); err != nil || imin != math.MinInt64 {
		t.Fatalf("imin corrupted: %v %d", err, imin)
	}
	if err := cbor.Unmarshal(fields["umax"], &umax); err != nil || umax != math.MaxUint64 {
		t.Fatalf("umax corrupted: %v %d", err, umax)
	}

	// none of the extreme fields may be encoded as a CBOR float (major type 7)
	for _, key := range []string{"imax", "imin", "umax"} {
		if fields[key][0]&0xe0 == 0xe0 {
			t.Fatalf("field %q encoded as CBOR simple/float: first byte 0x%02x", key, fields[key][0])
		}
	}

	// belt & braces: a generic decode must not surface float64 corruption
	data := contract.RawMessage(mustEnvelopeData(t, messenger.last(t).data))
	generic := contract.NewMessage("", "", "", data, nil, nil).ToMap()
	for k, v := range generic {
		if _, isFloat := v.(float64); isFloat {
			t.Fatalf("field %q decoded as float64 (%v) — 9.22e+18-style corruption", k, v)
		}
	}
}

// mustEnvelopeData extracts the raw data segment from a published wire payload.
func mustEnvelopeData(t *testing.T, payload []byte) []byte {
	t.Helper()
	var message contract.NeoFlowMessage
	if err := cbor.Unmarshal(payload, &message); err != nil {
		t.Fatalf("payload is not a CBOR envelope: %v", err)
	}
	return message.Data
}

func TestPublishFloatFieldsKeepDeclaredWidth(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "f", Type: contract.TypeFloat},
				{Key: "d", Type: contract.TypeDouble},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{
		"f": float32(0.1),
		"d": float64(3.141592653589793),
	}); err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	var f float32
	if err := cbor.Unmarshal(fields["f"], &f); err != nil || f != float32(0.1) {
		t.Fatalf("float field corrupted: %v %v", err, f)
	}
	var d float64
	if err := cbor.Unmarshal(fields["d"], &d); err != nil || d != 3.141592653589793 {
		t.Fatalf("double field corrupted: %v %v", err, d)
	}
}

func TestPublishRejectsContainerValuesAsValueDomainErrors(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "m", Type: contract.TypeString},
				{Key: "s", Type: contract.TypeString},
			},
		},
	})

	// D2: no container types — map/slice values are out of the value domain.
	if err := instance.Publish("output1", map[string]any{
		"m": map[string]any{"k": 1},
		"s": []any{1, 2},
	}); err != nil {
		t.Fatalf("expected Publish to continue (fields nulled), got: %v", err)
	}

	errorReports := 0
	for _, topic := range messenger.topics() {
		if strings.HasPrefix(topic, "neoedgex/neoflow/error/") {
			errorReports++
		}
	}
	if errorReports != 2 {
		t.Fatalf("expected one error report per rejected container field, got %d", errorReports)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	for _, key := range []string{"m", "s"} {
		if len(fields[key]) != 1 || fields[key][0] != 0xf6 {
			t.Fatalf("expected CBOR null for rejected field %q, got % x", key, []byte(fields[key]))
		}
	}
}

// TestPublishRejectsFloatOverflow pins that a finite double beyond float32
// range into a float tag is rejected by the matrix (never narrowed to Inf):
// the field goes out as null with a per-field error report.
func TestPublishRejectsFloatOverflow(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "big", Type: contract.TypeFloat},
				{Key: "ok", Type: contract.TypeFloat},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{
		"big": float64(1e300),
		"ok":  float64(1.5),
	}); err != nil {
		t.Fatalf("expected Publish to continue after overflow reject, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	if len(fields["big"]) != 1 || fields["big"][0] != 0xf6 {
		t.Fatalf("expected CBOR null for %q, got % x", "big", []byte(fields["big"]))
	}
	var ok float32
	if err := cbor.Unmarshal(fields["ok"], &ok); err != nil || ok != 1.5 {
		t.Fatalf("healthy field affected: %v %v", err, ok)
	}

	errorReports := 0
	for _, topic := range messenger.topics() {
		if strings.HasPrefix(topic, "neoedgex/neoflow/error/") {
			errorReports++
		}
	}
	if errorReports != 1 {
		t.Fatalf("expected 1 overflow error report, got %d", errorReports)
	}
}

func TestPublishRejectsNaNAndInf(t *testing.T) {
	instance, messenger := newTestInstance(t, contract.NodeData{
		Name: "demo-node",
		Outputs: map[string][]contract.PortFieldSchema{
			"output1": {
				{Key: "nan", Type: contract.TypeDouble},
				{Key: "inf", Type: contract.TypeDouble},
				{Key: "ok", Type: contract.TypeDouble},
			},
		},
	})

	if err := instance.Publish("output1", map[string]any{
		"nan": math.NaN(),
		"inf": math.Inf(1),
		"ok":  float64(1.5),
	}); err != nil {
		t.Fatalf("expected Publish to continue after NaN/Inf rejects, got: %v", err)
	}

	_, fields := decodeWire(t, messenger.last(t).data)
	for _, key := range []string{"nan", "inf"} {
		if len(fields[key]) != 1 || fields[key][0] != 0xf6 {
			t.Fatalf("expected CBOR null for %q, got % x", key, []byte(fields[key]))
		}
	}
	var ok float64
	if err := cbor.Unmarshal(fields["ok"], &ok); err != nil || ok != 1.5 {
		t.Fatalf("healthy field affected: %v %v", err, ok)
	}

	errorReports := 0
	for _, topic := range messenger.topics() {
		if strings.HasPrefix(topic, "neoedgex/neoflow/error/") {
			errorReports++
		}
	}
	if errorReports != 2 {
		t.Fatalf("expected 2 NaN/Inf error reports, got %d", errorReports)
	}
}
