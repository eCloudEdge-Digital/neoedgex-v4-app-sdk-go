package contract

import (
	"encoding/base64"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
)

// PortFieldData 描述節點執行後，輸出資料的型別與實際值。
type PortFieldData struct {
	Type   DataType   `json:"type"`
	Format DataFormat `json:"format"`
	Value  string     `json:"value"`
}

func NewPortFieldDataWithString(value string, format DataFormat) (*PortFieldData, error) {
	if !format.GetType().IsSupported() {
		return nil, fmt.Errorf("unsupported data format '%s'", format)
	}

	if _, err := ConvertValueByFormat(value, format); err != nil {
		return nil, fmt.Errorf("value '%s' is not compatible with format '%s': %v", value, format, err)
	}

	return &PortFieldData{
		Type:   format.GetType(),
		Format: format,
		Value:  value,
	}, nil
}

func NewPortFieldDataWithAny(anyValue any, destFormat DataFormat) (*PortFieldData, error) {
	if !destFormat.GetType().IsSupported() {
		return nil, fmt.Errorf("unsupported data format '%s'", destFormat)
	}
	if isNilAnyValue(anyValue) {
		return nil, fmt.Errorf("nil value is not supported for conversion")
	}

	value, srcFormat, err := ConvertAnyValue(anyValue)
	if err != nil {
		return nil, err
	} else if !srcFormat.CanConvertTo(destFormat) {
		return nil, fmt.Errorf("cannot convert from format '%s' to '%s'", srcFormat, destFormat)
	}

	return PortFieldData{
		Value:  value,
		Type:   srcFormat.GetType(),
		Format: srcFormat,
	}.ConvertTo(destFormat)
}

