package contract

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// NeoFlowMessage is the CBOR envelope of a NeoFlow data message: the
// publishing node, the publish time, and the still-encoded data map. A node
// application receives Message instead.
type NeoFlowMessage struct {
	SourceNodeID string          `cbor:"source"`
	Timestamp    string          `cbor:"timestamp"`
	Data         cbor.RawMessage `cbor:"data"`
}

// PublishTimestampLayout is the time layout a NeoFlowMessage.Timestamp is
// written with on the publish side: RFC3339 with a fixed three-digit fraction.
// Tags are polled on millisecond intervals, so a second-precision stamp
// collapses every sample taken within the same second into one
// indistinguishable time. The SDK formats a UTC time with it, so a published
// stamp always ends in "Z"; a component that builds an envelope outside the
// SDK should do the same.
//
// The fraction is fixed-width on purpose: time.RFC3339Nano trims trailing
// zeros and drops the fraction entirely on a whole second, which would hand
// consumers a variable-length string. Fixed width also keeps the format
// backward compatible in both directions, because time.RFC3339 parses both a
// stamp written this way and a second-precision one from an older publisher.
//
// It describes the publish side only. An inbound timestamp is delivered to the
// handler verbatim and is NEVER validated against this layout, so a message
// from an older or non-SDK publisher may carry any RFC3339 form — including a
// numeric zone offset or a different fractional width.
const PublishTimestampLayout = "2006-01-02T15:04:05.000Z07:00"

// RawMessage is the undecoded data section of a NeoFlow message: bytes holding
// a CBOR map of field key to field value. Message.ToMap and Message.ToStruct
// decode it against the receiving node's input schema; a handler that wants
// the wire values untouched can unmarshal the bytes with a CBOR codec itself.
type RawMessage cbor.RawMessage

// Message is an incoming NeoFlow message delivered to a node handler.
//
// Undefined semantics: a field whose value is undefined on the wire (CBOR
// null, or the key absent because the upstream schema does not carry it, or
// a value that cannot be decoded or converted to the input schema type)
// surfaces as nil in ToMap results. See ToStruct for how undefined interacts
// with struct field types.
//
// Only NewMessage produces a fully working Message, because the input schema
// and the logger are unexported. A Message assembled as a composite literal
// still decodes, but every key takes the unknown-tag path, so schema typing is
// lost silently — a value declared double arrives as 25.34000015258789 instead
// of 25.34 — and no warning can be logged.
type Message struct {
	// Source is the publishing node's ID.
	Source string
	// Timestamp is the publish time in RFC3339 form, delivered exactly as the
	// upstream node wrote it and never validated. A node on this SDK version
	// stamps it in UTC to millisecond precision ("2026-03-22T10:30:00.123Z");
	// one on an older version stamps whole seconds in its own local zone
	// ("2026-03-22T18:30:00+08:00"), so parse with the time.RFC3339 layout,
	// which accepts both. It is empty when the inbound envelope carries no
	// timestamp field at all.
	Timestamp string
	// Handle is the input port the message arrived on. It selects the input
	// schema the accessors decode Data with.
	Handle string
	// Data is the undecoded CBOR data map; prefer ToMap or ToStruct over
	// reading it directly.
	Data RawMessage

	plan   DecodePlan
	logger Logger
}

// DecodePlan is the precompiled form of an input schema: each declared key
// mapped to its schema type.
type DecodePlan map[string]DataType

// NewDecodePlan precompiles an input schema into a DecodePlan.
func NewDecodePlan(schema []PortFieldSchema) DecodePlan {
	plan := make(DecodePlan, len(schema))
	for _, f := range schema {
		plan[f.Key] = f.Type
	}
	return plan
}

