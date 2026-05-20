package contract

type DataFormat string

// TypeFormat 常用格式名稱常數。
const (
	FormatUndefined   DataFormat = ""
	FormatBool        DataFormat = "bool"
	FormatInt16       DataFormat = "int16"
	FormatInt32       DataFormat = "int32"
	FormatInt64       DataFormat = "int64"
	FormatSecond      DataFormat = "second"
	FormatMillisecond DataFormat = "millisecond"
	FormatUint16      DataFormat = "uint16"
	FormatUint32      DataFormat = "uint32"
	FormatUint64      DataFormat = "uint64"
	FormatFloat       DataFormat = "float"
	FormatDouble      DataFormat = "double"
	FormatString      DataFormat = "string"
	FormatDatetime    DataFormat = "datetime"
	FormatBase64      DataFormat = "base64"
	FormatJson        DataFormat = "json"
)

// TypeFormatMap 以 format 為 key、對應的 type 為 value，方便直接查詢 format 合法性與所屬型別。
var TypeFormatMap = map[DataFormat]DataType{
	FormatBool:        TypeBool,
	FormatInt16:       TypeInt16,
	FormatInt32:       TypeInt32,
	FormatInt64:       TypeInt64,
	FormatSecond:      TypeInt64,
	FormatMillisecond: TypeInt64,
	FormatUint16:      TypeUint16,
	FormatUint32:      TypeUint32,
	FormatUint64:      TypeUint64,
	FormatFloat:       TypeFloat,
	FormatDouble:      TypeDouble,
	FormatString:      TypeString,
	FormatDatetime:    TypeString,
	FormatBase64:      TypeRaw,
	FormatJson:        TypeString,
}

func (dataFormat DataFormat) GetType() DataType {
	dataType, exists := TypeFormatMap[dataFormat]
	if exists {
		return dataType
	} else {
		return TypeUndefined
	}
}

func (dataFormat DataFormat) CanConvertTo(destFormat DataFormat) bool {
	srcFormat := dataFormat

	switch destFormat {
	case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble:
		switch srcFormat {
		case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble, FormatBool, FormatString:
			return true
		default:
			return false
		}

	case FormatSecond, FormatMillisecond, FormatDatetime:
		switch srcFormat {
		case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble, FormatSecond, FormatMillisecond, FormatDatetime:
			return true
		default:
			return false
		}

	case FormatBool:
		switch srcFormat {
		case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble, FormatBool:
			return true
		default:
			return false
		}

	case FormatString:
		switch srcFormat {
		case FormatInt16, FormatInt32, FormatInt64, FormatUint16, FormatUint32, FormatUint64, FormatFloat, FormatDouble, FormatBool, FormatString:
			return true
		default:
			return false
		}

	case FormatBase64:
		switch srcFormat {
		case FormatBase64:
			return true
		default:
			return false
		}

	case FormatJson:
		switch srcFormat {
		case FormatJson:
			return true
		default:
			return false
		}

	default:
		return false
	}
}