func NewEmptyField() *PortFieldData {
	return &PortFieldData{
		Type:   TypeUndefined,
		Format: FormatUndefined,
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
	return ConvertValueByFormat(v.Value, v.Format)
}

func (v PortFieldData) ConvertTo(destFormat DataFormat) (*PortFieldData, error) {
	if !v.Format.CanConvertTo(destFormat) {
		return nil, fmt.Errorf("cannot convert from format '%s' to '%s'", v.Format, destFormat)
	}

	srcFormat := v.Format
	if srcFormat == destFormat {
		return &v, nil
	}

	newValue, err := func() (string, error) {
		srcValue := v.Value

		switch destFormat {
		case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble:
			switch srcFormat {
			case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64:
				return convertIntFormatToNumberFormat(srcValue, srcFormat, destFormat)
			case FormatFloat, FormatDouble:
				return convertFloatFormatToNumberFormat(srcValue, srcFormat, destFormat)
			case FormatBool:
				return convertBoolFormatToNumberFormat(srcValue, destFormat)
			case FormatString:
				if rawValue, err := ConvertValueByFormat(srcValue, destFormat); err != nil {
					return "", fmt.Errorf("cannot convert string to number: %v", err)
				} else if value, format, err := ConvertAnyValue(rawValue); err != nil {
					return "", fmt.Errorf("cannot convert string to number: %v", err)
				} else if format != destFormat {
					return "", fmt.Errorf("cannot convert string to number: incompatible format '%s'", format)
				} else {
					return value, nil
				}
			}

		case FormatSecond, FormatMillisecond, FormatDatetime:
			switch srcFormat {
			case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble:
				return convertNumberFormatToTimeFormat(srcValue, srcFormat, destFormat)

			case FormatSecond, FormatMillisecond, FormatDatetime:
				return convertTimeFormatToTimeFormat(srcValue, srcFormat, destFormat)
			}

		case FormatBool:
			switch srcFormat {
			case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble:
				return convertNumberFormatToBoolFormat(srcValue, srcFormat)
			}

		case FormatString:
			switch srcFormat {
			case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble, FormatBool, FormatString:
				// No conversion needed
				return srcValue, nil
			}

		case FormatBase64:
			// Only raw to raw conversion is supported
			switch srcFormat {
			case FormatBase64:
				return srcValue, nil
			}
		}

		return "", fmt.Errorf("internal error: unsupported destination format '%s'", destFormat)
	}()

	if err != nil {
		return nil, err
	}

	return &PortFieldData{
		Type:   destFormat.GetType(),
		Format: destFormat,
		Value:  newValue,
	}, nil
}

// Any value to string value conversion, along with detected source format.
func ConvertAnyValue(anyValue any) (string, DataFormat, error) {
	if isNilAnyValue(anyValue) {
		return "", FormatUndefined, fmt.Errorf("nil value is not supported for conversion")
	}

	switch GetDataType(anyValue) {
	case TypeInt16, TypeInt32, TypeInt64:
		switch intValue := anyValue.(type) {
		case int16:
			return strconv.FormatInt(int64(intValue), 10), FormatInt16, nil
		case int32:
			return strconv.FormatInt(int64(intValue), 10), FormatInt32, nil
		case int64:
			return strconv.FormatInt(intValue, 10), FormatInt64, nil
		}

	case TypeUint16, TypeUint32, TypeUint64:
		switch uintValue := anyValue.(type) {
		case uint16:
			return strconv.FormatUint(uint64(uintValue), 10), FormatUint16, nil
		case uint32:
			return strconv.FormatUint(uint64(uintValue), 10), FormatUint32, nil
		case uint64:
			return strconv.FormatUint(uintValue, 10), FormatUint64, nil
		}

	case TypeFloat, TypeDouble:
		switch floatValue := anyValue.(type) {
		case float32:
			return strconv.FormatFloat(float64(floatValue), 'e', -1, 32), FormatFloat, nil
		case float64:
			return strconv.FormatFloat(floatValue, 'e', -1, 64), FormatDouble, nil
		}

	case TypeString:
		switch value := anyValue.(type) {
		case string:
			return value, FormatString, nil
		case time.Time:
			return value.Format(time.RFC3339), FormatDatetime, nil
		}

	case TypeBool:
		return strconv.FormatBool(anyValue.(bool)), FormatBool, nil

	case TypeRaw:
		return base64.StdEncoding.EncodeToString(anyValue.([]byte)), FormatBase64, nil
	}

	return "", FormatUndefined, fmt.Errorf("unsupported value type '%T' for conversion", anyValue)
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

// String value to any value conversion based on source format.
func ConvertValueByFormat(value string, srcFormat DataFormat) (any, error) {
	switch srcFormat {
	case FormatInt16:
		if intValue, err := strconv.ParseInt(value, 10, 16); err != nil {
			return nil, err
		} else {
			return int16(intValue), nil
		}

	case FormatInt32:
		if intValue, err := strconv.ParseInt(value, 10, 32); err != nil {
			return nil, err
		} else {
			return int32(intValue), nil
		}

	case FormatInt64:
		if intValue, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, err
		} else {
			return intValue, nil
		}

	case FormatUint16:
		if uintValue, err := strconv.ParseUint(value, 10, 16); err != nil {
			return nil, err
		} else {
			return uint16(uintValue), nil
		}

	case FormatUint32:
		if uintValue, err := strconv.ParseUint(value, 10, 32); err != nil {
			return nil, err
		} else {
			return uint32(uintValue), nil
		}

	case FormatUint64:
		if uintValue, err := strconv.ParseUint(value, 10, 64); err != nil {
			return nil, err
		} else {
			return uintValue, nil
		}

	case FormatFloat:
		if floatValue, err := strconv.ParseFloat(value, 32); err != nil {
			return nil, err
		} else {
			return float32(floatValue), nil
		}

	case FormatDouble:
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return nil, err
		} else {
			return floatValue, nil
		}

	case FormatString:
		return value, nil

	case FormatSecond:
		if intValue, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, err
		} else {
			return time.Unix(intValue, 0), nil
		}

	case FormatMillisecond:
		if intValue, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, err
		} else {
			return time.UnixMilli(intValue), nil
		}

	case FormatDatetime:
		if timeValue, err := time.Parse(time.RFC3339, value); err != nil {
			return nil, err
		} else {
			return timeValue, nil
		}

	case FormatBase64:
		if bytesValue, err := base64.StdEncoding.DecodeString(value); err != nil {
			return nil, err
		} else {
			return bytesValue, nil
		}

	case FormatBool:
		return (value == "true"), nil

	default:
		return nil, fmt.Errorf("unsupported destination format '%s' for conversion", srcFormat)
	}
}

