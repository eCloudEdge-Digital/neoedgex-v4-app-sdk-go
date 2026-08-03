package contract

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// ConvertToTypedValue converts a native Go scalar value to the concrete Go
// type of destType, applying the cross-type conversion matrix shared by the
// Publish path and the schema-driven decode path: integer conversions are
// range-checked, float->integer truncates the fractional part, string->number
// parses the string, bool<->number map to 1/0 and non-zero/zero, raw ([]byte)
// only converts to raw.
//
// It returns an error when:
//
//   - destType is not a declarable schema type;
//   - value is nil, including a nil map, slice, pointer, channel, func or
//     interface;
//   - value is a time.Time — format it to a string first;
//   - value's Go type has no schema type (see GetDataType), which covers every
//     container, struct and defined byte type. All Go integer kinds are
//     accepted, the unsized int and uint and the 8-bit ones included;
//   - value is NaN or +/-Inf;
//   - CanConvertTo rejects the pair, notably string -> bool;
//   - the conversion itself fails: a number outside the destination's range,
//     or a string the destination cannot parse. The string path parses
//     integers strictly, so "1.5" -> int16 fails while the float 1.5 -> int16
//     succeeds as 1.
func ConvertToTypedValue(value any, destType DataType) (any, error) {
	if !destType.IsSupported() {
		return nil, fmt.Errorf("unsupported data type '%s'", destType)
	}
	if isNilAnyValue(value) {
		return nil, fmt.Errorf("nil value is not supported for conversion")
	}
	if _, ok := value.(time.Time); ok {
		return nil, fmt.Errorf("time.Time is not supported; format it to a string first (e.g. t.Format(time.RFC3339))")
	}

	srcType := GetDataType(value)
	if srcType == TypeUndefined {
		return nil, fmt.Errorf("unsupported value type '%T' for conversion", value)
	}
	if f, isFloat := asFloat(value); isFloat && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return nil, fmt.Errorf("cannot convert '%s' value '%v' to type '%s': NaN and Inf are not supported", srcType, f, destType)
	}
	if !srcType.CanConvertTo(destType) {
		return nil, fmt.Errorf("cannot convert from type '%s' to '%s'", srcType, destType)
	}

	switch destType {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64:
		return convertToInteger(value, srcType, destType)

	case TypeFloat, TypeDouble:
		return convertToFloat(value, srcType, destType)

	case TypeBool:
		switch v := value.(type) {
		case bool:
			return v, nil
		case int8:
			return v != 0, nil
		case int16:
			return v != 0, nil
		case int32:
			return v != 0, nil
		case int:
			return v != 0, nil
		case int64:
			return v != 0, nil
		case uint8:
			return v != 0, nil
		case uint16:
			return v != 0, nil
		case uint32:
			return v != 0, nil
		case uint:
			return v != 0, nil
		case uint64:
			return v != 0, nil
		case float32:
			return v != 0, nil
		case float64:
			return v != 0, nil
		}

	case TypeString:
		switch v := value.(type) {
		case string:
			return v, nil
		default:
			s, _, err := ConvertAnyValue(v)
			return s, err
		}

	case TypeRaw:
		if b, ok := value.([]byte); ok {
			return b, nil
		}
	}

	return nil, fmt.Errorf("internal error: unsupported conversion from '%s' to '%s'", srcType, destType)
}

func convertToInteger(value any, srcType, destType DataType) (any, error) {
	switch v := value.(type) {
	case string:
		typed, err := ConvertValueByType(v, destType)
		if err != nil {
			return nil, fmt.Errorf("cannot convert string value '%s' to type '%s': %v", v, destType, err)
		}
		return typed, nil

	case bool:
		var i int64
		if v {
			i = 1
		}
		if isUnsignedType(destType) {
			return castUnsigned(uint64(i), destType)
		}
		return castInteger(i, destType)

	case float32, float64:
		f, _ := asFloat(value)
		f = math.Trunc(f)
		if outOfIntegerRange(f, destType) {
			return nil, fmt.Errorf("cannot convert '%s' value '%v' to type '%s': value out of range", srcType, f, destType)
		}
		if isUnsignedType(destType) {
			return castUnsigned(uint64(f), destType)
		}
		return castInteger(int64(f), destType)

	case uint8, uint16, uint32, uint, uint64:
		u := asUint64(v)
		if isUnsignedType(destType) {
			if u > unsignedMax(destType) {
				return nil, fmt.Errorf("cannot convert '%s' value '%d' to type '%s': value out of range", srcType, u, destType)
			}
			return castUnsigned(u, destType)
		}
		if u > uint64(signedMax(destType)) {
			return nil, fmt.Errorf("cannot convert '%s' value '%d' to type '%s': value out of range", srcType, u, destType)
		}
		return castInteger(int64(u), destType)

	case int8, int16, int32, int, int64:
		i := asInt64(v)
		if isUnsignedType(destType) {
			if i < 0 || uint64(i) > unsignedMax(destType) {
				return nil, fmt.Errorf("cannot convert '%s' value '%d' to type '%s': value out of range", srcType, i, destType)
			}
			return castUnsigned(uint64(i), destType)
		}
		if i < signedMin(destType) || i > signedMax(destType) {
			return nil, fmt.Errorf("cannot convert '%s' value '%d' to type '%s': value out of range", srcType, i, destType)
		}
		return castInteger(i, destType)
	}

	return nil, fmt.Errorf("internal error: unsupported source type '%s' in integer conversion", srcType)
}

