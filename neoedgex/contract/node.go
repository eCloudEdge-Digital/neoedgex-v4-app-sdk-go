package contract

// Node 代表一個節點，包含其資料與類型。
type Node struct {
	ID   string   `json:"id"`
	Type string   `json:"type"`
	Data NodeData `json:"data"`
}

// NodeData 詳細描述節點本身的顯示名稱、I/O 結構與參數。
type NodeData struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Inputs      map[string][]PortFieldSchema `json:"inputs"`
	Outputs     map[string][]PortFieldSchema `json:"outputs"`
	Application Application                  `json:"application"`
	Settings    map[string]any               `json:"settings"`
}

// PortFieldSchema 描述 inputs / outputs 裡的鍵值與型別。
type PortFieldSchema struct {
	Key    string     `json:"key"`
	Type   DataType   `json:"type"`
	Format DataFormat `json:"format"`
}

// Application 描述節點綁定的 App 與版本資訊。
type Application struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}