// NewMessage assembles an app-facing Message from an already-decoded wire
// envelope.
//
// data must hold a CBOR map; with anything else every accessor fails, ToMap
// returning nil and ToStruct an error. plan is the input schema of the
// receiving handle: a nil plan is legal and puts every key on the unknown-tag
// bypass path, giving up schema typing. logger is where the accessors report
// per-field decode failures, so a nil logger makes those failures invisible.
func NewMessage(source, timestamp, handle string, data RawMessage, plan DecodePlan, logger Logger) Message {
	return Message{
		Source:    source,
		Timestamp: timestamp,
		Handle:    handle,
		Data:      data,
		plan:      plan,
		logger:    logger,
	}
}

var decMode, _ = cbor.DecOptions{
	DefaultMapType: reflect.TypeOf(map[string]any(nil)),
}.DecMode()

// ToMap decodes the data map using the receiving node's input schema: each
// schema-declared field is decoded as the concrete Go type of its tag type
// (int16 -> int16, raw -> []byte, ...). When the wire value's kind — or, for
// floats, its wire precision — differs from the schema type, the same
// cross-type conversion matrix used on the Publish side is applied (integer
// range checks, float->int truncation, string->number parsing, NaN/Inf
// rejected); a single-precision wire value into a double schema is restored
// to its shortest-decimal value (float32(25.34) -> float64 25.34, not
// 25.34000015258789). If the matrix does not allow the conversion or it
// fails, the field is delivered as undefined (nil).
//
// Undefined semantics are uniform: whatever cannot be decoded or converted
// is delivered as nil with the key still present. That covers a schema field
// whose key is absent, whose wire value is CBOR null, or whose conversion
// failed, and equally an unknown tag whose natural decode failed (a nested
// map with non-string keys: accepted by the outer map decode, rejected by the
// value decode) or whose value has no natural-domain representation (a CBOR
// bignum, array or map). Keys present on the wire but not declared in the
// input schema are otherwise passed through ("bypass") with natural-domain
// normalization and a debug log: only bool, int64, uint64, float64, string
// and []byte are delivered, unsigned integers <= math.MaxInt64 become int64,
// and every CBOR float becomes float64, widening single-precision wire values.
//
// The SDK verifies on receipt that the data segment is a CBOR map, so
// failure here is pathological; in that case ToMap logs and returns nil.
func (msg Message) ToMap() map[string]any {
	fields, err := msg.dataFields()
	if err != nil {
		return nil
	}

	out := make(map[string]any, len(fields))
	for key, tagType := range msg.plan {
		raw, present := fields[key]
		if !present || isCBORUndefined(raw) {
			out[key] = nil
			continue
		}
		v, err := decodeFieldWithSchema(raw, tagType)
		if err != nil {
			msg.warnf("Field %q: %v; delivering undefined", key, err)
			out[key] = nil
			continue
		}
		out[key] = v
	}

	for k, raw := range fields {
		if _, ok := msg.plan[k]; ok {
			continue
		}
		if isCBORUndefined(raw) {
			out[k] = nil
			continue
		}
		v, err := decodeNatural(raw)
		if err != nil {
			msg.warnf("Unknown tag %q: %v; delivering undefined", k, err)
			out[k] = nil
			continue
		}
		msg.debugf("Tag %q is not defined in the input schema; bypassing with natural value", k)
		out[k] = v
	}
	return out
}

