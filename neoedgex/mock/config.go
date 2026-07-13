package mock

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

// MockConfig 是 mock 模式的設定檔格式。
type MockConfig struct {
	Nodes []contract.Node `json:"nodes"`
	Mock  MockSection     `json:"mock"`
}

// MockSection 定義 mock 模式的行為設定。
type MockSection struct {
	MessageInterval string        `json:"messageInterval"`
	Messages        []MockMessage `json:"messages"`
}

// MockMessage 定義要注入到節點的假輸入訊息。
type MockMessage struct {
	NodeID string                            `json:"nodeID"`
	Handle string                            `json:"handle"`
	Data   map[string]contract.PortFieldData `json:"data"`
}

// LoadConfig 從檔案讀取並解析 mock 設定。
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
