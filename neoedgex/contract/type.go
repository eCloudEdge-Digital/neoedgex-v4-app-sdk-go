package contract

// DataType is the tag type a field is declared with in a node's input or
// output port schema. It fixes the concrete Go type the SDK decodes an inbound
// field into and converts an outbound value to.
type DataType string

// The declarable schema types, with the Go type each one maps to.
const (
	// TypeUndefined is the zero value and cannot be declared in a schema:
	// GetDataType returns it for a value with no schema type, and every
	// conversion rejects it as source and as destination.
	TypeUndefined DataType = ""

	TypeBool   DataType = "bool"   // bool
	TypeInt16  DataType = "int16"  // int16
	TypeInt32  DataType = "int32"  // int32
	TypeInt64  DataType = "int64"  // int64
	TypeUint16 DataType = "uint16" // uint16
	TypeUint32 DataType = "uint32" // uint32
	TypeUint64 DataType = "uint64" // uint64
	TypeFloat  DataType = "float"  // float32
	TypeDouble DataType = "double" // float64
	TypeString DataType = "string" // string
	TypeRaw    DataType = "raw"    // []byte
)

// SupportedTypes is the set of declarable schema types, backing IsSupported.
//
// It is exported and mutable: writing to it changes type validation for the
// whole process, and a write races with any concurrent IsSupported call. Treat
// it as read-only.
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

// GetDataType reports the schema type of a native Go value.
//
// Every Go integer kind is accepted, mapped to the narrowest tag type that
// holds its whole range: int8 and int16 to int16, uint8 and uint16 to uint16,
// and the unsized int and uint to int64 and uint64. bool, float32, float64,
// string and []byte map to their own tag type. Adding an integer kind here
// only widens what the conversion path accepts as INPUT; the tag universe
// stays the 11 scalars.
//
// Everything else yields TypeUndefined — nil, time.Time, and any struct, map
// or slice other than []byte. []byte keeps mapping to raw: only the scalar
// uint8 is an integer here, the byte slice is not.
func GetDataType(anyValue any) DataType {
	switch anyValue.(type) {
	case bool:
		return TypeBool
	case int8, int16:
		return TypeInt16
	case int32:
		return TypeInt32
	case int, int64:
		return TypeInt64
	case uint8, uint16:
		return TypeUint16
	case uint32:
		return TypeUint32
	case uint, uint64:
		return TypeUint64
	case float32:
		return TypeFloat
	case float64:
		return TypeDouble
	case string:
		return TypeString
	case []byte:
		return TypeRaw
	default:
		return TypeUndefined
	}
}

// IsNumber reports whether the type is one of the integer or float types.
// bool, string, raw and TypeUndefined are not numbers.
func (dataType DataType) IsNumber() bool {
	switch dataType {
	case TypeInt16, TypeInt32, TypeInt64, TypeUint16, TypeUint32, TypeUint64, TypeFloat, TypeDouble:
		return true
	default:
		return false
	}
}

// IsSupported reports whether the type may be declared in a port schema, that
// is whether it is a member of SupportedTypes. TypeUndefined is not.
func (dataType DataType) IsSupported() bool {
	_, exists := SupportedTypes[dataType]
	return exists
}
