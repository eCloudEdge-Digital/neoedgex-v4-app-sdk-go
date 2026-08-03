package contract

// CanConvertTo reports whether a value of this type may be converted to
// destType. The rules are:
//
//   - the numeric types and bool convert to any numeric type, to bool and to
//     string;
//   - string converts to any numeric type and to string, but not to bool —
//     "true" is rejected, not parsed;
//   - raw ([]byte) converts only to raw, and nothing but raw converts to raw;
//   - TypeUndefined is accepted as neither source nor destination.
//
// A true result only means the pair is allowed. The conversion itself can
// still fail on range, parsing or NaN/Inf; see ConvertToTypedValue.
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

	default:
		return false
	}
}
