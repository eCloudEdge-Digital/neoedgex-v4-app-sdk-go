package node

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

func TestDecodeIncomingDataDefaultModeParsesJson(t *testing.T) {
	decoded := decodeIncomingData(map[string]contract.PortFieldData{
		"obj": {Type: contract.TypeJsonObject, Value: `{"a":1}`},
		"arr": {Type: contract.TypeJsonArray, Value: `[1,2]`},
	}, false)

	obj, ok := decoded["obj"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any for jsonObject default mode, got %T", decoded["obj"])
	}
	if obj["a"] != float64(1) {
		t.Fatalf("unexpected parsed object: %#v", obj)
	}

	arr, ok := decoded["arr"].([]any)
	if !ok {
		t.Fatalf("expected []any for jsonArray default mode, got %T", decoded["arr"])
	}
	if !reflect.DeepEqual(arr, []any{float64(1), float64(2)}) {
		t.Fatalf("unexpected parsed array: %#v", arr)
	}
}

func TestDecodeIncomingDataRawModeReturnsRawMessage(t *testing.T) {
	objRaw := `{"id":9223372036854775807}`
	arrRaw := `[9223372036854775807]`

	decoded := decodeIncomingData(map[string]contract.PortFieldData{
		"obj": {Type: contract.TypeJsonObject, Value: objRaw},
		"arr": {Type: contract.TypeJsonArray, Value: arrRaw},
	}, true)

	objMsg, ok := decoded["obj"].(json.RawMessage)
	if !ok {
		t.Fatalf("expected json.RawMessage for jsonObject raw mode, got %T", decoded["obj"])
	}
	if !bytes.Equal(objMsg, []byte(objRaw)) {
		t.Fatalf("expected verbatim object bytes %q, got %q", objRaw, string(objMsg))
	}

	arrMsg, ok := decoded["arr"].(json.RawMessage)
	if !ok {
		t.Fatalf("expected json.RawMessage for jsonArray raw mode, got %T", decoded["arr"])
	}
	if !bytes.Equal(arrMsg, []byte(arrRaw)) {
		t.Fatalf("expected verbatim array bytes %q, got %q", arrRaw, string(arrMsg))
	}
}

func TestDecodeIncomingDataRawModeValidatesJson(t *testing.T) {
	cases := map[string]contract.PortFieldData{
		"null-object":     {Type: contract.TypeJsonObject, Value: "null"},
		"null-array":      {Type: contract.TypeJsonArray, Value: "null"},
		"malformed":       {Type: contract.TypeJsonObject, Value: `{"broken":}`},
		"array-as-object": {Type: contract.TypeJsonObject, Value: `[1,2]`},
		"object-as-array": {Type: contract.TypeJsonArray, Value: `{"a":1}`},
	}

	for name, field := range cases {
		decoded := decodeIncomingData(map[string]contract.PortFieldData{name: field}, true)
		if value, exists := decoded[name]; !exists || value != nil {
			t.Fatalf("raw mode should reject %s -> nil, got %#v", name, value)
		}
	}
}

func TestDecodeIncomingDataRawModeLeavesNonJsonUnaffected(t *testing.T) {
	decoded := decodeIncomingData(map[string]contract.PortFieldData{
		"num": {Type: contract.TypeInt64, Value: "42"},
		"str": {Type: contract.TypeString, Value: "hello"},
	}, true)

	if got, ok := decoded["num"].(int64); !ok || got != 42 {
		t.Fatalf("expected int64(42) for non-json field in raw mode, got %#v", decoded["num"])
	}
	if got, ok := decoded["str"].(string); !ok || got != "hello" {
		t.Fatalf("expected string for non-json field in raw mode, got %#v", decoded["str"])
	}
}
