package contract

// Tests for the CBOR envelope and the message accessors (D12/D13/D15):
// Message.ToMap / Message.ToStruct — schema-driven typed decode, the shared
// conversion matrix on schema mismatch, undefined (nil) semantics, and
// unknown-tag bypass with natural-domain normalization (also exercised with a
// nil plan, the schema-less case).
// Precision guard: int64/uint64 extremes must never surface as float64.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

type capturingLogger struct {
	debugs []string
	warns  []string
}

func (l *capturingLogger) Debug(format string, args ...any) {
	l.debugs = append(l.debugs, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Info(string, ...any) {}
func (l *capturingLogger) Warn(format string, args ...any) {
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}
func (l *capturingLogger) Error(string, ...any) {}

func mustCBOR(t *testing.T, v any) []byte {
	t.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		t.Fatalf("cbor.Marshal failed: %v", err)
	}
	return b
}

func newTestMessage(t *testing.T, data any, schema []PortFieldSchema, logger Logger) Message {
	t.Helper()
	return NewMessage("src-node", "2026-07-31T00:00:00Z", "input1", RawMessage(mustCBOR(t, data)), NewDecodePlan(schema), logger)
}

// mustBigIntCBOR hand-crafts wire bytes and guards the fixture: the rule under
// test only stays covered while the codec keeps decoding these bytes to a
// math/big.Int, which no encoder in the SDK can produce.
func mustBigIntCBOR(t *testing.T, hexBytes string) cbor.RawMessage {
	t.Helper()
	raw, err := hex.DecodeString(hexBytes)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", hexBytes, err)
	}
	var decoded any
	if err := decMode.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("fixture %q no longer decodes: %v", hexBytes, err)
	}
	if _, ok := decoded.(big.Int); !ok {
		t.Fatalf("fixture %q decodes to %T, want big.Int", hexBytes, decoded)
	}
	return raw
}

// assertJSONNull pins the user-visible defect behind the rule: a big.Int value
// boxed in map[string]any is not addressable, so json.Marshal skips its
// pointer-receiver marshaler and emits {} with a nil error.
func assertJSONNull(t *testing.T, data map[string]any, keys ...string) {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("cannot re-read marshaled JSON %s: %v", encoded, err)
	}
	for _, key := range keys {
		if got := string(fields[key]); got != "null" {
			t.Fatalf("key %q marshaled to %s, want null (full JSON: %s)", key, got, encoded)
		}
	}
}

// TestNeoFlowMessageEnvelopeWireShape pins the C1 envelope: a CBOR map with
// keys source/timestamp/data, where data stays raw (delayed decoding) and the
// field values inside data are native CBOR values, not per-field wrappers.
func TestNeoFlowMessageEnvelopeWireShape(t *testing.T) {
	dataBytes := mustCBOR(t, map[string]any{"temp": int16(21)})
	wire := mustCBOR(t, NeoFlowMessage{
		SourceNodeID: "node-up",
		Timestamp:    "2026-07-31T00:00:00Z",
		Data:         dataBytes,
	})

	// Decode generically: top level must expose exactly the 3 contract keys.
	var top map[string]cbor.RawMessage
	if err := cbor.Unmarshal(wire, &top); err != nil {
		t.Fatalf("envelope is not a CBOR map: %v", err)
	}
	for _, key := range []string{"source", "timestamp", "data"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("envelope missing key %q; keys=%v", key, keysOf(top))
		}
	}
	if len(top) != 3 {
		t.Fatalf("envelope has extra keys: %v", keysOf(top))
	}

	// Round-trip through the typed envelope: data must be byte-identical raw.
	var env NeoFlowMessage
	if err := cbor.Unmarshal(wire, &env); err != nil {
		t.Fatalf("unexpected envelope unmarshal error: %v", err)
	}
	if env.SourceNodeID != "node-up" || env.Timestamp != "2026-07-31T00:00:00Z" {
		t.Fatalf("unexpected envelope fields: %+v", env)
	}
	if !bytes.Equal(env.Data, dataBytes) {
		t.Fatalf("data segment not preserved byte-exact")
	}
}

