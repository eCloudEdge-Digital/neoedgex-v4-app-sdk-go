package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex"
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
			// 範例：input1 攜帶 temperature (double)
			var temperature float64
			if value, exists := msg.Data["temperature"]; !exists {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined in input1 schema"))
				continue
			} else if value == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no temperature value in input1 message"))
				continue
			} else if castedValue, ok := value.(float64); !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined as double in input1 schema"))
				continue
			} else {
				temperature = castedValue
			}
			apiPath = "/temperature"
			requestBody = []byte(fmt.Sprintf(`{"value": %v}`, temperature))

		case "input2":
			// 範例：input2 攜帶 running (bool)
			var running bool
			if value, exists := msg.Data["running"]; !exists {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("running is not defined in input2 schema"))
				continue
			} else if value == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no running value in input2 message"))
				continue
			} else if castedValue, ok := value.(bool); !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("running is not defined as bool in input2 schema"))
				continue
			} else {
				running = castedValue
			}
			apiPath = "/status"
			requestBody = []byte(fmt.Sprintf(`{"running": %t}`, running))

		case "input3":
			// 範例：input3 攜帶 payload (format=json)，handler 拿到的是 raw JSON 字串，
			// 由 app 自行決定怎麼 unmarshal、是否用 json.Number 保留大整數精度。
			var rawPayload string
			if value, exists := msg.Data["payload"]; !exists {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("payload is not defined in input3 schema"))
				continue
			} else if value == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no payload value in input3 message"))
				continue
			} else if castedValue, ok := value.(string); !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("payload is not delivered as string (format=json), got %T", value))
				continue
			} else {
				rawPayload = castedValue
			}

			// 範例：用 json.Decoder.UseNumber() 讓 nested 數字保留為 json.Number，
			// 才能還原超過 float64 精度（2^53）的 int64 / uint64
			var payload map[string]any
			dec := json.NewDecoder(strings.NewReader(rawPayload))
			dec.UseNumber()
			if err := dec.Decode(&payload); err != nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("payload is not a JSON object: %w", err))
				continue
			}

			// 範例：取出 payload.id（json.Number）→ int64
			idNumber, ok := payload["id"].(json.Number)
			if !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("payload 'id' is not a number"))
				continue
			}
			id, err := idNumber.Int64()
			if err != nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("payload 'id' is not an int64: %w", err))
				continue
			}

			apiPath = fmt.Sprintf("/payload/%d", id)
			requestBody = []byte(rawPayload)

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
			"response_status": int32(resp.StatusCode),
		}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}

func main() {
	// 範例：啟動 App，並使用 ExampleApp 作為 NodeHandler。
	if err := neoedgex.New(&ExampleApp{}).Run(); err != nil {
		log.Fatal(err)
	}
}
