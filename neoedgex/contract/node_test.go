package contract

import (
	"encoding/json"
	"testing"
)

func TestNodeUnmarshalIgnoresLegacyPositionField(t *testing.T) {
	payload := []byte(`{
		"id":"node-1",
		"type":"custom",
		"position":{"x":12.5,"y":34.5},
		"data":{
			"name":"demo",
			"description":"test node",
			"inputs":{},
			"outputs":{},
			"application":{"key":"app","version":"1.0.0"},
			"settings":{}
		}
	}`)

	var node Node
	if err := json.Unmarshal(payload, &node); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if node.ID != "node-1" {
		t.Fatalf("unexpected node ID: %s", node.ID)
	}
	if node.Type != "custom" {
		t.Fatalf("unexpected node type: %s", node.Type)
	}
	if node.Data.Name != "demo" {
		t.Fatalf("unexpected node name: %s", node.Data.Name)
	}
}