func keysOf(m map[string]cbor.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestMessageToMapSchemaTypedAllScalars pins the schema-typed decode for the
// full 11-scalar universe: each field surfaces as the concrete Go type of its
// input schema tag type, byte- and value-exact.
func TestMessageToMapSchemaTypedAllScalars(t *testing.T) {
	rawBytes := []byte{0x00, 0xff, 0xfe, 0x80}
	msg := newTestMessage(t, map[string]any{
		"b":   true,
		"i16": int16(-12345),
		"i32": int32(-2000000000),
		"i64": int64(math.MinInt64),
		"u16": uint16(math.MaxUint16),
		"u32": uint32(math.MaxUint32),
		"u64": uint64(math.MaxUint64),
		"f":   float32(0.1),
		"d":   float64(3.141592653589793),
		"s":   "héllo 世界",
		"r":   rawBytes,
	}, []PortFieldSchema{
		{Key: "b", Type: TypeBool},
		{Key: "i16", Type: TypeInt16},
		{Key: "i32", Type: TypeInt32},
		{Key: "i64", Type: TypeInt64},
		{Key: "u16", Type: TypeUint16},
		{Key: "u32", Type: TypeUint32},
		{Key: "u64", Type: TypeUint64},
		{Key: "f", Type: TypeFloat},
		{Key: "d", Type: TypeDouble},
		{Key: "s", Type: TypeString},
		{Key: "r", Type: TypeRaw},
	}, nil)

	got := msg.ToMap()
	if got == nil {
		t.Fatal("ToMap returned nil for a valid data map")
	}

	want := map[string]any{
		"b":   true,
		"i16": int16(-12345),
		"i32": int32(-2000000000),
		"i64": int64(math.MinInt64),
		"u16": uint16(math.MaxUint16),
		"u32": uint32(math.MaxUint32),
		"u64": uint64(math.MaxUint64),
		"f":   float32(0.1),
		"d":   float64(3.141592653589793),
		"s":   "héllo 世界",
	}
	for k, w := range want {
		if fmt.Sprintf("%#v", got[k]) != fmt.Sprintf("%#v", w) {
			t.Fatalf("field %q: got %#v, want %#v", k, got[k], w)
		}
	}
	r, ok := got["r"].([]byte)
	if !ok || !bytes.Equal(r, rawBytes) {
		t.Fatalf("raw field: got %#v, want byte-identical []byte", got["r"])
	}
	if len(got) != 11 {
		t.Fatalf("unexpected extra keys in result: %d", len(got))
	}
}

// TestMessageToMapIntegerPrecisionGuard pins that int64/uint64 extremes are
// never widened through float64: no 9.22e+18 / 1.84e+19 corruption.
func TestMessageToMapIntegerPrecisionGuard(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"imax": int64(math.MaxInt64),
		"umax": uint64(math.MaxUint64),
	}, []PortFieldSchema{
		{Key: "imax", Type: TypeInt64},
		{Key: "umax", Type: TypeUint64},
	}, nil)

	got := msg.ToMap()
	imax, ok := got["imax"].(int64)
	if !ok || imax != math.MaxInt64 {
		t.Fatalf("imax: got %#v (%T), want int64(%d)", got["imax"], got["imax"], int64(math.MaxInt64))
	}
	umax, ok := got["umax"].(uint64)
	if !ok || umax != uint64(math.MaxUint64) {
		t.Fatalf("umax: got %#v (%T), want uint64(%d)", got["umax"], got["umax"], uint64(math.MaxUint64))
	}
	for k, v := range got {
		if _, isFloat := v.(float64); isFloat {
			t.Fatalf("field %q leaked into float64: %v", k, v)
		}
		if s := fmt.Sprintf("%v", v); strings.Contains(s, "e+") {
			t.Fatalf("field %q rendered in float notation: %s", k, s)
		}
	}
}

// TestMessageToMapUndefined pins the three undefined sources (D15): CBOR null,
// CBOR undefined (0xf7), and a schema-declared key absent from the wire — all
// surface as nil with the key PRESENT in the result map.
func TestMessageToMapUndefined(t *testing.T) {
	data := map[string]cbor.RawMessage{
		"null":  {0xf6},
		"undef": {0xf7},
		"ok":    cbor.RawMessage(mustCBOR(t, int64(1))),
	}
	msg := newTestMessage(t, data, []PortFieldSchema{
		{Key: "null", Type: TypeInt64},
		{Key: "undef", Type: TypeString},
		{Key: "absent", Type: TypeDouble},
		{Key: "ok", Type: TypeInt64},
	}, nil)

	got := msg.ToMap()
	for _, k := range []string{"null", "undef", "absent"} {
		v, exists := got[k]
		if !exists {
			t.Fatalf("undefined field %q must be present with nil value", k)
		}
		if v != nil {
			t.Fatalf("undefined field %q: got %#v, want nil", k, v)
		}
	}
	if v, ok := got["ok"].(int64); !ok || v != 1 {
		t.Fatalf("ok field: got %#v", got["ok"])
	}
}

// TestMessageToMapCrossTypeMatrix pins the D13 mismatch rule: wire kind !=
// schema type routes through the SAME conversion matrix as Publish; a denied
// or failed conversion delivers undefined (nil), never an error or a wrong type.
func TestMessageToMapCrossTypeMatrix(t *testing.T) {
	logger := &capturingLogger{}
	msg := newTestMessage(t, map[string]any{
		"trunc":    float64(2.9),   // double wire -> int16 schema: truncate
		"coerce":   "42",           // string wire -> int32 schema: parse
		"broken":   int64(70000),   // out of int16 range -> undefined
		"toStr":    int64(7),       // int wire -> string schema: stringified
		"boolNum":  true,           // bool wire -> int64 schema: 1
		"badParse": "not-a-number", // string wire -> double schema: undefined
		"strRaw":   "hi",           // string wire -> raw schema: denied -> undefined
		"negU":     int64(-1),      // negative wire -> uint16 schema: undefined
	}, []PortFieldSchema{
		{Key: "trunc", Type: TypeInt16},
		{Key: "coerce", Type: TypeInt32},
		{Key: "broken", Type: TypeInt16},
		{Key: "toStr", Type: TypeString},
		{Key: "boolNum", Type: TypeInt64},
		{Key: "badParse", Type: TypeDouble},
		{Key: "strRaw", Type: TypeRaw},
		{Key: "negU", Type: TypeUint16},
	}, logger)

	got := msg.ToMap()
	want := map[string]any{
		"trunc":    int16(2),
		"coerce":   int32(42),
		"broken":   nil,
		"toStr":    "7",
		"boolNum":  int64(1),
		"badParse": nil,
		"strRaw":   nil,
		"negU":     nil,
	}
	for k, w := range want {
		if fmt.Sprintf("%#v", got[k]) != fmt.Sprintf("%#v", w) {
			t.Fatalf("field %q: got %#v, want %#v", k, got[k], w)
		}
	}
	// Failed conversions must be surfaced via the logger (warn), not silently.
	if len(logger.warns) == 0 {
		t.Fatal("expected warn logs for conversion failures")
	}
}

