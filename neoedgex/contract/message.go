package contract

type NeoFlowMessage struct {
	SourceNodeID string                   `json:"source"`
	Timestamp    string                   `json:"timestamp"`
	Data         map[string]PortFieldData `json:"data"`
}

type Message struct {
	Handle    string
	Data      map[string]any
	Source    string
	Timestamp string
}
