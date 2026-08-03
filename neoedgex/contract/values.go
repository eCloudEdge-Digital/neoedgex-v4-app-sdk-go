package contract

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strconv"
)

// PortFieldData is a typed value container: one port field value held in
// stringified form together with the schema type it is to be read as.
//
// It is not a wire type — NeoFlow messages carry native CBOR values and
// publishing never produces one. It is used in two places. Device-facing
// application code builds a field from a raw device value with
// NewPortFieldDataWithString or NewPortFieldDataWithAny (NewEmptyField for
// "no value") and reads it back out with GetValueAndCast. And a mock
// configuration file stores exactly this shape: mock.MockMessage.Data is a map
// of these, which the SDK converts to native Go values at injection time.
//
// The zero value is an empty field: TypeUndefined, from which GetAnyValue
// returns an error.
type PortFieldData struct {
	// Type is the schema type Value is parsed as.
	Type DataType `json:"type"`
	// Value is the stringified value. NewPortFieldDataWithAny writes it in the
	// form ConvertAnyValue produces (floats in scientific notation, raw as
	// base64, bool as "true"/"false"); NewPortFieldDataWithString stores the
	// caller's string verbatim, so "0025" stays "0025".
	Value string `json:"value"`
}

// NewPortFieldDataWithString builds a field from an already-stringified value,
// storing it verbatim. It fails if dataType is not declarable in a schema, or
// if value does not parse as that type.
//
// TypeBool is the exception: it can never fail, because parsing a bool is a
// plain value == "true" comparison. "TRUE", "1" and "garbage" are all accepted
// and every one of them reads back as false. Normalize the string yourself
// before calling if the input is not already "true"/"false".
func NewPortFieldDataWithString(value string, dataType DataType) (*PortFieldData, error) {
	if !dataType.IsSupported() {
		return nil, fmt.Errorf("unsupported data type '%s'", dataType)
	}

	if _, err := ConvertValueByType(value, dataType); err != nil {
		return nil, fmt.Errorf("value '%s' is not compatible with type '%s': %v", value, dataType, err)
	}

	return &PortFieldData{
		Type:  dataType,
		Value: value,
	}, nil
}

// NewPortFieldDataWithAny builds a field from a native Go value, converting it
// to destType and then stringifying it, so the stored Value is normalized
// (float64 25.34 becomes "2.534e+01", []byte becomes base64). It fails
// wherever ConvertToTypedValue does — a nil value, a Go type with no schema
// type such as a struct or a map, a value outside destType's range, or a
// disallowed pair such as string to bool.
func NewPortFieldDataWithAny(anyValue any, destType DataType) (*PortFieldData, error) {
	typed, err := ConvertToTypedValue(anyValue, destType)
	if err != nil {
		return nil, err
	}

	value, _, err := ConvertAnyValue(typed)
	if err != nil {
		return nil, err
	}

	return &PortFieldData{
		Type:  destType,
		Value: value,
	}, nil
}

// NewEmptyField returns a field carrying no value: TypeUndefined and an empty
// Value. It is how a device driver represents a register it could not read.
// GetAnyValue and GetValueAndCast return an error on such a field.
func NewEmptyField() *PortFieldData {
	return &PortFieldData{
		Type: TypeUndefined,
	}
}

// GetValueAndCast parses a field's value and returns it as T, giving the zero
// value of T on error.
//
// The cast is a plain type assertion with no numeric widening, so T must be
// exactly the Go type the field's schema type maps to: an int16 field yields
// int16 and asking for int32 fails. It also fails whenever GetAnyValue does,
// notably on an empty field.
func GetValueAndCast[T any](PortFieldDataValue PortFieldData) (T, error) {
	var zero T
	if anyValue, err := PortFieldDataValue.GetAnyValue(); err != nil {
		return zero, err
	} else if castedValue, ok := anyValue.(T); !ok {
		return zero, fmt.Errorf("cannot cast value of type '%T' to target type", anyValue)
	} else {
		return castedValue, nil
	}
}

// GetAnyValue parses the field's Value as its Type and returns the native Go
// value. It returns an error if Value does not parse, or if Type is
// TypeUndefined, as it is on an empty field.
//
// A bool field never fails to parse: see ConvertValueByType.
func (v PortFieldData) GetAnyValue() (any, error) {
	return ConvertValueByType(v.Value, v.Type)
}

// ConvertTo returns the field re-expressed as destType, following the same
// rules as ConvertToTypedValue, so it fails on a disallowed pair such as raw
// to string and on a value out of the destination's range.
//
// When destType already equals the field's type it short-circuits: the field
// is copied as-is without parsing Value, so an unparseable value is passed
// through rather than reported.
func (v PortFieldData) ConvertTo(destType DataType) (*PortFieldData, error) {
	if v.Type == destType {
		return &v, nil
	}

	srcValue, err := v.GetAnyValue()
	if err != nil {
		return nil, err
	}

	return NewPortFieldDataWithAny(srcValue, destType)
}