// TestMessageToMapFloatWidthMatrix pins the D13 float-width rule: a wire
// float whose precision differs from the schema width routes through the
// SAME conversion matrix as Publish. Single -> double restores the
// shortest-decimal value (25.34, not the codec-widened 25.34000015258789);
// double -> float mirrors the matrix narrowing exactly. Same-width wire
// values keep the direct typed decode.
func TestMessageToMapFloatWidthMatrix(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"wide":   float32(25.34), // 0xfa wire, double schema -> matrix widening
		"narrow": float64(25.34), // 0xfb wire, float schema -> matrix narrowing
		"big":    float64(1e300), // 0xfb wire, float schema, beyond float32 range
		"f":      float32(25.34), // same width -> identity
		"d":      float64(25.34), // same width -> identity
	}, []PortFieldSchema{
		{Key: "wide", Type: TypeDouble},
		{Key: "narrow", Type: TypeFloat},
		{Key: "big", Type: TypeFloat},
		{Key: "f", Type: TypeFloat},
		{Key: "d", Type: TypeDouble},
	}, nil)

	got := msg.ToMap()
	if v, ok := got["wide"].(float64); !ok || v != 25.34 {
		t.Fatalf("wide: got %#v (%T), want float64(25.34) exactly", got["wide"], got["wide"])
	}
	if b, err := json.Marshal(got["wide"]); err != nil || string(b) != "25.34" {
		t.Fatalf("wide JSON literal: got %q (err=%v), want \"25.34\"", b, err)
	}
	if v, ok := got["narrow"].(float32); !ok || v != float32(25.34) {
		t.Fatalf("narrow: got %#v (%T), want float32(25.34)", got["narrow"], got["narrow"])
	}
	// Beyond-float32-range narrowing overflows to Inf, which the matrix
	// rejects — the field is delivered as undefined (nil), same on both
	// decode and Publish sides.
	if _, err := ConvertToTypedValue(float64(1e300), TypeFloat); err == nil {
		t.Fatalf("matrix accepted 1e300->float, want out-of-range error")
	}
	if v, exists := got["big"]; !exists || v != nil {
		t.Fatalf("big: exists=%v value=%#v (%T), want present nil", exists, v, v)
	}
	if v, ok := got["f"].(float32); !ok || v != float32(25.34) {
		t.Fatalf("f identity: got %#v (%T), want float32(25.34)", got["f"], got["f"])
	}
	if v, ok := got["d"].(float64); !ok || v != 25.34 {
		t.Fatalf("d identity: got %#v (%T), want float64(25.34)", got["d"], got["d"])
	}
}

// TestMessageToMapUnknownTagBypass pins the bypass rule: keys not in the
// input schema are delivered with natural-domain normalization (positive
// integers <= MaxInt64 become int64, larger stay uint64) plus a debug log.
func TestMessageToMapUnknownTagBypass(t *testing.T) {
	logger := &capturingLogger{}
	msg := newTestMessage(t, map[string]any{
		"known":     int64(1),
		"ghost":     uint64(5),              // <= MaxInt64 -> int64
		"ghostBig":  uint64(math.MaxUint64), // > MaxInt64 -> stays uint64
		"ghostStr":  "s",
		"ghostNull": nil, // undefined even without schema
	}, []PortFieldSchema{
		{Key: "known", Type: TypeInt64},
	}, logger)

	got := msg.ToMap()
	if v, ok := got["ghost"].(int64); !ok || v != 5 {
		t.Fatalf("ghost: got %#v (%T), want int64(5)", got["ghost"], got["ghost"])
	}
	if v, ok := got["ghostBig"].(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("ghostBig: got %#v (%T), want uint64 max", got["ghostBig"], got["ghostBig"])
	}
	if got["ghostStr"] != "s" {
		t.Fatalf("ghostStr: got %#v", got["ghostStr"])
	}
	if v, exists := got["ghostNull"]; !exists || v != nil {
		t.Fatalf("ghostNull: exists=%v value=%#v, want present nil", exists, v)
	}
	found := false
	for _, line := range logger.debugs {
		if strings.Contains(line, "ghost") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a debug log about bypassed tags, got %v", logger.debugs)
	}
}

