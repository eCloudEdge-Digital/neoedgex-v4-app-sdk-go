package contract

import "testing"

// numericTypes lists every DataType the conversion matrix treats as a number.
var numericTypes = []DataType{
	TypeInt16, TypeInt32, TypeInt64,
	TypeUint16, TypeUint32, TypeUint64,
	TypeFloat, TypeDouble,
}

// TestCanConvertTo pins the type-based conversion-compatibility matrix exactly
// as implemented in format.go: every (source -> dest) cell is asserted for both
// the allowed and denied directions so the matrix cannot drift silently.
func TestCanConvertTo(t *testing.T) {
	tests := []struct {
		name string
		src  DataType
		dest DataType
		want bool
	}{
		// --- numeric dest: allowed from numeric, bool, string ---
		{"int32->int64", TypeInt32, TypeInt64, true},
		{"uint16->float", TypeUint16, TypeFloat, true},
		{"double->int16", TypeDouble, TypeInt16, true},
		{"bool->int32", TypeBool, TypeInt32, true},
		{"string->double", TypeString, TypeDouble, true},
		{"float->uint64", TypeFloat, TypeUint64, true},
		// numeric dest: denied from raw, json, undefined
		{"raw->int32 denied", TypeRaw, TypeInt32, false},
		{"jsonObject->float denied", TypeJsonObject, TypeFloat, false},
		{"jsonArray->double denied", TypeJsonArray, TypeDouble, false},
		{"undefined->int16 denied", TypeUndefined, TypeInt16, false},

		// --- bool dest: allowed from numeric and bool only ---
		{"int64->bool", TypeInt64, TypeBool, true},
		{"double->bool", TypeDouble, TypeBool, true},
		{"bool->bool", TypeBool, TypeBool, true},
		// bool dest: denied from string, raw, json, undefined
		{"string->bool denied", TypeString, TypeBool, false},
		{"raw->bool denied", TypeRaw, TypeBool, false},
		{"jsonObject->bool denied", TypeJsonObject, TypeBool, false},
		{"undefined->bool denied", TypeUndefined, TypeBool, false},

		// --- string dest: allowed from numeric, bool, string ---
		{"int32->string", TypeInt32, TypeString, true},
		{"bool->string", TypeBool, TypeString, true},
		{"string->string", TypeString, TypeString, true},
		{"float->string", TypeFloat, TypeString, true},
		// string dest: denied from raw, json, undefined
		{"raw->string denied", TypeRaw, TypeString, false},
		{"jsonArray->string denied", TypeJsonArray, TypeString, false},
		{"undefined->string denied", TypeUndefined, TypeString, false},

		// --- raw dest: allowed from raw only ---
		{"raw->raw", TypeRaw, TypeRaw, true},
		{"int32->raw denied", TypeInt32, TypeRaw, false},
		{"string->raw denied", TypeString, TypeRaw, false},
		{"bool->raw denied", TypeBool, TypeRaw, false},
		{"jsonObject->raw denied", TypeJsonObject, TypeRaw, false},
		{"undefined->raw denied", TypeUndefined, TypeRaw, false},

		// --- json dest: same-shape only; every cross-type (incl. object<->array) denied ---
		{"jsonObject->jsonObject", TypeJsonObject, TypeJsonObject, true},
		{"jsonArray->jsonArray", TypeJsonArray, TypeJsonArray, true},
		{"jsonObject->jsonArray denied", TypeJsonObject, TypeJsonArray, false},
		{"jsonArray->jsonObject denied", TypeJsonArray, TypeJsonObject, false},
		{"string->jsonObject denied", TypeString, TypeJsonObject, false},
		{"raw->jsonArray denied", TypeRaw, TypeJsonArray, false},
		{"int32->jsonObject denied", TypeInt32, TypeJsonObject, false},

		// --- undefined dest: default case, always denied ---
		{"undefined->undefined denied", TypeUndefined, TypeUndefined, false},
		{"int32->undefined denied", TypeInt32, TypeUndefined, false},
		{"raw->undefined denied", TypeRaw, TypeUndefined, false},
		{"jsonObject->undefined denied", TypeJsonObject, TypeUndefined, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.src.CanConvertTo(tc.dest); got != tc.want {
				t.Fatalf("%q.CanConvertTo(%q) = %v, want %v", tc.src, tc.dest, got, tc.want)
			}
		})
	}
}

// TestCanConvertToSelfForNumbers pins that every numeric type converts to
// itself (self -> self is allowed for numbers), covering all numeric cells.
func TestCanConvertToSelfForNumbers(t *testing.T) {
	for _, dt := range numericTypes {
		if !dt.CanConvertTo(dt) {
			t.Fatalf("expected %q.CanConvertTo(%q) == true (self)", dt, dt)
		}
	}
}

// TestCanConvertToNumberCrossProduct pins the full numeric<->numeric block:
// any numeric type converts to any other numeric type.
func TestCanConvertToNumberCrossProduct(t *testing.T) {
	for _, src := range numericTypes {
		for _, dest := range numericTypes {
			if !src.CanConvertTo(dest) {
				t.Fatalf("expected %q.CanConvertTo(%q) == true (numeric cross-product)", src, dest)
			}
		}
	}
}
