package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"

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
		// 範例：只處理 handle 為 input1 的訊息，其他 handle 的訊息不處理。
		// (目前只有 input1 這個 handle 的訊息)
		if msg.Handle != "input1" {
			continue
		}

		// 範例：從 input1 的訊息中讀取 temperature 欄位
		var temperature int32
		if value, exists := msg.Data["temperature"]; !exists {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined in input schema"))
			continue
		} else if value == nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("no temperature value in message"))
			continue
		} else if castedValue, ok := value.(int32); !ok {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature is not defined as int32 in input schema"))
			continue
		} else {
			temperature = castedValue
		}

		// 範例：將讀取到的 temperature 值以 POST 請求發布出去。
		resp, err := httpClient.Post(httpEndpoint, "application/json", bytes.NewBuffer([]byte(fmt.Sprintf(`{"number": %d}`, temperature))))
		if err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("failed to send POST request: %w", err))
			continue
		}

		// 範例：將讀取到的 temperature 值以 number 欄位發布至 output1，並帶上 POST 請求的回應狀態碼 response_status 欄位。
		if err := ctx.Publish(map[string]any{
			"temperature":     temperature,
			"response_status": resp.StatusCode,
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