// TestMessageUndecodableUnknownTagIsUndefined pins the unified undefined rule
// on a tag that is NOT declared in the input schema and whose value the
// natural decode rejects: ToMap keeps the key with a nil value (it is never
// dropped) and an `any` field for it stays nil in ToStruct — the same result
// as every other "no value" case. The fixture is a nested CBOR map with
// non-string keys: accepted by the outer data-map decode, rejected by the
// value decode.
func TestMessageUndecodableUnknownTagIsUndefined(t *testing.T) {
	logger := &capturingLogger{}
	msg := newTestMessage(t, map[string]any{
		"ghost": map[int64]string{1: "a"},
		"known": int64(1),
	}, []PortFieldSchema{{Key: "known", Type: TypeInt64}}, logger)

	// Fixture guard: if this value ever starts decoding cleanly the test would
	// silently stop covering the rule.
	fields, err := msg.dataFields()
	if err != nil {
		t.Fatalf("outer data map must still decode: %v", err)
	}
	if _, err := decodeNatural(fields["ghost"]); err == nil {
		t.Fatal("fixture no longer triggers a natural-decode failure")
	}

	got := msg.ToMap()
	v, exists := got["ghost"]
	if !exists {
		t.Fatal("undecodable unknown tag must stay in the map as nil, not be dropped")
	}
	if v != nil {
		t.Fatalf("ghost: got %#v, want nil", v)
	}
	if len(logger.warns) != 1 || !strings.Contains(logger.warns[0], `"ghost"`) {
		t.Fatalf("expected exactly one warn naming the tag, got %v", logger.warns)
	}

	type input struct {
		Ghost any   `cbor:"ghost"`
		Known int64 `cbor:"known"`
	}
	var in input
	if err := msg.ToStruct(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Ghost != nil {
		t.Fatalf("Ghost: got %#v, want nil", in.Ghost)
	}
	if in.Known != 1 {
		t.Fatalf("Known: got %d, want 1", in.Known)
	}
}

// TestMessageToMapOutOfUniverseValueIsUndefined pins the closed natural
// domain on the bypass path: an integer past math.MinInt64 and a CBOR bignum
// both decode to math/big.Int, which is outside the supported type universe,
// so each is delivered as undefined (key present, value nil) with one warn.
// Without the rule they pass through and json.Marshal renders them as {}.
func TestMessageToMapOutOfUniverseValueIsUndefined(t *testing.T) {
	logger := &capturingLogger{}
	msg := newTestMessage(t, map[string]cbor.RawMessage{
		"belowMinInt64": mustBigIntCBOR(t, "3bffffffffffffffff"),     // -18446744073709551616
		"bignumNeg":     mustBigIntCBOR(t, "c349010000000000000000"), // tag 3: -(2^64 + 1)
		"bignumPos":     mustBigIntCBOR(t, "c249010000000000000000"), // tag 2: 2^64
		"known":         cbor.RawMessage(mustCBOR(t, int64(1))),
	}, []PortFieldSchema{{Key: "known", Type: TypeInt64}}, logger)

	got := msg.ToMap()
	warns := strings.Join(logger.warns, "\n")
	for _, key := range []string{"belowMinInt64", "bignumNeg", "bignumPos"} {
		v, exists := got[key]
		if !exists {
			t.Fatalf("%q must stay in the map as nil, not be dropped", key)
		}
		if v != nil {
			t.Fatalf("%q: got %#v (%T), want nil", key, v, v)
		}
		if n := strings.Count(warns, `"`+key+`"`); n != 1 {
			t.Fatalf("expected exactly one warn naming %q, got %d: %v", key, n, logger.warns)
		}
	}
	if len(logger.warns) != 3 {
		t.Fatalf("expected one warn per rejected tag, got %v", logger.warns)
	}
	if v, ok := got["known"].(int64); !ok || v != 1 {
		t.Fatalf("known: got %#v (%T), want int64(1)", got["known"], got["known"])
	}
	assertJSONNull(t, got, "belowMinInt64", "bignumNeg", "bignumPos")
}

// TestMessageToMapContainerIsUndefined pins the D2 consequence on the receive
// side: no tag type can declare a container and Publish refuses container
// values, so a CBOR array or map on an undeclared tag is out of the delivered
// universe. It arrives as ONE undefined (nil) with exactly one warn — never
// partially normalized element by element, which used to drop bad elements
// with no log at all.
func TestMessageToMapContainerIsUndefined(t *testing.T) {
	logger := &capturingLogger{}
	msg := newTestMessage(t, map[string]any{
		"list":   []any{int64(1), "two"},
		"nested": map[string]any{"k": int64(1)},
		"known":  int64(1),
	}, []PortFieldSchema{{Key: "known", Type: TypeInt64}}, logger)

	got := msg.ToMap()
	warns := strings.Join(logger.warns, "\n")
	for _, key := range []string{"list", "nested"} {
		v, exists := got[key]
		if !exists {
			t.Fatalf("%q must stay in the map as nil, not be dropped", key)
		}
		if v != nil {
			t.Fatalf("%q: got %#v (%T), want nil", key, v, v)
		}
		if n := strings.Count(warns, `"`+key+`"`); n != 1 {
			t.Fatalf("expected exactly one warn naming %q, got %d: %v", key, n, logger.warns)
		}
	}
	if len(logger.warns) != 2 {
		t.Fatalf("expected one warn per container tag, got %v", logger.warns)
	}
	if v, ok := got["known"].(int64); !ok || v != 1 {
		t.Fatalf("known: got %#v (%T), want int64(1)", got["known"], got["known"])
	}

	// Same rule at the struct boundary: an `any` field for a container tag
	// stays nil instead of receiving the container.
	type input struct {
		List   any `cbor:"list"`
		Nested any `cbor:"nested"`
	}
	var in input
	if err := NewMessage("src", "ts", "input1", msg.Data, nil, nil).ToStruct(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.List != nil || in.Nested != nil {
		t.Fatalf("container fields must stay nil: list=%#v nested=%#v", in.List, in.Nested)
	}
}

// TestMessageToMapPathologicalDataReturnsNil pins the O(1)-gate backstop: a
// data segment that is not a CBOR string-keyed map yields nil plus a warn log
// (no panic, no error return — user ruling in D12).
func TestMessageToMapPathologicalDataReturnsNil(t *testing.T) {
	logger := &capturingLogger{}
	msg := NewMessage("s", "t", "h", RawMessage(mustCBOR(t, []any{1, 2})), nil, logger)
	if got := msg.ToMap(); got != nil {
		t.Fatalf("expected nil for non-map data, got %#v", got)
	}
	if len(logger.warns) != 1 {
		t.Fatalf("expected exactly one warn log for pathological data, got %d: %v", len(logger.warns), logger.warns)
	}
	if !strings.Contains(logger.warns[0], `source="s"`) || !strings.Contains(logger.warns[0], `handle="h"`) {
		t.Fatalf("warn should carry source and handle for triage, got: %s", logger.warns[0])
	}

	nilLoggerMsg := NewMessage("s", "t", "h", RawMessage(mustCBOR(t, []any{1, 2})), nil, nil)
	if got := nilLoggerMsg.ToMap(); got != nil {
		t.Fatalf("expected nil for non-map data with nil logger, got %#v", got)
	}
}

// TestMessageToStructPathologicalDataWarnsOnce pins that ToStruct's degraded
// path leaves the same single SDK-layer warn trace as ToMap (shared choke
// point) in addition to returning the error.
func TestMessageToStructPathologicalDataWarnsOnce(t *testing.T) {
	logger := &capturingLogger{}
	msg := NewMessage("s", "t", "h", RawMessage(mustCBOR(t, []any{1, 2})), nil, logger)
	var target struct{}
	if err := msg.ToStruct(&target); err == nil {
		t.Fatal("expected error for non-map data")
	}
	if len(logger.warns) != 1 {
		t.Fatalf("expected exactly one warn log, got %d: %v", len(logger.warns), logger.warns)
	}
}

// TestMessageToStructDeclaredPriorityAndAny pins the D13 ToStruct contract:
// concrete declarations beat the schema; `any` fields receive the
// schema-typed value (or natural-domain when the key is unknown to the schema).
func TestMessageToStructDeclaredPriorityAndAny(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"temp":  int16(1234),
		"ratio": float64(2.5),
		"name":  "sensor-A",
		"ghost": uint64(5),
	}, []PortFieldSchema{
		{Key: "temp", Type: TypeInt16},
		{Key: "ratio", Type: TypeDouble},
		{Key: "name", Type: TypeString},
	}, nil)

	type input struct {
		Temp  int32  `cbor:"temp"` // concrete, wider than schema -> declaration wins
		Ratio any    `cbor:"ratio"`
		Name  string `cbor:"name"`
		Ghost any    `cbor:"ghost"` // not in schema -> natural domain
	}
	var in input
	if err := msg.ToStruct(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Temp != int32(1234) {
		t.Fatalf("Temp: got %#v, want int32(1234) via declared type", in.Temp)
	}
	if v, ok := in.Ratio.(float64); !ok || v != 2.5 {
		t.Fatalf("Ratio: got %#v (%T), want schema-typed float64(2.5)", in.Ratio, in.Ratio)
	}
	if in.Name != "sensor-A" {
		t.Fatalf("Name: got %q", in.Name)
	}
	if v, ok := in.Ghost.(int64); !ok || v != 5 {
		t.Fatalf("Ghost: got %#v (%T), want natural-domain int64(5)", in.Ghost, in.Ghost)
	}
}

// agreementSchema declares one key per divergence shape the concrete-field
// path used to have, plus same-representation controls that must keep taking
// the direct decode.
var agreementSchema = []PortFieldSchema{
	{Key: "wideD", Type: TypeDouble},
	{Key: "narrowF", Type: TypeFloat},
	{Key: "strI16", Type: TypeInt16},
	{Key: "truncI32", Type: TypeInt32},
	{Key: "boolI64", Type: TypeInt64},
	{Key: "numStr", Type: TypeString},
	{Key: "numBool", Type: TypeBool},
	{Key: "negToU16", Type: TypeUint16},
	{Key: "sameD", Type: TypeDouble},
	{Key: "sameF", Type: TypeFloat},
	{Key: "sameI16", Type: TypeInt16},
	{Key: "sameU32", Type: TypeUint32},
	{Key: "sameS", Type: TypeString},
	{Key: "sameR", Type: TypeRaw},
	{Key: "sameB", Type: TypeBool},
}

var agreementData = map[string]any{
	"wideD":    float32(25.34), // single wire into a double tag: the measured case
	"narrowF":  float64(25.34), // double wire into a float tag
	"strI16":   "25",           // text wire into an int16 tag
	"truncI32": float64(2.9),   // double wire into an int32 tag
	"boolI64":  true,           // bool wire into an int64 tag
	"numStr":   int64(7),       // integer wire into a string tag
	"numBool":  int64(3),       // integer wire into a bool tag
	"negToU16": int64(300),     // signed wire into a uint16 tag
	"sameD":    float64(1.5),
	"sameF":    float32(1.5),
	"sameI16":  int16(-7),
	"sameU32":  uint32(70000),
	"sameS":    "text",
	"sameR":    []byte{0x01, 0x02},
	"sameB":    true,
}

// agreementTarget declares every key as the Go type its schema tag maps to,
// i.e. the declaration AGREES with the schema everywhere.
type agreementTarget struct {
	WideD    float64 `cbor:"wideD"`
	NarrowF  float32 `cbor:"narrowF"`
	StrI16   int16   `cbor:"strI16"`
	TruncI32 int32   `cbor:"truncI32"`
	BoolI64  int64   `cbor:"boolI64"`
	NumStr   string  `cbor:"numStr"`
	NumBool  bool    `cbor:"numBool"`
	NegToU16 uint16  `cbor:"negToU16"`
	SameD    float64 `cbor:"sameD"`
	SameF    float32 `cbor:"sameF"`
	SameI16  int16   `cbor:"sameI16"`
	SameU32  uint32  `cbor:"sameU32"`
	SameS    string  `cbor:"sameS"`
	SameR    []byte  `cbor:"sameR"`
	SameB    bool    `cbor:"sameB"`
}

// TestMessageToStructAgreesWithToMapWhenDeclarationMatchesSchema pins the
// D13 clarification: a concrete declaration wins over the schema only where
// the two CONFLICT — where they agree the field gets exactly what ToMap
// delivers for the same key, conversion matrix included. ToStruct used to send
// every non-`any` field straight to the codec, so schema double + a
// single-precision wire value read 25.34000015258789 from a float64 field
// while ToMap read 25.34 off the same bytes.
func TestMessageToStructAgreesWithToMapWhenDeclarationMatchesSchema(t *testing.T) {
	msg := newTestMessage(t, agreementData, agreementSchema, nil)

	fromMap := msg.ToMap()
	var fromStruct agreementTarget
	if err := msg.ToStruct(&fromStruct); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sv := reflect.ValueOf(fromStruct)
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		key := st.Field(i).Tag.Get("cbor")
		structValue := sv.Field(i).Interface()
		if got, want := fmt.Sprintf("%#v", structValue), fmt.Sprintf("%#v", fromMap[key]); got != want {
			t.Fatalf("field %q: ToStruct gave %s, ToMap gave %s — the two accessors must agree", key, got, want)
		}
	}
	if len(fromMap) != st.NumField() {
		t.Fatalf("ToMap returned %d keys but the target declares %d; the sweep is incomplete", len(fromMap), st.NumField())
	}

	// The reported case, pinned literally rather than only relative to ToMap.
	if fromStruct.WideD != 25.34 {
		t.Fatalf("wideD: got %v, want exactly 25.34 (not the codec-widened 25.34000015258789)", fromStruct.WideD)
	}
	if b, err := json.Marshal(fromStruct.WideD); err != nil || string(b) != "25.34" {
		t.Fatalf("wideD JSON literal: got %q (err=%v), want \"25.34\"", b, err)
	}
}

