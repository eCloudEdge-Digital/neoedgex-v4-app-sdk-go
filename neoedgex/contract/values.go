package contract

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
)

// PortFieldData 描述節點執行後，輸出資料的型別與實際值。
type PortFieldData struct {
	Type  DataType `json:"type"`
	Value string   `json:"value"`
}

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

func NewPortFieldDataWithAny(anyValue any, destType DataType) (*PortFieldData, error) {
	if !destType.IsSupported() {
		return nil, fmt.Errorf("unsupported data type '%s'", destType)
	}
	if isNilAnyValue(anyValue) {
		return nil, fmt.Errorf("nil value is not supported for conversion")
	}

	value, srcType, err := ConvertAnyValue(anyValue)
	if err != nil {
		return nil, err
	} else if !srcType.CanConvertTo(destType) {
		return nil, fmt.Errorf("cannot convert from type '%s' to '%s'", srcType, destType)
	}

	return PortFieldData{
		Value: value,
		Type:  srcType,
	}.ConvertTo(destType)
}

func NewEmptyField() *PortFieldData {
	return &PortFieldData{
		Type: TypeUndefined,
	}
}

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

func (v PortFieldData) GetAnyValue() (any, error) {
	return ConvertValueByType(v.Value, v.Type)
}

func (v PortFieldData) ConvertTo(destType DataType) (*PortFieldData, error) {
	if !v.Type.CanConvertTo(destType) {
		return nil, fmt.Errorf("cannot convert from type '%s' to '%s'", v.Type, destType)
	}

	srcType := v.Type
	if srcType == destType {
		return &v, nil
	}

	newValue, err := func() (string, error) {
		srcValue := v.Value

		switch destType {
		case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble:
			switch srcType {
			case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64:
				return convertIntTypeToNumberType(srcValue, srcType, destType)
			case TypeFloat, TypeDouble:
				return convertFloatTypeToNumberType(srcValue, srcType, destType)
			case TypeBool:
				return convertBoolTypeToNumberType(srcValue, destType)
			case TypeString:
				if rawValue, err := ConvertValueByType(srcValue, destType); err != nil {
					return "", fmt.Errorf("cannot convert string to number: %v", err)
				} else if value, dataType, err := ConvertAnyValue(rawValue); err != nil {
					return "", fmt.Errorf("cannot convert string to number: %v", err)
				} else if dataType != destType {
					return "", fmt.Errorf("cannot convert string to number: incompatible type '%s'", dataType)
				} else {
					return value, nil
				}
			}

		case TypeBool:
			switch srcType {
			case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble:
				return convertNumberTypeToBoolType(srcValue, srcType)
			}

		case TypeString:
			switch srcType {
			case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble, TypeBool, TypeString:
				// No conversion needed
				return srcValue, nil
			}

		case TypeRaw:
			// Only raw to raw conversion is supported
			switch srcType {
			case TypeRaw:
				return srcValue, nil
			}
		}

		return "", fmt.Errorf("internal error: unsupported destination type '%s'", destType)
	}()

	if err != nil {
		return nil, err
	}

	return &PortFieldData{
		Type:  destType,
		Value: newValue,
	}, nil
}

// Any value to string value conversion, along with detected source type.
func ConvertAnyValue(anyValue any) (string, DataType, error) {
	if isNilAnyValue(anyValue) {
		return "", TypeUndefined, fmt.Errorf("nil value is not supported for conversion")
	}

	switch GetDataType(anyValue) {
	case TypeInt16, TypeInt32, TypeInt64:
		switch intValue := anyValue.(type) {
		case int16:
			return strconv.FormatInt(int64(intValue), 10), TypeInt16, nil
		case int32:
			return strconv.FormatInt(int64(intValue), 10), TypeInt32, nil
		case int64:
			return strconv.FormatInt(intValue, 10), TypeInt64, nil
		}

	case TypeUint16, TypeUint32, TypeUint64:
		switch uintValue := anyValue.(type) {
		case uint16:
			return strconv.FormatUint(uint64(uintValue), 10), TypeUint16, nil
		case uint32:
			return strconv.FormatUint(uint64(uintValue), 10), TypeUint32, nil
		case uint64:
			return strconv.FormatUint(uintValue, 10), TypeUint64, nil
		}

	case TypeFloat, TypeDouble:
		switch floatValue := anyValue.(type) {
		case float32:
			return strconv.FormatFloat(float64(floatValue), 'e', -1, 32), TypeFloat, nil
		case float64:
			return strconv.FormatFloat(floatValue, 'e', -1, 64), TypeDouble, nil
		}

	case TypeString:
		switch value := anyValue.(type) {
		case string:
			return value, TypeString, nil
		case time.Time:
			return value.Format(time.RFC3339), TypeString, nil
		}

	case TypeBool:
		return strconv.FormatBool(anyValue.(bool)), TypeBool, nil

	case TypeRaw:
		return base64.StdEncoding.EncodeToString(anyValue.([]byte)), TypeRaw, nil
	}

	return "", TypeUndefined, fmt.Errorf("unsupported value type '%T' for conversion", anyValue)
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

// String value to any value conversion based on source type.
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

	case TypeJsonObject:
		var obj map[string]any
		if err := json.Unmarshal([]byte(value), &obj); err != nil {
			return nil, err
		} else if obj == nil {
			return nil, fmt.Errorf("value '%s' is not a valid JSON object", value)
		} else {
			return obj, nil
		}

	case TypeJsonArray:
		var arr []any
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return nil, err
		} else if arr == nil {
			return nil, fmt.Errorf("value '%s' is not a valid JSON array", value)
		} else {
			return arr, nil
		}

	default:
		return nil, fmt.Errorf("unsupported destination type '%s' for conversion", srcType)
	}
}