// Convert values whose src format is int format to dest number format.
func convertIntFormatToNumberFormat(value string, src, dest DataFormat) (string, error) {
	if src == dest {
		return value, nil
	}

	switch dest {
	case FormatInt16, FormatInt32, FormatInt64:
		if intValue, err := strconv.ParseInt(value, 10, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to int format '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatInt(intValue, 10), nil
		}

	case FormatUint16, FormatUint32, FormatUint64:
		if uintValue, err := strconv.ParseUint(value, 10, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to uint format '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatUint(uintValue, 10), nil
		}

	case FormatFloat, FormatDouble:
		if floatValue, err := strconv.ParseFloat(value, getBitSize(dest)); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to float format '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatFloat(floatValue, 'e', -1, getBitSize(dest)), nil
		}

	default:
		return "", fmt.Errorf("internal error: unsupported destination format '%s' in int-to-int conversion", dest)
	}
}

// Convert values whose src format is float format to dest number format.
func convertFloatFormatToNumberFormat(value string, src, dest DataFormat) (string, error) {
	if src == dest {
		return value, nil
	}

	// float to float conversion
	switch dest {
	case FormatFloat:
		// float64 -> float32
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to float format '%s': %v", src, value, dest, err)
		} else {
			return strconv.FormatFloat(float64(float32(floatValue)), 'e', -1, 32), nil
		}
	case FormatDouble:
		// float32 -> float64
		return value, nil
	}

	// Parse float value and truncate fractional part
	floatValue, err := strconv.ParseFloat(value, getBitSize(src))
	if err != nil {
		return "", fmt.Errorf("cannot parse '%s' value '%s' as float for conversion to format '%s': %v", src, value, dest, err)
	} else if math.IsNaN(floatValue) {
		return "", fmt.Errorf("cannot convert '%s' value '%s' to format '%s': value is NaN", src, value, dest)
	} else if math.IsInf(floatValue, 0) {
		return "", fmt.Errorf("cannot convert '%s' value '%s' to format '%s': value is Inf", src, value, dest)
	} else {
		// Truncate float value
		floatValue = math.Trunc(floatValue)
	}

	// float to int/uint conversion
	var isOutOfRange bool
	switch dest {
	case FormatInt16, FormatInt32, FormatInt64:
		switch dest {
		case FormatInt16:
			isOutOfRange = (floatValue < math.MinInt16 || floatValue > math.MaxInt16)
		case FormatInt32:
			isOutOfRange = (floatValue < math.MinInt32 || floatValue > math.MaxInt32)
		case FormatInt64:
			isOutOfRange = (floatValue < math.MinInt64 || floatValue > math.MaxInt64)
		}

		if isOutOfRange {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to int format '%s': value out of range", src, value, dest)
		} else {
			return strconv.FormatInt(int64(floatValue), 10), nil
		}

	case FormatUint16, FormatUint32, FormatUint64:
		switch dest {
		case FormatUint16:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint16)
		case FormatUint32:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint32)
		case FormatUint64:
			isOutOfRange = (floatValue < 0 || floatValue > math.MaxUint64)
		}
		if isOutOfRange {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to uint format '%s': value out of range", src, value, dest)
		} else {
			return strconv.FormatUint(uint64(floatValue), 10), nil
		}

	default:
		return "", fmt.Errorf("internal error: unsupported destination format '%s' in int-to-int conversion", dest)
	}
}

// Convert values whose src format is bool format to dest number format.
func convertBoolFormatToNumberFormat(value string, dest DataFormat) (string, error) {
	isTrue := value == "true"
	switch dest {
	case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64:
		if isTrue {
			return "1", nil
		} else {
			return "0", nil
		}

	case FormatFloat, FormatDouble:
		var floatValue float64
		if isTrue {
			floatValue = 1.0
		} else {
			floatValue = 0.0
		}
		return strconv.FormatFloat(floatValue, 'e', -1, getBitSize(dest)), nil
	}

	return "", fmt.Errorf("internal error: unsupported destination format '%s' in bool-to-number conversion", dest)
}