func convertToFloat(value any, srcType, destType DataType) (any, error) {
	var f float64
	switch v := value.(type) {
	case string:
		typed, err := ConvertValueByType(v, destType)
		if err != nil {
			return nil, fmt.Errorf("cannot convert string value '%s' to type '%s': %v", v, destType, err)
		}
		floatValue, _ := asFloat(typed)
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			return nil, fmt.Errorf("cannot convert string value '%s' to type '%s': NaN and Inf are not supported", v, destType)
		}
		return typed, nil
	case bool:
		if v {
			f = 1
		}
	case float32:
		if destType == TypeFloat {
			return v, nil
		}
		// Widening to double must restore the shortest-decimal value so the
		// result matches the pre-CBOR stringified path and ConvertAnyValue
		// display formatting (float32(25.34) -> 25.34, not 25.34000015258789).
		f, _ = strconv.ParseFloat(strconv.FormatFloat(float64(v), 'e', -1, 32), 64)
	case float64:
		f = v
	case int8:
		f = float64(v)
	case int16:
		f = float64(v)
	case int32:
		f = float64(v)
	case int:
		f = float64(v)
	case int64:
		f = float64(v)
	case uint8:
		f = float64(v)
	case uint16:
		f = float64(v)
	case uint32:
		f = float64(v)
	case uint:
		f = float64(v)
	case uint64:
		f = float64(v)
	default:
		return nil, fmt.Errorf("internal error: unsupported source type '%s' in float conversion", srcType)
	}

	if destType == TypeFloat {
		if math.Abs(f) > math.MaxFloat32 {
			return nil, fmt.Errorf("cannot convert '%s' value '%v' to type '%s': value out of range", srcType, value, destType)
		}
		return float32(f), nil
	}
	return f, nil
}

func castInteger(i int64, destType DataType) (any, error) {
	switch destType {
	case TypeInt16:
		return int16(i), nil
	case TypeInt32:
		return int32(i), nil
	case TypeInt64:
		return i, nil
	}
	return nil, fmt.Errorf("internal error: '%s' is not a signed integer type", destType)
}

func castUnsigned(u uint64, destType DataType) (any, error) {
	switch destType {
	case TypeUint16:
		return uint16(u), nil
	case TypeUint32:
		return uint32(u), nil
	case TypeUint64:
		return u, nil
	}
	return nil, fmt.Errorf("internal error: '%s' is not an unsigned integer type", destType)
}

func isUnsignedType(dataType DataType) bool {
	switch dataType {
	case TypeUint16, TypeUint32, TypeUint64:
		return true
	default:
		return false
	}
}

func signedMin(dataType DataType) int64 {
	switch dataType {
	case TypeInt16:
		return math.MinInt16
	case TypeInt32:
		return math.MinInt32
	default:
		return math.MinInt64
	}
}

func signedMax(dataType DataType) int64 {
	switch dataType {
	case TypeInt16:
		return math.MaxInt16
	case TypeInt32:
		return math.MaxInt32
	default:
		return math.MaxInt64
	}
}

func unsignedMax(dataType DataType) uint64 {
	switch dataType {
	case TypeUint16:
		return math.MaxUint16
	case TypeUint32:
		return math.MaxUint32
	default:
		return math.MaxUint64
	}
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	}
	return 0
}

func asUint64(v any) uint64 {
	switch t := v.(type) {
	case uint8:
		return uint64(t)
	case uint16:
		return uint64(t)
	case uint32:
		return uint64(t)
	case uint:
		return uint64(t)
	case uint64:
		return t
	}
	return 0
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float32:
		return float64(t), true
	case float64:
		return t, true
	}
	return 0, false
}

// outOfIntegerRange reports whether the (already truncated) float value falls
// outside destType. The upper bounds for int64/uint64 are exclusive because
// float64(MaxInt64/MaxUint64) rounds up to 2^63/2^64, which do not fit.
func outOfIntegerRange(f float64, destType DataType) bool {
	switch destType {
	case TypeInt16:
		return f < math.MinInt16 || f > math.MaxInt16
	case TypeInt32:
		return f < math.MinInt32 || f > math.MaxInt32
	case TypeInt64:
		return f < math.MinInt64 || f >= math.MaxInt64
	case TypeUint16:
		return f < 0 || f > math.MaxUint16
	case TypeUint32:
		return f < 0 || f > math.MaxUint32
	case TypeUint64:
		return f < 0 || f >= math.MaxUint64
	default:
		return true
	}
}
