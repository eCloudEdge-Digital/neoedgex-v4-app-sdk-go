package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
)

type ExampleApp struct{}

func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	httpEndpoint := os.Getenv("HTTP_ENDPOINT")
	if httpEndpoint == "" {
		ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("HTTP_ENDPOINT environment variable is not set"))
		return
	}

	httpClient := http.Client{}

	for msg := range ctx.Messages() {
		// 範例：依 msg.Handle 將訊息分派到對應的 input 處理流程，
		// 各自準備不同的 API path 與 request body。
		var apiPath string
		var requestBody []byte
		switch msg.Handle {
		case "input1":
			// 範例：input1 攜帶 temperature (int32)。
			// msg.ToMap() 依 input schema 解碼成原生 Go 值；undefined
			//（key 不存在、上游沒給值、或轉換失敗）一律呈現為 nil。
			data := msg.ToMap()
			value, exists := data["temperature"]
			if !exists {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined in input1 schema"))
				continue
			}
			if value == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no temperature value in input1 message"))
				continue
			}
			temperature, ok := value.(int32)
			if !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined as int32 in input1 schema"))
				continue
			}
			apiPath = "/temperature"
			requestBody = []byte(fmt.Sprintf(`{"value": %d}`, temperature))

		case "input2":
			// 範例：input2 攜帶 running (bool)，改用 msg.ToStruct 解碼。
			// undefined 時 ToStruct 不會動到欄位；宣告成指標才能區分
			// 「false」與「沒有值」（undefined 時指標維持 nil）。
			var in struct {
				Running *bool `cbor:"running"`
			}
			if err := msg.ToStruct(&in); err != nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("failed to decode input2 message: %w", err))
				continue
			}
			if in.Running == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no running value in input2 message"))
				continue
			}
			apiPath = "/status"
			requestBody = []byte(fmt.Sprintf(`{"running": %t}`, *in.Running))

		case "input3":
			// 範例：input3 攜帶 message (string)
			data := msg.ToMap()
			message, ok := data["message"].(string)
			if !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no message string value in input3 message"))
				continue
			}
			apiPath = "/event"
			body, err := json.Marshal(map[string]string{"message": message})
			if err != nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("failed to encode event body: %w", err))
				continue
			}
			requestBody = body

		default:
			// 未在 schema 中定義的 handle，忽略即可
			continue
		}

		// 範例：依 input 來源送出對應的 HTTP POST 請求
		resp, err := httpClient.Post(httpEndpoint+apiPath, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("failed to POST %s: %w", apiPath, err))
			continue
		}

		// 範例：將剛剛呼叫的 API path 與回應狀態碼發布至 output1
		if err := ctx.Publish("output1", map[string]any{
			"api_path":        apiPath,
			"response_status": resp.StatusCode,
		}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}

func main() {
	// 範例：從 mock-config.json 讀取 mock 設定，並啟動 App 的 mock 模式。
	var mockConfig neoedgex.MockConfig
	if rawConfig, err := os.ReadFile("./cmd/mock-neoedgex/mock-config.json"); err != nil {
		log.Fatalf("Failed to read mock config file: %v", err)
	} else if err := json.Unmarshal(rawConfig, &mockConfig); err != nil {
		log.Fatalf("Failed to parse mock config: %v", err)
	}

	// 範例：啟動 App，並啟用 mock 模式
	app := neoedgex.New(&ExampleApp{})
	app.EnableMock(&mockConfig)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