// ConvertAnyValue stringifies a native Go scalar value and reports the schema
// type it was detected as.
//
// Floats are formatted in scientific notation at shortest round-trip
// precision, so 25.34 becomes "2.534e+01", and []byte is base64-encoded.
// Integers are plain decimal and bool becomes "true"/"false". A nil value, or
// one whose Go type has no schema type (see GetDataType), returns an error
// and TypeUndefined.
//
// The reported type follows GetDataType, so every Go integer kind is accepted
// and the narrow ones widen: int8 reports int16, uint8 reports uint16, and the
// unsized int and uint report int64 and uint64.
func ConvertAnyValue(anyValue any) (string, DataType, error) {
	if isNilAnyValue(anyValue) {
		return "", TypeUndefined, fmt.Errorf("nil value is not supported for conversion")
	}

	switch value := anyValue.(type) {
	case int8:
		return strconv.FormatInt(int64(value), 10), TypeInt16, nil
	case int16:
		return strconv.FormatInt(int64(value), 10), TypeInt16, nil
	case int32:
		return strconv.FormatInt(int64(value), 10), TypeInt32, nil
	case int:
		return strconv.FormatInt(int64(value), 10), TypeInt64, nil
	case int64:
		return strconv.FormatInt(value, 10), TypeInt64, nil
	case uint8:
		return strconv.FormatUint(uint64(value), 10), TypeUint16, nil
	case uint16:
		return strconv.FormatUint(uint64(value), 10), TypeUint16, nil
	case uint32:
		return strconv.FormatUint(uint64(value), 10), TypeUint32, nil
	case uint:
		return strconv.FormatUint(uint64(value), 10), TypeUint64, nil
	case uint64:
		return strconv.FormatUint(value, 10), TypeUint64, nil
	case float32:
		return strconv.FormatFloat(float64(value), 'e', -1, 32), TypeFloat, nil
	case float64:
		return strconv.FormatFloat(value, 'e', -1, 64), TypeDouble, nil
	case string:
		return value, TypeString, nil
	case bool:
		return strconv.FormatBool(value), TypeBool, nil
	case []byte:
		return base64.StdEncoding.EncodeToString(value), TypeRaw, nil
	default:
		return "", TypeUndefined, fmt.Errorf("unsupported value type '%T' for conversion", anyValue)
	}
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

// ConvertValueByType parses a stringified value into the concrete Go type of a
// schema type — the inverse of ConvertAnyValue. Integers and floats go through
// strconv, raw is base64-decoded and string is returned unchanged.
//
// The srcType parameter is misnamed: it is the DESTINATION type the value is
// parsed as, as the function's own error text ("unsupported destination type")
// says.
//
// TypeBool is the one branch that can never fail. It is a plain
// value == "true" comparison, so "TRUE", "1", "yes" and "" all yield false
// with a nil error rather than being rejected.
func ConvertValueByType(value string, srcType DataType) (any, error) {
	switch srcType {
	case TypeInt16:
		if intValue, err := strconv.ParseInt(value, 10, 16); err != nil {
			return nil, err
		} else {
			return int16(intValue), nil
		}

	case TypeInt32:
		if intValue, err := strconv.ParseInt(value, 10, 32); err != nil {
			return nil, err
		} else {
			return int32(intValue), nil
		}

	case TypeInt64:
		if intValue, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, err
		} else {
			return intValue, nil
		}

	case TypeUint16:
		if uintValue, err := strconv.ParseUint(value, 10, 16); err != nil {
			return nil, err
		} else {
			return uint16(uintValue), nil
		}

	case TypeUint32:
		if uintValue, err := strconv.ParseUint(value, 10, 32); err != nil {
			return nil, err
		} else {
			return uint32(uintValue), nil
		}

	case TypeUint64:
		if uintValue, err := strconv.ParseUint(value, 10, 64); err != nil {
			return nil, err
		} else {
			return uintValue, nil
		}

	case TypeFloat:
		if floatValue, err := strconv.ParseFloat(value, 32); err != nil {
			return nil, err
		} else {
			return float32(floatValue), nil
		}

	case TypeDouble:
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return nil, err
		} else {
			return floatValue, nil
		}

	case TypeString:
		return value, nil

	case TypeRaw:
		if bytesValue, err := base64.StdEncoding.DecodeString(value); err != nil {
			return nil, err
		} else {
			return bytesValue, nil
		}

	case TypeBool:
		return (value == "true"), nil

	default:
		return nil, fmt.Errorf("unsupported destination type '%s' for conversion", srcType)
	}
}