// ToStruct decodes the data map into target (a non-nil pointer to a struct).
//
// # Field matching
//
// A field's wire key is its `cbor` struct tag, or — when there is no `cbor`
// tag or it is empty — its `json` struct tag, or else the field name. So
//
//	Level float64 `json:"level"`
//
// is matched under "level", and a `cbor` tag next to a `json` one wins. Tag
// options are stripped: `json:"level,omitempty"` and `cbor:"level,omitempty"`
// both match "level". This is the precedence the codec itself applies, edge
// included — an options-only `cbor:",omitempty"` still counts as a `cbor` tag,
// so it falls back to the field name and the `json` tag beside it is not read.
//
// Unexported fields are skipped, and so is a field whose winning tag is
// exactly "-", both `cbor:"-"` and `json:"-"`. As in encoding/json, `json:"-,"`
// is not that: it names the field "-".
//
// Matching is exact-case, so a field named Temp is not filled by the wire key
// "temp". The codec's case-insensitive fallback is out of reach here, because
// ToStruct walks the struct itself instead of handing it to the codec whole.
//
// # Field typing
//
// A concrete field whose declared Go type is the one its key's input schema
// type maps to receives exactly what ToMap delivers for that key, conversion
// matrix included: a double-declared key sent as a single-precision float
// reaches a float64 field as 25.34, not 25.34000015258789, and a string "25"
// on an int16-declared key reaches an int16 field as 25. Pointer fields follow
// their element type. Where the declaration CONFLICTS with the schema type the
// declaration still wins: the value is decoded directly as declared, with the
// codec's range checking and none of the matrix.
//
// An `any` (interface{}) field receives the schema-typed value for its key —
// or, when the key is not declared in the input schema, the natural-domain
// value: every CBOR float becomes float64, widening single-precision wire
// values, and a value outside the natural domain (a CBOR bignum, array or map)
// leaves the field nil.
//
// Undefined follows two mutually exclusive rules.
//
// A key that is absent, or whose wire value is CBOR null, leaves its field
// completely untouched whatever the field's kind: it keeps what it already
// held, which in a freshly declared struct is the Go zero value. A non-pointer
// field therefore cannot tell "no value" from a real zero; declare it as a
// pointer (or `any`) to keep that distinction.
//
// A key that is present but cannot be decoded or converted splits by field
// kind. An `any` field is left nil and the failure is only logged, matching
// ToMap. A concrete field aborts the call instead: ToStruct returns an error
// naming the field, because it has no way to hold undefined — an int16 field
// rejects 70000, which ToMap delivers as undefined.
//
// On that error target is already partially written: fields processed before
// the failing one keep their decoded values and fields after it are never
// reached. A target must be discarded, not inspected, when ToStruct fails.
func (msg Message) ToStruct(target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a non-nil pointer to a struct, got %T", target)
	}

	fields, err := msg.dataFields()
	if err != nil {
		return fmt.Errorf("cannot decode data map: %w", err)
	}

	sv := rv.Elem()
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if !field.IsExported() {
			continue
		}
		key, matched := fieldKey(field)
		if !matched {
			continue
		}
		raw, present := fields[key]
		if !present || isCBORUndefined(raw) {
			continue
		}

		if field.Type.Kind() == reflect.Interface && field.Type.NumMethod() == 0 {
			var v any
			var err error
			if tagType, declared := msg.plan[key]; declared {
				v, err = decodeFieldWithSchema(raw, tagType)
			} else {
				v, err = decodeNatural(raw)
			}
			if err != nil {
				msg.warnf("Field %q: %v; leaving nil", key, err)
				continue
			}
			sv.Field(i).Set(reflect.ValueOf(v))
			continue
		}

		// Only a wire representation the declared type cannot take directly can
		// make the schema change the outcome, so the head byte is checked first
		// and the plan is consulted only then. The matching case keeps the plain
		// decode into the field pointer: no lookup, no boxing, no reflect.Set.
		if len(raw) > 0 && !wireMatchesField(raw[0], field.Type) {
			if tagType, declared := msg.plan[key]; declared {
				if v, schemaErr := decodeFieldWithSchema(raw, tagType); schemaErr == nil && assignSchemaValue(sv.Field(i), v) {
					continue
				}
				// A schema value the declared type cannot hold means the two
				// genuinely conflict, and the declaration wins (D13); a schema
				// error means the value is bad for both. Either way the direct
				// decode below produces the declared-type result or the error.
			}
		}

		if err := decMode.Unmarshal(raw, sv.Field(i).Addr().Interface()); err != nil {
			return fmt.Errorf("field %q: %w", key, err)
		}
	}
	return nil
}

