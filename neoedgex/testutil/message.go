package testutil

import (
	"fmt"
	"maps"
	"slices"

	"github.com/fxamacker/cbor/v2"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/internal/logger"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// The envelope values a built message carries in the fields the runtime copies
// from the upstream publisher. Both are fixed so that tests and Example output
// stay reproducible.
const (
	messageSource    = "upstream-node"
	messageTimestamp = "2026-01-01T00:00:00Z"
)

// Undeclared is the Field type for a key that travels on the wire without the
// receiving node's input schema declaring it. Such a key takes the decoder's
// bypass path: it arrives in its natural CBOR domain — every float as float64,
// every unsigned integer that fits as int64 — instead of a schema type. That
// is what an upstream node's extra tag does in production.
//
// Leaving Field.Type unset is not the same thing and is rejected: an
// accidentally missing type is the one mistake this builder exists to catch.
const Undeclared contract.DataType = "testutil:undeclared"

// Field is one field of a message under construction: the value the upstream
// node put on the wire, and the type the receiving node's input schema
// declares that key as.
//
// The two are independent on purpose, because they are independent in
// production — the wire representation comes from the sending node's output
// schema, the decode type from the receiving node's input schema. Value is
// CBOR-encoded as the Go type written here (float32 as a CBOR single, float64
// as a CBOR double, uint64 as an unsigned integer, an untyped integer literal
// as an integer), and Type decides how the receiving node reads it back. So
// Value: float32(25.34) with Type: contract.TypeDouble reproduces exactly the
// single-into-double case and is delivered as float64 25.34, not
// 25.34000015258789.
//
// A nil Value is encoded as CBOR null, which is what a sending node publishes
// for a field it has no value for, and is delivered as undefined (nil).
type Field struct {
	Value any
	Type  contract.DataType
}

// Fields maps each field key of a message to its wire value and declared type.
type Fields map[string]Field

// MessageOption customizes a message built by NewMessage or
// MockNodeEnv.NewMessage.
type MessageOption func(*messageOptions)

type messageOptions struct {
	logger contract.Logger
}

// WithLogger attaches nodeLogger to the message, in place of the node logger
// the runtime attaches. The accessors report per-field decode failures there
// and nowhere else — a wire value the schema type cannot hold is delivered as
// nil and only the log line says why — so a test that wants to assert on those
// reasons has to supply a logger. Without it the message carries a no-op
// logger and the reasons are dropped.
func WithLogger(nodeLogger contract.Logger) MessageOption {
	return func(options *messageOptions) { options.logger = nodeLogger }
}

// NewMessage builds the message a node handler receives on handle, the way the
// runtime builds it: the envelope fields the upstream publisher fills, a
// CBOR-encoded data section, and the receiving node's input schema attached as
// a decode plan, so ToMap and ToStruct return schema-typed values.
//
// fields carries both halves the decode path needs, one entry per key: the
// value as an ordinary Go value, and the type the receiving node's input
// schema declares that key as. Both are mandatory. A message built without a
// plan still decodes, but every key silently takes the bypass path — a value
// declared float arrives as float64 instead of float32, a uint64 as int64 — so
// a handler tested that way is tested against types it never sees in
// production. A key the input schema genuinely does not declare is written
// with Undeclared, which puts that one key on the bypass path deliberately.
//
// The envelope carries source "upstream-node" and timestamp
// "2026-01-01T00:00:00Z". Both are plain exported fields of the result, so a
// handler that reads them can still be fed anything: assign msg.Source or
// msg.Timestamp after building.
//
// When the node configuration under test is available, prefer
// MockNodeEnv.NewMessage: it reads the types out of the configured input
// schema instead of having them repeated here.
//
// It panics instead of returning an error. Every failure is a static defect in
// the test's own literal — a missing or unsupported Type, a value no CBOR
// encoder can represent — never a runtime condition a test could react to, so
// an error return would add an `if err != nil` to every call site for a case
// that only means the test is malformed. Panicking also keeps the builder
// usable where no testing.TB exists, in Example functions and package-level
// fixtures, which a t.Fatalf-style helper cannot serve.
func NewMessage(handle string, fields Fields, opts ...MessageOption) contract.Message {
	const caller = "testutil.NewMessage"

	values := make(map[string]any, len(fields))
	schema := make([]contract.PortFieldSchema, 0, len(fields))
	for key, field := range fields {
		values[key] = field.Value
		if field.Type == Undeclared {
			continue
		}
		if !field.Type.IsSupported() {
			panic(fmt.Sprintf("%s: field %q: %s", caller, key, undeclarableTypeReason(field.Type)))
		}
		schema = append(schema, contract.PortFieldSchema{Key: key, Type: field.Type})
	}

	return buildMessage(caller, handle, values, contract.NewDecodePlan(schema), opts)
}

// NewMessage builds a message for handle out of the input schema in Config, so
// the values need no types written out: Config.Data.Inputs[handle] is the same
// schema the runtime compiles its decode plan from, which makes this the
// closest reproduction of a real delivery a test can get. The message also
// carries this env's logger, as the runtime attaches the node's own.
//
// data is the wire payload keyed by field key. A key the schema declares but
// data omits is absent from the wire and delivered as undefined (nil), like a
// value the upstream never produced; a key data carries but the schema does
// not declare takes the bypass path, like an extra tag from an upstream whose
// output schema is wider than this node's input schema.
//
// It panics if handle is not declared in Config.Data.Inputs, because the
// runtime would then attach no decode plan at all and every field would
// silently bypass the schema. A test that means to exercise a handle the node
// does not declare should say so with the package-level NewMessage and
// Undeclared field types.
func (m *MockNodeEnv) NewMessage(handle string, data map[string]any, opts ...MessageOption) contract.Message {
	const caller = "testutil.MockNodeEnv.NewMessage"

	schema, declared := m.Config.Data.Inputs[handle]
	if !declared {
		panic(fmt.Sprintf(
			"%s: handle %q is not declared in Config.Data.Inputs (declared: %v); "+
				"to build a message for a handle the node does not declare, use testutil.NewMessage with Undeclared field types",
			caller, handle, slices.Sorted(maps.Keys(m.Config.Data.Inputs))))
	}

	values := make(map[string]any, len(data))
	maps.Copy(values, data)

	return buildMessage(caller, handle, values, contract.NewDecodePlan(schema),
		append([]MessageOption{WithLogger(m.Logger())}, opts...))
}

// Deliver queues messages on MessageChan and closes it, which is what lets a
// handler's `for msg := range ctx.Messages()` loop return so the test can
// assert on what it did. It replaces whatever MessageChan held.
//
// It is setup only: call it before handing the env to the handler, because
// Messages() reads the field without taking the mutex. A handler that has to
// see an open channel — one under test for context cancellation, say — needs
// MessageChan assigned directly instead.
func (m *MockNodeEnv) Deliver(messages ...contract.Message) {
	channel := make(chan contract.Message, len(messages))
	for _, message := range messages {
		channel <- message
	}
	close(channel)
	m.MessageChan = channel
}

// buildMessage mirrors the runtime's inbound construction: the data map is
// CBOR-encoded exactly as the sending node's Publish encodes it, and the
// decoded envelope is handed to contract.NewMessage together with the decode
// plan of the receiving handle.
func buildMessage(caller, handle string, values map[string]any, plan contract.DecodePlan, opts []MessageOption) contract.Message {
	options := messageOptions{logger: logger.NewNoopLogger()}
	for _, opt := range opts {
		opt(&options)
	}

	data, err := cbor.Marshal(values)
	if err != nil {
		if key, found := firstUnencodableField(values); found {
			panic(fmt.Sprintf("%s: field %q: value of type %T cannot be CBOR-encoded: %v", caller, key, values[key], err))
		}
		panic(fmt.Sprintf("%s: cannot CBOR-encode the field values: %v", caller, err))
	}

	return contract.NewMessage(messageSource, messageTimestamp, handle, contract.RawMessage(data), plan, options.logger)
}

// firstUnencodableField names the field that made the whole map fail to
// encode, so the panic points at the literal to fix. Keys are walked in sorted
// order because Go map iteration is random and a panic message has to be
// reproducible.
func firstUnencodableField(values map[string]any) (string, bool) {
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if _, err := cbor.Marshal(values[key]); err != nil {
			return key, true
		}
	}
	return "", false
}

func undeclarableTypeReason(dataType contract.DataType) string {
	if dataType == contract.TypeUndefined {
		return "no Type declared; set it to the contract.Type* constant the receiving node's input schema declares for this key, " +
			"or to testutil.Undeclared if the schema does not declare the key at all"
	}
	return fmt.Sprintf("type %q is not a declarable schema type", dataType)
}
