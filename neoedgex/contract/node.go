package contract

// Node is one node's platform configuration, as handed to the application.
type Node struct {
	// ID is the node's unique platform identifier.
	ID string `json:"id"`
	// Type is the node's platform node type.
	Type string   `json:"type"`
	Data NodeData `json:"data"`
}

// NodeData holds a node's display information, its port schemas and its
// user-configured settings.
type NodeData struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Inputs and Outputs map a port handle to that port's field schema.
	Inputs      map[string][]PortFieldSchema `json:"inputs"`
	Outputs     map[string][]PortFieldSchema `json:"outputs"`
	Application Application                  `json:"application"`
	// Settings holds the node's user-configured settings. It is decoded from
	// JSON, so every number arrives as float64 regardless of how it was
	// authored — a setting written as 5 type-asserts to float64, not to int.
	Settings map[string]any `json:"settings"`
}

// PortFieldSchema declares one field of a port: the key it travels under on
// the wire and the type its values are decoded into or converted to.
type PortFieldSchema struct {
	Key  string   `json:"key"`
	Type DataType `json:"type"`
}

// Application identifies the app the node is bound to.
type Application struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}