// wireMatchesField reports whether a CBOR value with this head byte decodes
// into a field of type fieldType the same way the schema route would, judged
// on the head byte alone. It is deliberately conservative: false only costs a
// plan lookup and a matrix attempt, whereas a wrong true would keep the
// divergence ToStruct is fixing. Float width counts, which is the whole point
// — a single-precision wire value into a float64 field must go through the
// matrix to come back as 25.34 rather than 25.34000015258789.
func wireMatchesField(head byte, fieldType reflect.Type) bool {
	switch fieldType.Kind() {
	case reflect.Pointer:
		return wireMatchesField(head, fieldType.Elem())
	case reflect.Bool:
		return head == cborFalseHead || head == cborTrueHead
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return head>>5 == cborUnsignedIntMajor || head>>5 == cborNegativeIntMajor
	case reflect.Float32:
		return head == cborSingleFloatHead
	case reflect.Float64:
		return head == cborDoubleFloatHead
	case reflect.String:
		return head>>5 == cborTextStringMajor
	case reflect.Slice:
		return head>>5 == cborByteStringMajor
	default:
		return false
	}
}

// assignSchemaValue writes a schema-decoded value into a struct field,
// allocating through pointer declarations, and reports whether the declared
// type could hold it. A false result leaves the field untouched and means the
// declaration conflicts with the schema type.
//
// The kind check is what keeps the conversion honest: reflect.Convert alone
// would happily turn a float64 into an int16 field and re-introduce silent
// truncation, so only same-kind conversions (named types over the schema's own
// Go type, e.g. `type Celsius float64`) are allowed through.
func assignSchemaValue(field reflect.Value, value any) bool {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return false
	}

	target := field.Type()
	depth := 0
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
		depth++
	}
	if rv.Kind() != target.Kind() || !rv.Type().ConvertibleTo(target) {
		return false
	}

	rv = rv.Convert(target)
	for ; depth > 0; depth-- {
		boxed := reflect.New(rv.Type())
		boxed.Elem().Set(rv)
		rv = boxed
	}
	field.Set(rv)
	return true
}

// dataFields is the single decode choke point for the data section; the Warn
// here is the SDK-layer trace for corrupt traffic, so the accessors above must
// not log the same failure again.
func (msg Message) dataFields() (map[string]cbor.RawMessage, error) {
	var fields map[string]cbor.RawMessage
	if err := decMode.Unmarshal([]byte(msg.Data), &fields); err != nil {
		msg.warnf("cannot decode data map (source=%q, handle=%q): %v", msg.Source, msg.Handle, err)
		return nil, err
	}
	return fields, nil
}

func (msg Message) debugf(format string, args ...any) {
	if msg.logger != nil {
		msg.logger.Debug(format, args...)
	}
}

func (msg Message) warnf(format string, args ...any) {
	if msg.logger != nil {
		msg.logger.Warn(format, args...)
	}
}

// fieldKey resolves a struct field's wire key and reports whether the field
// takes part at all, reproducing the codec's own tag precedence so a struct
// matches the same way here as in a whole-struct decode. Two details carry
// that: an empty `cbor` tag falls through to `json` (the codec reads the tag
// value, not its presence), and the skip test is on the whole tag rather than
// the parsed name, because `json:"-,"` names a field "-" instead of skipping.
func fieldKey(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("cbor")
	if tag == "" {
		tag = field.Tag.Get("json")
	}
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name, true
	}
	return name, true
}

// isCBORUndefined reports whether raw is a CBOR null (0xf6) or undefined
// (0xf7) primitive — both map to the SDK's undefined semantics.
func isCBORUndefined(raw cbor.RawMessage) bool {
	return len(raw) == 1 && (raw[0] == 0xf6 || raw[0] == 0xf7)
}

const (
	cborSingleFloatHead = 0xfa
	cborDoubleFloatHead = 0xfb
	cborFalseHead       = 0xf4
	cborTrueHead        = 0xf5
)

// The CBOR major type lives in the top 3 bits of a value's head byte.
const (
	cborUnsignedIntMajor = 0
	cborNegativeIntMajor = 1
	cborByteStringMajor  = 2
	cborTextStringMajor  = 3
)

