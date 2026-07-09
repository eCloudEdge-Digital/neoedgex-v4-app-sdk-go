package contract

// CanConvertTo 判斷來源型別是否可轉換為目標型別，維持與過往 format-based 轉換矩陣相同的允許/拒絕規則。
func (dataType DataType) CanConvertTo(destType DataType) bool {
	srcType := dataType

	switch destType {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble:
		switch srcType {
		case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble, TypeBool, TypeString:
			return true
		default:
			return false
		}

	case TypeBool:
		switch srcType {
		case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble, TypeBool:
			return true
		default:
			return false
		}

	case TypeString:
		switch srcType {
		case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble, TypeBool, TypeString:
			return true
		default:
			return false
		}

	case TypeRaw:
		switch srcType {
		case TypeRaw:
			return true
		default:
			return false
		}

	case TypeJsonObject:
		// json fields accept their own shape only (no cross-type conversion).
		switch srcType {
		case TypeJsonObject:
			return true
		default:
			return false
		}

	case TypeJsonArray:
		switch srcType {
		case TypeJsonArray:
			return true
		default:
			return false
		}

	default:
		return false
	}
}