// Convert values whose src type is int type to dest number type.
func convertIntTypeToNumberType(value string, src, dest DataType) (string, error) {
	if src == dest {
		return value, nil
	}

	switch dest {
	case TypeInt16, TypeInt32, TypeInt64:
		if intValue, err := strconv.ParseInt(value, 10, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to int type '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatInt(intValue, 10), nil
		}

	case TypeUint16, TypeUint32, TypeUint64:
		if uintValue, err := strconv.ParseUint(value, 10, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to uint type '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatUint(uintValue, 10), nil
		}

	case TypeFloat, TypeDouble:
		if floatValue, err := strconv.ParseFloat(value, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to float type '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatFloat(floatValue, 'e', -1, getBitSize(dest)), nil
		}

	default:
		return "", fmt.Errorf("internal error: unsupported destination type '%s' in int-to-number conversion", dest)
	}
}

// Convert values whose src type is float type to dest number type.
func convertFloatTypeToNumberType(value string, src, dest DataType) (string, error) {
	if src == dest {
		return value, nil
	}

	// float to float conversion
	switch dest {
	case TypeFloat:
		// float64 -> float32
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to float type '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatFloat(float64(float32(floatValue)), 'e', -1, 32), nil
		}
	case TypeDouble:
		// float32 -> float64
		return value, nil
	}

	// Parse float value and truncate fractional part
	floatValue, err := strconv.ParseFloat(value, getBitSize(src))
	if err != nil {
		return "", fmt.Errorf("cannot parse '%s' value '%s' as float for conversion to type '%s': %v", src, value, dest, err)
	} else if math.IsNaN(floatValue) {
		return "", fmt.Errorf("cannot convert '%s' value '%s' to type '%s': value is NaN", src, value, dest)
	} else if math.IsInf(floatValue, 0) {
		return "", fmt.Errorf("cannot convert '%s' value '%s' to type '%s': value is Inf", src, value, dest)
	} else {
		// Truncate float value
		floatValue = math.Trunc(floatValue)
	}

	// float to int/uint conversion
	var isOutOfRange bool
	switch dest {
	case TypeInt16, TypeInt32, TypeInt64:
		switch dest {
		case TypeInt16:
			isOutOfRange = (floatValue < math.MinInt16 || floatValue > math.MaxInt16)
		case TypeInt32:
			isOutOfRange = (floatValue < math.MinInt32 || floatValue > math.MaxInt32)
		case TypeInt64:
			isOutOfRange = (floatValue < math.MinInt64 || floatValue > math.MaxInt64)
		}

		if isOutOfRange {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to int type '%s': value out of range", src, value, dest)
		} else {
			return strconv.FormatInt(int64(floatValue), 10), nil
		}

	case TypeUint16, TypeUint32, TypeUint64:
		switch dest {
		case TypeUint16:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint16)
		case TypeUint32:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint32)
		case TypeUint64:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint64)
		}
		if isOutOfRange {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to uint type '%s': value out of range", src, value, dest)
		} else {
			return strconv.FormatUint(uint64(floatValue), 10), nil
		}

	default:
		return "", fmt.Errorf("internal error: unsupported destination type '%s' in float-to-number conversion", dest)
	}
}

// Convert values whose src type is bool type to dest number type.
func convertBoolTypeToNumberType(value string, dest DataType) (string, error) {
	isTrue := value == "true"
	switch dest {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64:
		if isTrue {
			return "1", nil
		} else {
			return "0", nil
		}

	case TypeFloat, TypeDouble:
		var floatValue float64
		if isTrue {
			floatValue = 1.0
		} else {
			floatValue = 0.0
		}
		return strconv.FormatFloat(floatValue, 'e', -1, getBitSize(dest)), nil
	}

	return "", fmt.Errorf("internal error: unsupported destination type '%s' in bool-to-number conversion", dest)
}

// Convert values whose src type is number type to bool type.
func convertNumberTypeToBoolType(value string, src DataType) (string, error) {
	switch src {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64:
		if value == "0" {
			return "false", nil
		} else {
			return "true", nil
		}

	case TypeFloat, TypeDouble:
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to bool: %v", src, value, err)
		} else if floatValue == 0.0 {
			return "false", nil
		} else {
			return "true", nil
		}
	}

	return "", fmt.Errorf("internal error: unsupported source type '%s' in number-to-bool conversion", src)
}

// Bit size
func getBitSize(dataType DataType) int {
	switch dataType {
	case TypeInt16, TypeUint16:
		return 16
	case TypeInt32, TypeUint32, TypeFloat:
		return 32
	case TypeInt64, TypeUint64, TypeDouble:
		return 64
	default:
		return -1
	}
}