// Convert values whose src format is number format to bool format.
func convertNumberFormatToBoolFormat(value string, src DataFormat) (string, error) {
	switch src {
	case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64:
		if value == "0" {
			return "false", nil
		} else {
			return "true", nil
		}

	case FormatFloat, FormatDouble:
		if floatValue, err := strconv.ParseFloat(value, 64); err != nil {
			return "", fmt.Errorf("cannot convert '%s' value '%s' to bool: %v", src, value, err)
		} else if floatValue == 0.0 {
			return "false", nil
		} else {
			return "true", nil
		}
	}

	return "", fmt.Errorf("internal error: unsupported source format '%s' in number-to-bool conversion", src)
}

// Convert values whose src format is number format to time format.
func convertNumberFormatToTimeFormat(value string, src, dest DataFormat) (string, error) {
	var datetime time.Time
	switch src {
	case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64:
		if int64ValueString, err := convertIntFormatToNumberFormat(value, src, FormatInt64); err != nil {
			return "", err
		} else if int64Value, err := strconv.ParseInt(int64ValueString, 10, 64); err != nil {
			return "", fmt.Errorf("internal error: cannot parse converted int64 value '%s' for time conversion: %v", int64ValueString, err)
		} else {
			datetime = convertInt64ToTime(int64Value)
		}

	case FormatFloat, FormatDouble:
		if int64ValueString, err := convertFloatFormatToNumberFormat(value, src, FormatInt64); err != nil {
			return "", err
		} else if int64Value, err := strconv.ParseInt(int64ValueString, 10, 64); err != nil {
			return "", fmt.Errorf("internal error: cannot parse converted int64 value '%s' for time conversion: %v", int64ValueString, err)
		} else {
			datetime = convertInt64ToTime(int64Value)
		}

	default:
		return "", fmt.Errorf("internal error: unsupported source format '%s' in number-to-time conversion", src)
	}

	switch dest {
	case FormatSecond:
		return strconv.FormatInt(datetime.Unix(), 10), nil
	case FormatMillisecond:
		return strconv.FormatInt(datetime.UnixMilli(), 10), nil
	case FormatDatetime:
		return datetime.Format(time.RFC3339), nil
	default:
		return "", fmt.Errorf("internal error: unsupported destination format '%s' in number-to-time conversion", dest)
	}
}

func convertTimeFormatToTimeFormat(value string, src, dest DataFormat) (string, error) {
	if src == dest {
		return value, nil
	}

	var datetime time.Time
	if anyValue, err := ConvertValueByFormat(value, src); err != nil {
		return "", fmt.Errorf("internal error: cannot parse time value '%s' of format '%s': %v", value, src, err)
	} else if casted, ok := anyValue.(time.Time); !ok {
		return "", fmt.Errorf("internal error: parsed value is not time.Time")
	} else {
		datetime = casted
	}

	switch dest {
	case FormatSecond:
		return strconv.FormatInt(datetime.Unix(), 10), nil

	case FormatMillisecond:
		return strconv.FormatInt(datetime.UnixMilli(), 10), nil

	case FormatDatetime:
		return datetime.Format(time.RFC3339), nil

	default:
		return "", fmt.Errorf("internal error: unsupported destination format '%s' in time-to-time conversion", dest)
	}
}

// Bit size
func getBitSize(format DataFormat) int {
	switch format {
	case FormatInt16, FormatUint16:
		return 16
	case FormatInt32, FormatUint32, FormatFloat:
		return 32
	case FormatInt64, FormatUint64, FormatDouble:
		return 64
	default:
		return -1
	}
}

func convertInt64ToTime(int64Value int64) time.Time {
	if int64Value >= 1e17 {
		// Nanoseconds
		return time.Unix(0, int64Value)
	} else if int64Value >= 1e14 {
		// Microseconds
		return time.UnixMicro(int64Value)
	} else if int64Value >= 1e11 {
		// Milliseconds
		return time.UnixMilli(int64Value)
	} else {
		// Seconds
		return time.Unix(int64Value, 0)
	}
}