// TestMessageToStructPointerFieldsAgreeWithToMap repeats the agreement over
// pointer declarations, the shape the guide recommends for telling a real zero
// from undefined. They took the same schema-less shortcut as plain fields.
func TestMessageToStructPointerFieldsAgreeWithToMap(t *testing.T) {
	msg := newTestMessage(t, agreementData, agreementSchema, nil)
	fromMap := msg.ToMap()

	type pointerTarget struct {
		WideD   *float64 `cbor:"wideD"`
		NarrowF *float32 `cbor:"narrowF"`
		StrI16  *int16   `cbor:"strI16"`
		NumStr  *string  `cbor:"numStr"`
		NumBool *bool    `cbor:"numBool"`
		SameD   *float64 `cbor:"sameD"`
		SameR   *[]byte  `cbor:"sameR"`
		Absent  *float64 `cbor:"neverSent"`
	}
	var out pointerTarget
	if err := msg.ToStruct(&out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sv := reflect.ValueOf(out)
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		key := st.Field(i).Tag.Get("cbor")
		field := sv.Field(i)
		if key == "neverSent" {
			if !field.IsNil() {
				t.Fatalf("absent key must leave the pointer nil, got %v", field)
			}
			continue
		}
		if field.IsNil() {
			t.Fatalf("field %q: pointer left nil for a value ToMap delivered as %#v", key, fromMap[key])
		}
		if got, want := fmt.Sprintf("%#v", field.Elem().Interface()), fmt.Sprintf("%#v", fromMap[key]); got != want {
			t.Fatalf("field %q: ToStruct gave %s, ToMap gave %s", key, got, want)
		}
	}
	if *out.WideD != 25.34 {
		t.Fatalf("wideD through a pointer: got %v, want 25.34", *out.WideD)
	}
}