// decodeFieldWithSchema decodes one field as the concrete Go type of the
// schema tag type. A same-kind, same-width wire value decodes directly (the
// codec enforces the value range, e.g. 70000 into int16 fails); on failure
// the wire value is re-read in the natural domain and routed through the
// cross-type conversion matrix shared with the Publish side.
//
// Float width mismatches must be routed to the matrix BEFORE the typed
// decode: the codec widens a single-precision wire value into float64
// without error, which would skip the matrix and lose the shortest-decimal
// restore (float32(25.34) must surface as float64 25.34, not
// 25.34000015258789).
func decodeFieldWithSchema(raw cbor.RawMessage, tagType DataType) (any, error) {
	if len(raw) > 0 {
		switch {
		case raw[0] == cborSingleFloatHead && tagType == TypeDouble:
			var v float32
			if err := decMode.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return ConvertToTypedValue(v, tagType)
		case raw[0] == cborDoubleFloatHead && tagType == TypeFloat:
			var v float64
			if err := decMode.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			return ConvertToTypedValue(v, tagType)
		}
	}

	v, typedErr := decodeTyped(raw, tagType)
	if typedErr == nil {
		return v, nil
	}

	natural, err := decodeNatural(raw)
	if err != nil {
		return nil, err
	}
	converted, convErr := ConvertToTypedValue(natural, tagType)
	if convErr != nil {
		return nil, convErr
	}
	return converted, nil
}

func decodeTyped(raw cbor.RawMessage, tagType DataType) (any, error) {
	switch tagType {
	case TypeInt16:
		var v int16
		return v, decMode.Unmarshal(raw, &v)
	case TypeInt32:
		var v int32
		return v, decMode.Unmarshal(raw, &v)
	case TypeInt64:
		var v int64
		return v, decMode.Unmarshal(raw, &v)
	case TypeUint16:
		var v uint16
		return v, decMode.Unmarshal(raw, &v)
	case TypeUint32:
		var v uint32
		return v, decMode.Unmarshal(raw, &v)
	case TypeUint64:
		var v uint64
		return v, decMode.Unmarshal(raw, &v)
	case TypeFloat:
		var v float32
		return v, decMode.Unmarshal(raw, &v)
	case TypeDouble:
		var v float64
		return v, decMode.Unmarshal(raw, &v)
	case TypeBool:
		var v bool
		return v, decMode.Unmarshal(raw, &v)
	case TypeString:
		var v string
		return v, decMode.Unmarshal(raw, &v)
	case TypeRaw:
		var v []byte
		return v, decMode.Unmarshal(raw, &v)
	default:
		return nil, fmt.Errorf("unsupported schema type '%s'", tagType)
	}
}

func decodeNatural(raw cbor.RawMessage) (any, error) {
	var v any
	if err := decMode.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return naturalValue(v)
}

// naturalValue normalizes a codec-default decoded value to the natural Go
// domain and rejects everything outside it, so the caller can deliver
// undefined. The delivered domain is exactly nil, bool, int64, uint64,
// float64, string and []byte. CBOR unsigned integers that fit int64 become
// int64. Floats need no case here: the codec already decodes every CBOR float
// into float64 when the target is any, so single-precision wire values arrive
// pre-widened.
//
// Two kinds of value reach the reject branch. A CBOR array or map: no tag type
// can declare a container and Publish refuses container values, so one can
// only arrive from a non-SDK publisher — it is rejected whole, which keeps the
// caller's one-warn-per-tag rule instead of silently dropping single elements.
// And math/big.Int, which the codec produces for a CBOR bignum and for an
// integer past int64/uint64 range; delivering it would corrupt silently
// because boxed in a map[string]any it is not addressable, so its
// pointer-receiver MarshalJSON never runs and json.Marshal emits {} with a
// nil error.
func naturalValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, int64, float64, string, []byte:
		return v, nil
	case uint64:
		if t <= 1<<63-1 {
			return int64(t), nil
		}
		return t, nil
	default:
		return nil, fmt.Errorf("unsupported value type '%T'", v)
	}
}
