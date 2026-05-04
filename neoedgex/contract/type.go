package contract

import "time"

// 基礎 type 名稱常數。
type DataType string

const (
	TypeUndefined DataType = ""
	TypeBool      DataType = "bool"
	TypeInt16     DataType = "int16"
	TypeInt32     DataType = "int32"
	TypeInt64     DataType = "int64"
	TypeUint16    DataType = "uint16"
	TypeUint32    DataType = "uint32"
	TypeUint64    DataType = "uint64"
	TypeFloat     DataType = "float"
	TypeDouble    DataType = "double"
	TypeString    DataType = "string"
	TypeRaw       DataType = "raw"
)

// SupportedTypes 用於快速驗證 type 是否受支援。
var SupportedTypes = map[DataType]struct{}{
	TypeBool:   {},
	TypeInt16:  {},
	TypeInt32:  {},
	TypeInt64:  {},
	TypeUint16: {},
	TypeUint32: {},
	TypeUint64: {},
	TypeFloat:  {},
	TypeDouble: {},
	TypeString: {},
	TypeRaw:    {},
}

func GetDataType(anyValue any) DataType {
	switch anyValue.(type) {
	case bool:
		return TypeBool
	case int16:
		return TypeInt16
	case int32:
		return TypeInt32
	case int64:
		return TypeInt64
	case uint16:
		return TypeUint16
	case uint32:
		return TypeUint32
	case uint64:
		return TypeUint64
	case float32:
		return TypeFloat
	case float64:
		return TypeDouble
	case string, time.Time:
		return TypeString
	case []byte:
		return TypeRaw
	default:
		return TypeUndefined
	}
}

func (dataType DataType) IsNumber() bool {
	switch dataType {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble:
		return true
	default:
		return false
	}
}

func (dataType DataType) IsSupported() bool {
	_, exists := SupportedTypes[dataType]
	return exists
}
