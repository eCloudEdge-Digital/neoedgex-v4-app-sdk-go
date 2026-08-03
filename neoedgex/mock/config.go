// Package mock provides the configuration format for running a NeoEdgeX node
// application locally, without the platform.
//
// A mock configuration file supplies the node configurations the SDK would
// otherwise read from the platform, plus a list of fake input messages to
// inject on a timer. Load it with LoadConfig and hand it to
// (*neoedgex.App).EnableMock; the App then runs against an in-memory
// transport, logging whatever the handler publishes instead of sending it to a
// broker.
//
//	config, err := mock.LoadConfig("./mock-config.json")
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := neoedgex.New(&MyApp{}).EnableMock(config).Run(); err != nil {
//		log.Fatal(err)
//	}
//
// Mock mode is for development only; remove the EnableMock call before
// deployment.
package mock

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// MockConfig is the mock configuration file format.
type MockConfig struct {
	// Nodes stands in for the platform's node configuration. At least one node
	// is required.
	Nodes []contract.Node `json:"nodes"`
	Mock  MockSection     `json:"mock"`
}

// MockSection configures message injection. Messages are injected one at a
// time, cycling through the list, starting shortly after the app comes up.
type MockSection struct {
	// MessageInterval is a Go duration string such as "3s" or "500ms". A
	// value that is empty, unparseable or not positive is ignored without any
	// error and falls back to 3s.
	MessageInterval string        `json:"messageInterval"`
	Messages        []MockMessage `json:"messages"`
}

// MockMessage is one fake input message to inject into a node.
type MockMessage struct {
	// NodeID must match the ID of one of the configured nodes.
	NodeID string `json:"nodeID"`
	// Handle is the input port to deliver the message on.
	Handle string `json:"handle"`
	// Data holds each field in stringified type/value form rather than as a
	// native JSON value, for example {"type":"double","value":"2.534e+01"}.
	// The SDK converts each entry to its native Go value at injection time, so
	// the handler sees what it would see in production.
	Data map[string]contract.PortFieldData `json:"data"`
}

// LoadConfig reads and parses a mock configuration file. It returns an error
// if the file cannot be read or parsed, or if it declares no nodes.
func LoadConfig(path string) (*MockConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mock config: %w", err)
	}

	var config MockConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse mock config: %w", err)
	}

	if len(config.Nodes) == 0 {
		return nil, fmt.Errorf("mock config: nodes must not be empty")
	}

	return &config, nil
}