// TestMessageToStructDeclarationWinsOverConflictingSchema pins the other half
// of D13, untouched by the agreement fix: where the declared Go type is NOT
// the schema type's Go type, the declaration still drives the decode — with
// the codec's range checking and none of the conversion matrix.
func TestMessageToStructDeclarationWinsOverConflictingSchema(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"wideD":   float32(25.34), // schema double
		"narrowF": float64(25.34), // schema float
		"small":   int64(70000),   // schema int16: ToMap says undefined
		"named":   float32(25.34), // schema double, declared as a named float32
	}, []PortFieldSchema{
		{Key: "wideD", Type: TypeDouble},
		{Key: "narrowF", Type: TypeFloat},
		{Key: "small", Type: TypeInt16},
		{Key: "named", Type: TypeDouble},
	}, nil)

	type celsius float32
	type conflicting struct {
		WideD   float32 `cbor:"wideD"`   // declared narrower than the schema
		NarrowF float64 `cbor:"narrowF"` // declared wider than the schema
		Small   int32   `cbor:"small"`   // declared wide enough for a value the schema rejects
		Named   celsius `cbor:"named"`   // named type over a conflicting kind
	}
	var out conflicting
	if err := msg.ToStruct(&out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.WideD != float32(25.34) {
		t.Fatalf("WideD: got %v, want the declared float32 25.34", out.WideD)
	}
	if out.NarrowF != 25.34 {
		t.Fatalf("NarrowF: got %v, want the declared float64 25.34", out.NarrowF)
	}
	if out.Small != 70000 {
		t.Fatalf("Small: got %d, want 70000 — the wider declaration must beat the int16 schema", out.Small)
	}
	if out.Named != celsius(25.34) {
		t.Fatalf("Named: got %v, want celsius(25.34)", out.Named)
	}
	// ToMap, driven by the schema alone, disagrees on exactly these keys —
	// which is the point of D13, not a bug.
	if v := msg.ToMap()["small"]; v != nil {
		t.Fatalf("ToMap should still deliver the out-of-range int16 as undefined, got %#v", v)
	}

	// A conflict the codec cannot bridge still aborts naming the field.
	badMsg := newTestMessage(t, map[string]any{"bad": "not-a-number"},
		[]PortFieldSchema{{Key: "bad", Type: TypeString}}, nil)
	var bad struct {
		Bad float64 `cbor:"bad"`
	}
	if err := badMsg.ToStruct(&bad); err == nil {
		t.Fatal("expected an error for a text wire value into a float64 declaration")
	} else if !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("error should name the field, got: %v", err)
	}
}

// TestMessageToStructNeverForcesSchemaValueIntoConflictingKind pins the guard
// on the diverted path: when the wire representation does not match the field
// and the schema DOES produce a value, that value is only written if the
// declared kind is the schema's own. Without the guard a plain reflect
// conversion would apply — float64 -> int16 truncating, float64 -> float32
// saturating to +Inf, []byte -> string reinterpreting bytes — turning a loud
// codec error into silent corruption.
func TestMessageToStructNeverForcesSchemaValueIntoConflictingKind(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"frac":  float64(2.9),   // schema double, declared int16
		"huge":  float64(1e300), // schema double, declared float32
		"bytes": []byte{1, 2},   // schema raw, declared string
	}, []PortFieldSchema{
		{Key: "frac", Type: TypeDouble},
		{Key: "huge", Type: TypeDouble},
		{Key: "bytes", Type: TypeRaw},
	}, nil)

	// The schema route produces a usable value for each of these keys, so only
	// the kind guard stops it from being forced into the declaration.
	for _, key := range []string{"frac", "huge", "bytes"} {
		if v := msg.ToMap()[key]; v == nil {
			t.Fatalf("fixture broken: ToMap must deliver a value for %q for the guard to matter", key)
		}
	}

	cases := []struct {
		name   string
		target any
	}{
		{"double into int16 declaration", &struct {
			Frac int16 `cbor:"frac"`
		}{}},
		{"out-of-range double into float32 declaration", &struct {
			Huge float32 `cbor:"huge"`
		}{}},
		{"raw into string declaration", &struct {
			Bytes string `cbor:"bytes"`
		}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := msg.ToStruct(tc.target); err == nil {
				t.Fatalf("expected the declaration to be honoured and the codec to reject the value, got %+v", tc.target)
			}
		})
	}
}

