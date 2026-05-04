package mock

import (
	"os"
	"testing"
)

func TestLoadConfig_Valid(t *testing.T) {
	config, err := LoadConfig("testdata/mock-config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(config.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(config.Nodes))
	}
	if config.Nodes[0].ID != "test-node-1" {
		t.Fatalf("expected node ID 'test-node-1', got %q", config.Nodes[0].ID)
	}
	if len(config.Mock.Messages) != 1 {
		t.Fatalf("expected 1 mock message, got %d", len(config.Mock.Messages))
	}
	if config.Mock.MessageInterval != "1s" {
		t.Fatalf("expected interval '1s', got %q", config.Mock.MessageInterval)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_EmptyNodes(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/empty.json"
	os.WriteFile(path, []byte(`{"nodes":[]}`), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for empty nodes")
	}
}

func TestLoadConfig_ParsesPortFieldData(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/typed.json"
	os.WriteFile(path, []byte(`{"nodes":[{"id":"n1","type":"app","data":{"name":"n1"}}],"mock":{"messages":[{"nodeID":"n1","handle":"input1","data":{"temperature":{"type":"number","format":"double","value":"25.5"}}}]}}`), 0644)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	field := config.Mock.Messages[0].Data["temperature"]
	if field.Value != "25.5" {
		t.Fatalf("expected value '25.5', got %q", field.Value)
	}
}