// TestMessageToStructTagPrecedence pins field matching to the precedence the
// codec itself applies: `cbor` tag, else `json` tag, else the field name, with
// tag options stripped and a bare "-" skipping the field. It replaces the
// earlier TestMessageToStructIgnoresJSONTag, which pinned the opposite (a json
// tag silently missing) before the fallback was added. The one deliberate
// divergence from the codec is exact-case field-name matching.
func TestMessageToStructTagPrecedence(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"level":  float64(25.34),
		"count":  int64(7),
		"chosen": int64(1),
		"other":  int64(2),
		"-":      int64(3),
		"Exact":  int64(4),
		"temp":   int64(5),
	}, []PortFieldSchema{
		{Key: "level", Type: TypeDouble},
		{Key: "count", Type: TypeInt64},
	}, nil)

	t.Run("json tag drives matching when it is the only tag", func(t *testing.T) {
		var out struct {
			Level float64 `json:"level"`
			Count int64   `json:"count"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Level != 25.34 || out.Count != 7 {
			t.Fatalf("a json tag must select the wire key, got %+v", out)
		}
	})

	t.Run("cbor tag wins over json tag", func(t *testing.T) {
		var out struct {
			Value int64 `cbor:"chosen" json:"other"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Value != 1 {
			t.Fatalf("the cbor tag must win, got %d, want 1 (2 means the json tag won)", out.Value)
		}
	})

	t.Run("tag options are stripped from both tags", func(t *testing.T) {
		var out struct {
			FromCBOR int64 `cbor:"chosen,omitempty"`
			FromJSON int64 `json:"other,omitempty"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.FromCBOR != 1 || out.FromJSON != 2 {
			t.Fatalf("the name before the first comma must be the key, got %+v", out)
		}
	})

	t.Run("an options-only cbor tag keeps the field name", func(t *testing.T) {
		var out struct {
			Exact int64 `cbor:",omitempty" json:"other"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Exact != 4 {
			t.Fatalf("a present cbor tag must block the json fallback, got %d, want 4", out.Exact)
		}
	})

	t.Run("a bare dash skips the field in either tag", func(t *testing.T) {
		var out struct {
			CBORSkipped int64 `cbor:"-"`
			JSONSkipped int64 `json:"-"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.CBORSkipped != 0 || out.JSONSkipped != 0 {
			t.Fatalf("the wire key %q must not reach a dash-tagged field, got %+v", "-", out)
		}
	})

	t.Run("a dash with options names the field", func(t *testing.T) {
		var out struct {
			Dash int64 `json:"-,"`
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Dash != 3 {
			t.Fatalf(`json:"-," names the field "-" rather than skipping it, got %d, want 3`, out.Dash)
		}
	})

	t.Run("an untagged field matches its name exactly", func(t *testing.T) {
		var out struct {
			Exact int64
			Temp  int64
		}
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Exact != 4 {
			t.Fatalf("Exact: got %d, want 4 (field-name matching)", out.Exact)
		}
		if out.Temp != 0 {
			t.Fatalf("matching must be exact-case; %q must not fill Temp, got %d", "temp", out.Temp)
		}
	})

	// Drift guard: the precedence above is the codec's own, so a struct
	// carrying every tag shape must decode identically both ways. Only the
	// case-sensitivity divergence is kept out of this struct, since the codec
	// would fill Temp from "temp" and ToStruct deliberately will not.
	t.Run("the codec agrees on the same struct", func(t *testing.T) {
		type tagged struct {
			Level    float64 `json:"level"`
			Count    int64   `cbor:"count" json:"other"`
			Opts     int64   `json:"chosen,omitempty"`
			CBORSkip int64   `cbor:"-"`
			JSONSkip int64   `json:"-"`
		}
		var direct tagged
		if err := decMode.Unmarshal([]byte(msg.Data), &direct); err != nil {
			t.Fatalf("unexpected codec error: %v", err)
		}
		var out tagged
		if err := msg.ToStruct(&out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != direct {
			t.Fatalf("ToStruct and the codec disagree on field matching: ToStruct %+v, codec %+v", out, direct)
		}
		if direct.Level != 25.34 || direct.Count != 7 || direct.Opts != 1 {
			t.Fatalf("fixture broken: the codec must fill these for the agreement to mean anything, got %+v", direct)
		}
	})
}

// TestMessageToStructUndefinedSemantics pins D15 at the struct boundary: a
// non-pointer field stays at its zero value (gives up the 0-vs-undefined
// distinction), a pointer field keeps it (nil), and a set pointer field works.
func TestMessageToStructUndefinedSemantics(t *testing.T) {
	msg := newTestMessage(t, map[string]cbor.RawMessage{
		"nullInt": {0xf6},
		"set":     cbor.RawMessage(mustCBOR(t, float64(1.5))),
	}, []PortFieldSchema{
		{Key: "nullInt", Type: TypeInt64},
		{Key: "absent", Type: TypeDouble},
		{Key: "set", Type: TypeDouble},
	}, nil)

	type input struct {
		NullInt   int64    `cbor:"nullInt"` // null -> stays zero
		AbsentPtr *float64 `cbor:"absent"`  // absent -> stays nil
		NullPtr   *int64   `cbor:"nullInt"` // null -> stays nil
		SetPtr    *float64 `cbor:"set"`     // value -> set
		AbsentAny any      `cbor:"absent"`  // absent -> stays nil
	}
	in := input{NullInt: 0}
	if err := msg.ToStruct(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.NullInt != 0 {
		t.Fatalf("NullInt: got %d, want zero value", in.NullInt)
	}
	if in.AbsentPtr != nil || in.NullPtr != nil {
		t.Fatalf("pointer fields must stay nil for undefined: absent=%v null=%v", in.AbsentPtr, in.NullPtr)
	}
	if in.SetPtr == nil || *in.SetPtr != 1.5 {
		t.Fatalf("SetPtr: got %v, want 1.5", in.SetPtr)
	}
	if in.AbsentAny != nil {
		t.Fatalf("AbsentAny: got %#v, want nil", in.AbsentAny)
	}
}

// TestMessageToStructErrorsAndFieldRules pins the remaining ToStruct rules:
// invalid targets error, a concrete-field decode failure errors naming the
// field, `cbor:"-"` and unexported fields are skipped, and untagged fields
// match by field name.
func TestMessageToStructErrorsAndFieldRules(t *testing.T) {
	msg := newTestMessage(t, map[string]any{
		"bad":     "not-a-number",
		"Named":   int64(9),
		"skipped": int64(1),
	}, []PortFieldSchema{{Key: "bad", Type: TypeString}}, nil)

	// invalid targets
	if err := msg.ToStruct(nil); err == nil {
		t.Fatal("expected error for nil target")
	}
	var notStruct int
	if err := msg.ToStruct(&notStruct); err == nil {
		t.Fatal("expected error for non-struct target")
	}
	var byValue struct{}
	if err := msg.ToStruct(byValue); err == nil {
		t.Fatal("expected error for non-pointer target")
	}

	// concrete decode failure names the field
	type badInput struct {
		Bad int64 `cbor:"bad"`
	}
	var bi badInput
	if err := msg.ToStruct(&bi); err == nil {
		t.Fatal("expected error for string wire value into int64 declaration")
	} else if !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("error should name the field, got: %v", err)
	}

	// skip/naming rules
	type namedInput struct {
		Named   int64 // untagged: matched by field name
		Skipped int64 `cbor:"-"`
	}
	var ni namedInput
	if err := msg.ToStruct(&ni); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ni.Named != 9 {
		t.Fatalf("Named: got %d, want 9 (field-name matching)", ni.Named)
	}
	if ni.Skipped != 0 {
		t.Fatalf("Skipped: got %d, want untouched zero", ni.Skipped)
	}
}

// TestMessageToMapNilPlanNaturalDomain pins the schema-less case — a nil
// DecodePlan, i.e. no declared tags — as the whole natural domain: unsigned <=
// MaxInt64 becomes int64, larger stays uint64, null is nil, and no schema
// conversion happens.
func TestMessageToMapNilPlanNaturalDomain(t *testing.T) {
	raw := RawMessage(mustCBOR(t, map[string]any{
		"small": uint64(5),
		"big":   uint64(math.MaxUint64),
		"neg":   int64(-3),
		"d":     float64(2.5),
		"s":     "x",
		"b":     true,
		"bytes": []byte{1, 2},
		"nul":   nil,
	}))

	got := NewMessage("src", "ts", "input1", raw, nil, nil).ToMap()
	if got == nil {
		t.Fatal("ToMap returned nil for a valid map")
	}
	if v, ok := got["small"].(int64); !ok || v != 5 {
		t.Fatalf("small: got %#v (%T), want int64(5)", got["small"], got["small"])
	}
	if v, ok := got["big"].(uint64); !ok || v != math.MaxUint64 {
		t.Fatalf("big: got %#v (%T), want uint64 max", got["big"], got["big"])
	}
	if v, ok := got["neg"].(int64); !ok || v != -3 {
		t.Fatalf("neg: got %#v", got["neg"])
	}
	if got["d"] != float64(2.5) || got["s"] != "x" || got["b"] != true {
		t.Fatalf("scalar fields wrong: %#v", got)
	}
	if b, ok := got["bytes"].([]byte); !ok || !bytes.Equal(b, []byte{1, 2}) {
		t.Fatalf("bytes: got %#v", got["bytes"])
	}
	if v, exists := got["nul"]; !exists || v != nil {
		t.Fatalf("nul: exists=%v value=%#v", exists, v)
	}
}

// TestMessageToStructNilPlan pins the schema-less struct decode: concrete
// declarations drive the decode (with the codec's range checking), null leaves
// zero values, and pointers get nil.
func TestMessageToStructNilPlan(t *testing.T) {
	raw := RawMessage(mustCBOR(t, map[string]any{
		"count": uint64(7),
		"nul":   nil,
	}))

	type target struct {
		Count int32  `cbor:"count"`
		Nul   *int64 `cbor:"nul"`
	}
	var out target
	if err := NewMessage("src", "ts", "input1", raw, nil, nil).ToStruct(&out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Count != 7 {
		t.Fatalf("Count: got %d", out.Count)
	}
	if out.Nul != nil {
		t.Fatalf("Nul: got %v, want nil", out.Nul)
	}

	// range violation surfaces as a codec error
	over := RawMessage(mustCBOR(t, map[string]any{"count": int64(70000)}))
	var small struct {
		Count int16 `cbor:"count"`
	}
	if err := NewMessage("src", "ts", "input1", over, nil, nil).ToStruct(&small); err == nil {
		t.Fatal("expected range error for 70000 into int16 declaration")
	}
}
