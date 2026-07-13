# NeoEdgeX App SDK v4 第三方開發指南

> 最新版本變更見[文末版本變更紀錄](#版本變更紀錄)。

## 這個 SDK 是什麼

NeoEdgeX App SDK v4 是用來開發 NeoEdgeX 節點應用程式的 Go SDK，支援 driver、protocol adapter、forwarder、processor 等節點類型。SDK 提供統一的執行模型：

- 透過 `ctx.Messages()` 接收來自 NeoFlow 的上游訊息
- 透過 `ctx.NodeConfig()` 讀取節點設定
- 透過 `ctx.Publish(...)` 發布下游輸出
- 透過 `ctx.ReportError(...)` 回報執行錯誤

節點生命週期、訊息傳輸、心跳、狀態回報、關閉流程，以及 mock 模式，由 SDK 統一處理。

## 公開可依賴邊界

第三方應用程式只應依賴以下公開套件：

- `neoedgex`
- `neoedgex/mock`
- `neoedgex/testutil`（只建議用在單元測試）

本指南涵蓋的公開入口：

- `neoedgex.New(handler)`
- `(*App).Run()`
- `(*App).EnableMock(...)`
- `(*App).DisableSDKLog()`
- `(*App).UseRawJson()`
- `neoedgex.LoadMockConfig(...)`
- `neoedgex.NodeHandler`
- `neoedgex.NodeEnv`
- `neoedgex.Node`
- `neoedgex.Message`
- `neoedgex.Logger`
- `neoedgex.ErrorCode`
- `mock.LoadConfig(...)`
- `testutil.MockNodeEnv`

repo 裡可見的其他路徑不屬於對外契約，不應依賴。

## 套件依賴規則

正式 app 請只 import 這些套件：

- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex`
- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock`

測試程式可額外 import：

- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/testutil`

即使 repo 裡看得到其他路徑，也不要 import 內部或不穩定的實作套件。

## 最小可用範例

實作 `neoedgex.NodeHandler`，透過 `neoedgex.New(...).Run()` 啟動。

```go
package main

import (
	"log"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
)

type ExampleApp struct{}

func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for range ctx.Messages() {
		if err := ctx.Publish("output1", map[string]any{
			"hello": "world",
		}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}

func main() {
	if err := neoedgex.New(&ExampleApp{}).Run(); err != nil {
		log.Fatal(err)
	}
}
```

停用 SDK 內部 log，在 `Run()` 前呼叫 `DisableSDKLog()`：

```go
app := neoedgex.New(&ExampleApp{}).DisableSDKLog()
if err := app.Run(); err != nil {
	log.Fatal(err)
}
```

預設情況下，handler 自 `Message.Data` 讀到的 `jsonObject` / `jsonArray` 欄位會在送進 handler 前先解碼成 `map[string]any` / `[]any`。Go 的 JSON decoder 會把所有 JSON 數字解成 `float64`，因此大於 2^53 的整數會遺失精度。若 app 必須保留原始 bytes（例如把 payload 原樣往下游轉送的 forwarder），可在 `Run()` 前呼叫 `UseRawJson()`：

```go
app := neoedgex.New(&ExampleApp{}).UseRawJson()
if err := app.Run(); err != nil {
	log.Fatal(err)
}
```

呼叫 `UseRawJson()` 後，handler 自 `Message.Data` 讀到的 `jsonObject` / `jsonArray` 欄位會以 `json.RawMessage`（驗證過的原始 bytes）交付，而非 `map[string]any` / `[]any`。驗證行為不變：`null`、格式錯誤的 JSON、以及型別不符（array 給 `jsonObject`、object 給 `jsonArray`）仍會被拒絕，只有交付的 Go 型別不同。由於保留了原始 bytes，大整數可維持完整精度，往下游 `Publish` 重新 marshal 時也會逐字（含巢狀數字）重現原值。非 JSON 型別不受影響。

## 如何設定 Custom App

SDK 從固定根路徑 `/opt/neoedgex` 讀取平台掛載的檔案：

- `/opt/neoedgex/config/messenger.json`：平台產生的 ACL credentials，SDK 用以訂閱和發送 NeoFlow Message
- `/opt/neoedgex/config/config.json`：平台下發的節點設定，SDK 透過 `ctx.NodeConfig()` 提供給 handler

Custom App node 的設定來自 `ctx.NodeConfig()` 回傳的節點定義，分三個區塊：

1. `config.data.inputs`
2. `config.data.outputs`
3. `config.data.settings`

### Input Schema

input schema 定義在 `config.data.inputs`：

```json
{
  "inputs": {
    "input1": [
      { "key": "temperature", "type": "double" }
    ],
    "input2": [
      { "key": "running", "type": "bool" }
    ],
    "input3": [
      { "key": "capturedAt", "type": "string" }
    ]
  }
}
```
<img width="200" height="102" src="./assets/node-input-config.png" />

可以同時定義多個 input handle，每個 handle 各自帶獨立的欄位 schema；handler 透過 `msg.Handle` 判斷訊息來自哪一個 input。

input schema 描述 handler 從 `ctx.Messages()` 讀到的欄位。每個欄位包含：

- `key`：出現在 `msg.Data` 裡的欄位名稱
- `type`：欄位的資料型態，完整決定解碼後的 Go 值

實際的 Go 型別由 SDK 依 `type` 解碼決定。

### Output Schema

output schema 定義在 `config.data.outputs`：

```json
{
  "outputs": {
    "output1": [
      { "key": "power", "type": "double" },
      { "key": "status", "type": "string" }
    ]
  }
}
```

可以同時定義多個 output handle，每個 handle 各自帶獨立的欄位 schema；handler 透過 `ctx.Publish(handle, data)` 的第一個引數選擇要送往哪一個 output。

這份 schema 決定 `ctx.Publish(handle, map[string]any{...})` 的驗證與轉換行為：

- publish 的 map key 需和該 `handle` 所定義的 key 一致
- destination `type` 決定可接受哪些 Go 值，以及如何轉換
- schema 中被省略或為 `nil` 的欄位，SDK 補空欄位（`type=""`、`value=""`）

新增、刪除或改名欄位後，需同步更新 `ctx.Publish(...)` 的呼叫。

<img width="200" height="87" src="./assets/node-output-config.png" />

### Settings

執行設定定義在 `config.data.settings`，對應的 `docker-compose.yml` 欄位如下：

- `containerName`：同時影響 service key 與 `container_name`
- `image`：service 的 `image`
- `envVars`：service 的 `environment`
- `files`：`volumes` 下的額外 bind mounts
- `devices`：service 的 `devices`
- `gpu.enabled=true`：為 service 加上 `gpus`
- `portBindings`：service 的 `ports`

以下欄位屬於 node settings，不直接出現在 compose service 中：

- `credentials`：`neoedgex-agent` 用這組 credential 登入 docker registry，拉取 `image` 指定的 image

對應的 `docker-compose.yml` 範例：

```yaml
name: neoedgex
services:
  7719d4f0cc984dd6:
    container_name: 7719d4f0cc984dd6
    depends_on:
      neoedgex-messenger:
        condition: service_started
        required: true
    devices:
      - source: /dev/ttyUSB0
        target: /dev/ttyUSB0
        permissions: rw
    environment:
      a: b
    gpus:
      - capabilities:
          - gpu
        driver: nvidia
        count: -1
    image: 192.168.64.202:5001/busybox:stable
    networks:
      neoedgex-network: null
    restart: always
    ports:
      - target: 80
        published: "8080"
        protocol: tcp
    volumes:
      - type: bind
        source: ...
        target: /opt/neoedgex/config
        read_only: true
      - type: bind
        source: ...
        target: /var/myfile/ca-copy.crt
        read_only: true
```

### 傳遞 App Config

app 從環境變數或掛載檔案讀取業務設定；SDK 負責把這些內容帶進容器，但不解析 app 的 business config。

#### 模式 A：用固定 key 的 env var 當 config

適合：

- 小型設定
- 單一字串或 JSON blob
- 少量、容易直接放進 environment 的設定

在 `settings.envVars` 定義固定 key：

```json
"envVars": [
  {
    "key": "HTTPCLIENT_CONFIG_JSON",
    "value": "{\"endpoint\":\"https://api.example.com/ingest\",\"method\":\"POST\"}",
    "note": "app business config"
  }
]
```

app 讀取固定 key：

```go
raw := os.Getenv("HTTPCLIENT_CONFIG_JSON")
if raw == "" {
    return fmt.Errorf("HTTPCLIENT_CONFIG_JSON is required")
}
```

也可以拆成多個獨立的 env var：

```json
"envVars": [
  {
    "key": "HTTPCLIENT_ENDPOINT",
    "value": "https://api.example.com/ingest",
    "note": "HTTP endpoint"
  },
  {
    "key": "HTTPCLIENT_METHOD",
    "value": "POST",
    "note": "HTTP method"
  },
  {
    "key": "HTTPCLIENT_TIMEOUT_SECONDS",
    "value": "10",
    "note": "request timeout"
  }
]
```

```go
endpoint := os.Getenv("HTTPCLIENT_ENDPOINT")
method := os.Getenv("HTTPCLIENT_METHOD")
timeoutRaw := os.Getenv("HTTPCLIENT_TIMEOUT_SECONDS")
```

#### 模式 B：用固定路徑的檔案當 config

適合：

- 較大的 JSON / YAML
- 結構化設定
- 憑證、key、secret file
- 需要以檔案形式人工替換或掛載的內容

在 `settings.files` 宣告固定路徑：

```json
"files": [
  {
    "uuid": "app-config-file",
    "path": "/myconfig.json",
    "secret": "false"
  }
]
```

app 直接讀該路徑：

```go
payload, err := os.ReadFile("/myconfig.json")
if err != nil {
    return fmt.Errorf("read /myconfig.json: %w", err)
}
```

#### 選擇 env var 或 file

- 小型、單值、少量 JSON：優先用 env var
- 較大或結構化 config：優先用 file
- 憑證、key、secret file：通常用 file
- 同時支援 env 與 file 時，在 app 內明確定義固定優先順序，例如 env 先、file 後

SDK 不決定這個優先順序；這是 app 自身的 contract，應在 app 文件中明確說明。

## 訊息模型

### 術語

- `node`：一個被這個 app 匹配到的 NeoEdgeX 節點設定
- `handle`：input 或 output port 名稱，例如 `input1`、`output1`
- `mock mode`：SDK 的本地模擬模式，不需要真實平台就能注入假訊息
<img width="200" height="61"  src="./assets/node-diagram.png" />

### NodeEnv 與 Message

每個 handler 會收到一個 `neoedgex.NodeEnv`。

`NodeEnv` 提供：

- `NodeConfig()`：原始節點設定，含 `Data.Settings`、`Data.Inputs`、`Data.Outputs`
- `Messages()`：接收進來的 `neoedgex.Message`
- `Context()`：這個 node 的生命週期 context，用於 HTTP、DB、gRPC、worker loop 等呼叫
- `Logger()`：node-scoped logger
- `Publish(handle string, data map[string]any)`：送出到指定的 output handle
- `ReportError(code, err)`：回報平台可見的 node error
- `Stop()`：要求 SDK 停止這個 node，用於 handler 遭遇無法繼續的 fatal error

`neoedgex.Message` 包含：

- `Handle`：觸發此訊息的 input handle 名稱
- `Data`：`map[string]any`，已從 NeoFlow 欄位解碼的 Go 原生值
- `Source`：來源節點 ID
- `Timestamp`：上游 publish 時間，RFC3339 格式；若上游 payload 未提供則為空字串

### 讀取 Input 值

`msg.Data` 已包含 Go 原生值。讀取時需先依 `msg.Handle` 判斷訊息來自哪一個 input，再判斷欄位 key 是否存在，以及 value 是否為 `nil`。

以一則含 scalar、`jsonObject` 與 `jsonArray` 欄位的 input payload 為例，`msg.Data` 中各欄位的 Go 值如下（`jsonObject` 為 `map[string]any`、`jsonArray` 為 `[]any`；呼叫 `UseRawJson()` 後兩者皆為 `json.RawMessage`）：

```go
neoedgex.Message{
	Handle:    "input1",
	Source:    "upstream-node",
	Timestamp: "2026-03-31T09:10:11Z",
	Data: map[string]any{
		"temperature": 25.5,
		"payload":     map[string]any{"unit": "C"},
		"samples":     []any{1.0, 2.0, 3.0},
	},
}
```

讀取時對每個欄位套用相同的防禦式流程：先判斷 key 是否存在，再判斷 value 是否為 `nil`，最後做型別斷言。以 `temperature` 為例：

```go
func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for msg := range ctx.Messages() {
		switch msg.Handle {
		case "input1":
			var temperature float64
			if value, exists := msg.Data["temperature"]; !exists {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input1 schema 未定義 tag temperature"))
				continue
			} else if value == nil {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature 未由上游節點成功輸出"))
				continue
			} else if castedValue, ok := value.(float64); !ok {
				ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: tag temperature 型別不符，預期 float64"))
				continue
			} else {
				temperature = castedValue
			}
			_ = temperature

		default:
			// 未在 schema 中定義的 handle，忽略即可
			continue
		}
	}
}
```

其他型別的斷言只是把目標 Go 型別換掉，流程相同。`jsonObject` 欄位在預設與 `UseRawJson()` 兩種模式下的斷言分別為：

```go
// 預設：jsonObject 交付為 map[string]any
payload, ok := msg.Data["payload"].(map[string]any)

// 呼叫 UseRawJson() 後：jsonObject 交付為 json.RawMessage
raw, ok := msg.Data["payload"].(json.RawMessage)
```

`msg.Data` 的語意：

- `!exists`：tag 不在 input schema 定義中，屬於 internal error
- `exists && value == nil`：前一個 node 未成功輸出該 tag，由 app 決定套預設值、跳過或回報 error
- `exists && value != nil`：可進行型別判斷，Go 型別與 input schema 的 `type` 一致，見下表：

| type | handler 讀到的 Go 型別 |
| --- | --- |
| `bool` | `bool` |
| `int16` | `int16` |
| `int32` | `int32` |
| `int64` | `int64` |
| `uint16` | `uint16` |
| `uint32` | `uint32` |
| `uint64` | `uint64` |
| `float` | `float32` |
| `double` | `float64` |
| `string` | `string` |
| `raw` | `[]byte` |
| `jsonObject` | `map[string]any`（呼叫 `UseRawJson()` 後為 `json.RawMessage`） |
| `jsonArray` | `[]any`（呼叫 `UseRawJson()` 後為 `json.RawMessage`） |

### Publish 規則

`Publish` 的行為：

- 依節點 `handle` 對應的 output schema 建構 payload；`handle` 須已在 `config.data.outputs` 中定義，否則回傳 error
- schema 中的欄位若未出現在 `data` 裡，SDK 補空欄位
- 明確提供但值為 `nil` 的欄位，同樣補空欄位
- `data` 裡不在該 output schema 中的 key 一律忽略

缺少 output 欄位時的具體例子：

```go
// output1 schema:
// - power: type=double
// - status: type=string

_ = ctx.Publish("output1", map[string]any{
	"power": 42.0,
})
```

SDK 用傳入的值建立 `power`，`status` 因未在 `data` 中出現而補空欄位。明確傳 `status: nil` 結果相同：

```go
err := ctx.Publish("output1", map[string]any{
	"power": 42.0,
	"status": nil,
})
if err != nil {
	ctx.ReportError(neoedgex.CodeProcessError, err)
}
```

### Go 值轉換

`ctx.Publish` 的轉換行為由傳入 handle 對應 schema 的 destination type 決定。

<table>
  <thead>
    <tr>
      <th>Destination type</th>
      <th>Go 值類別</th>
      <th>轉換規則</th>
      <th>例子</th>
      <th>不接受 / 備註</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td rowspan="2"><code>bool</code></td>
      <td><code>bool</code></td>
      <td><code>true -&gt; "true"</code>，<code>false -&gt; "false"</code></td>
      <td><code>true -&gt; "true"</code></td>
      <td rowspan="2">不接受普通 <code>string</code>。</td>
    </tr>
    <tr>
      <td>有號整數、無號整數、浮點</td>
      <td>採用 zero / non-zero 規則：<code>0</code> 或 <code>0.0</code> 轉成 <code>"false"</code>；其他值都轉成 <code>"true"</code>。</td>
      <td><code>int64(0) -&gt; "false"</code>；<code>float64(3.14) -&gt; "true"</code></td>
    </tr>
    <tr>
      <td rowspan="4"><code>int16</code>、<code>int32</code>、<code>int64</code></td>
      <td>有號整數</td>
      <td>轉到目標位寬，並做 range check。</td>
      <td><code>int32(42) -&gt; "42"</code></td>
      <td rowspan="4"><code>NaN</code>、<code>Inf</code>、超出範圍的值、<code>time.Time</code>、<code>[]byte</code> 都會失敗。</td>
    </tr>
    <tr>
      <td>無號整數</td>
      <td>轉到目標位寬，並做 range check。</td>
      <td><code>uint32(42) -&gt; "42"</code></td>
    </tr>
    <tr>
      <td><code>float32</code>、<code>float64</code></td>
      <td>先截斷小數部分，再做轉換與 range check。</td>
      <td>destination <code>int64</code> + <code>float64(12.9) -&gt; "12"</code></td>
    </tr>
    <tr>
      <td><code>bool</code>、數字字串</td>
      <td><code>bool</code> 轉成 <code>1</code> 或 <code>0</code>。數字字串必須能被 parse 成目標型別。</td>
      <td>destination <code>int32</code> + <code>true -&gt; "1"</code>；destination <code>int16</code> + <code>"42" -&gt; "42"</code></td>
    </tr>
    <tr>
      <td rowspan="4"><code>uint16</code>、<code>uint32</code>、<code>uint64</code></td>
      <td>有號整數</td>
      <td>只有在結果非負且落在目標 uint 範圍內時，才可轉到目標位寬。</td>
      <td>destination <code>uint32</code> + <code>int32(42) -&gt; "42"</code></td>
      <td rowspan="4">負值、<code>NaN</code>、<code>Inf</code>、超出範圍的值都會失敗。</td>
    </tr>
    <tr>
      <td>無號整數</td>
      <td>轉到目標 uint 位寬，並做 range check。</td>
      <td><code>uint32(42) -&gt; "42"</code></td>
    </tr>
    <tr>
      <td><code>float32</code>、<code>float64</code></td>
      <td>先截斷小數部分，再做 uint 範圍檢查與轉換。</td>
      <td>destination <code>uint64</code> + <code>float64(12.9) -&gt; "12"</code></td>
    </tr>
    <tr>
      <td><code>bool</code>、數字字串</td>
      <td><code>bool</code> 轉成 <code>1</code> 或 <code>0</code>。數字字串必須能被 parse 成目標型別。</td>
      <td>destination <code>uint32</code> + <code>true -&gt; "1"</code></td>
    </tr>
    <tr>
      <td rowspan="4"><code>float</code>、<code>double</code></td>
      <td>有號整數</td>
      <td>轉成目標浮點格式，並以 scientific notation 序列化。</td>
      <td>destination <code>double</code> + <code>int64(42) -&gt; "4.2e+01"</code></td>
      <td rowspan="4">不接受 <code>time.Time</code> 與 <code>[]byte</code>。</td>
    </tr>
    <tr>
      <td>無號整數</td>
      <td>轉成目標浮點格式，並以 scientific notation 序列化。</td>
      <td>destination <code>double</code> + <code>uint32(42) -&gt; "4.2e+01"</code></td>
    </tr>
    <tr>
      <td><code>float32</code>、<code>float64</code></td>
      <td>轉成目標精度。</td>
      <td>destination <code>float</code> + <code>float64(25.5) -&gt; "2.55e+01"</code></td>
    </tr>
    <tr>
      <td><code>bool</code>、數字字串</td>
      <td><code>bool</code> 轉成 <code>1.0</code> 或 <code>0.0</code>。數字字串必須能被 parse 成目標浮點型別。</td>
      <td>destination <code>double</code> + <code>true -&gt; "1e+00"</code>；destination <code>double</code> + <code>"3.14" -&gt; "3.14e+00"</code></td>
    </tr>
    <tr>
      <td rowspan="4"><code>string</code></td>
      <td><code>string</code></td>
      <td>原樣保留。</td>
      <td><code>"neoedgex" -&gt; "neoedgex"</code></td>
      <td rowspan="4">不接受 <code>time.Time</code> 與 <code>[]byte</code>。</td>
    </tr>
    <tr>
      <td>有號整數、無號整數</td>
      <td>轉成十進位字串。</td>
      <td><code>42 -&gt; "42"</code></td>
    </tr>
    <tr>
      <td><code>float32</code>、<code>float64</code></td>
      <td>轉成 scientific notation 字串。</td>
      <td><code>25.5 -&gt; "2.55e+01"</code></td>
    </tr>
    <tr>
      <td><code>bool</code></td>
      <td>轉成 <code>"true"</code> 或 <code>"false"</code>。</td>
      <td><code>true -&gt; "true"</code></td>
    </tr>
    <tr>
      <td><code>raw</code></td>
      <td><code>[]byte</code></td>
      <td>在 wire 上做 base64 encode。只允許 <code>raw</code> 轉 <code>raw</code>；沒有其他型別能轉入或轉出 <code>raw</code>。</td>
      <td><code>[]byte("hello") -&gt; "aGVsbG8="</code></td>
      <td>其他 Go 型別都不支援。</td>
    </tr>
    <tr>
      <td rowspan="3"><code>jsonObject</code></td>
      <td><code>map[string]any</code></td>
      <td>SDK 會對 map 做 <code>json.Marshal</code> 後寫入 wire value。欄位必須宣告為 <code>jsonObject</code>。</td>
      <td><code>map[string]any{"k":1} -&gt; "{\"k\":1}"</code></td>
      <td rowspan="6">嚴格驗證，且只接受同形狀：<code>jsonObject</code> / <code>jsonArray</code> 欄位只接受自己對應的 JSON 形狀（強制 object 與 array 之分，拒絕 <code>null</code>）。struct <b>不會</b>自動 marshal——請自行 marshal 成 <code>json.RawMessage</code>。其他 Go 型別一律拒絕。</td>
    </tr>
    <tr>
      <td><code>json.RawMessage</code>（object）</td>
      <td>先做嚴格驗證，再<b>逐字</b>寫入（只 trim 前後空白，不重新 marshal / compact），因此大整數可保留完整精度。形狀由第一個非空白 byte 判定。</td>
      <td><code>json.RawMessage("{\"id\":9223372036854775807}")</code> 會逐 byte 保留。</td>
    </tr>
    <tr>
      <td><code>[]any</code>（此處拒絕）</td>
      <td>array 值不能放進 <code>jsonObject</code> 欄位。</td>
      <td>—</td>
    </tr>
    <tr>
      <td rowspan="3"><code>jsonArray</code></td>
      <td><code>[]any</code></td>
      <td>SDK 會對 slice 做 <code>json.Marshal</code> 後寫入 wire value。欄位必須宣告為 <code>jsonArray</code>。</td>
      <td><code>[]any{1,2,3} -&gt; "[1,2,3]"</code></td>
    </tr>
    <tr>
      <td><code>json.RawMessage</code>（array）</td>
      <td>先做嚴格驗證，再<b>逐字</b>寫入（只 trim 前後空白），保留大整數精度。形狀由第一個非空白 byte 判定。</td>
      <td><code>json.RawMessage("[9223372036854775807]")</code> 會逐 byte 保留。</td>
    </tr>
    <tr>
      <td><code>map[string]any</code>（此處拒絕）</td>
      <td>object 值不能放進 <code>jsonArray</code> 欄位。</td>
      <td>—</td>
    </tr>
  </tbody>
</table>

SDK 依據 Go 值與 schema 的 destination type 決定是否可轉換。

假設 `output1` schema 定義了這個欄位：

```text
- enabled: type=bool
```

若這樣 publish：

```go
err := ctx.Publish("output1", map[string]any{
	"enabled": 9527,
})
```

SDK 套用 `bool` 的 zero / non-zero 規則，`enabled` 轉成 `true`。

但若改成這樣 publish：

```go
err := ctx.Publish("output1", map[string]any{
	"enabled": "true",
})
```

`Publish` 不因此回傳 error；SDK 把 `enabled` 設為 empty field，並呼叫 `ReportError` 回報平台。`Publish` 只有在三種情況下才回傳 error：指定的 output handle 不存在、JSON 序列化失敗、或 MQTT 發送失敗。型別轉換失敗不會透過回傳值傳遞。

### Publish 流程

以下是完整的 end-to-end 範例，說明 Go 值如何從 handler 流向下游節點。假設 `output1` schema 定義如下：

```text
- temperature: type=double
- running: type=bool
- capturedAt: type=string
```

handler 發布 Go 值。`string` 欄位預期收到 Go `string`：

```go
func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for range ctx.Messages() {
		if err := ctx.Publish("output1", map[string]any{
			"temperature": 25.5,
			"running":     true,
			"capturedAt":  "2026-03-22T10:30:00Z",
		}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}
```

SDK 在 publisher 這一側轉成以下 output payload，每個欄位只帶 `type` 與 `value`：

```json
{
  "source": "publisher-node",
  "data": {
    "temperature": {
      "type": "double",
      "value": "2.55e+01"
    },
    "running": {
      "type": "bool",
      "value": "true"
    },
    "capturedAt": {
      "type": "string",
      "value": "2026-03-22T10:30:00Z"
    }
  }
}
```

下游 node 在 `input1` 收到後，SDK 解碼完成，handler 看到的 `neoedgex.Message` 為（`string` 欄位解碼成 Go `string`）：

```go
neoedgex.Message{
	Handle:    "input1",
	Source:    "publisher-node",
	Timestamp: "2026-03-22T10:30:00Z",
	Data: map[string]any{
		"temperature": 25.5,
		"running":     true,
		"capturedAt":  "2026-03-22T10:30:00Z",
	},
}
```

## Mock 開發流程

mock mode 適合本地開發與整合測試，不需要真實 NeoEdgeX 平台。

```go
package main

import (
	"log"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/mock"
)

func main() {
	app := neoedgex.New(&ExampleApp{})

	config, err := mock.LoadConfig("./mock-config.json")
	if err != nil {
		log.Fatal(err)
	}
	app.EnableMock(config)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

最小 mock config：

```json
{
  "nodes": [
    {
      "id": "node-1",
      "type": "app",
      "data": {
        "name": "demo-node",
        "inputs": {
          "input1": [
            { "key": "temperature", "type": "double" }
          ]
        },
        "outputs": {
          "output1": [
            { "key": "value", "type": "string" }
          ]
        },
        "application": {
          "key": "demo-app",
          "version": "2.0.0"
        },
        "settings": {}
      }
    }
  ],
  "mock": {
    "messageInterval": "3s",
    "messages": [
      {
        "nodeID": "node-1",
        "handle": "input1",
        "data": {
          "temperature": {
            "type": "double",
            "value": "2.55e+01"
          }
        }
      }
    ]
  }
}
```

`neoedgex.LoadMockConfig(...)` 是 `mock.LoadConfig(...)` 的別名；已 import `neoedgex/mock` 時，直接用 `mock.LoadConfig(...)` 即可。

正式部署時不要開啟 mock mode。

## 單元測試輔助

用 `neoedgex/testutil.MockNodeEnv` 建立測試用的 `NodeEnv`，可設定 `Config`、`MessageChan`、`DoneChan`、`MockLogger`、`PublishErr`；執行後從 `PublishedData`、`ReportedErrors`、`StopCalled` 取得結果。

```go
ctx := &testutil.MockNodeEnv{
	Config:      nodeConfig,
	MessageChan: messages,
}

handler.Handle(ctx)

if len(ctx.PublishedData) == 0 {
	t.Fatal("expected published data")
}
```

這個套件只建議用在測試；正式 app entrypoint 不需要 import `neoedgex/testutil`。

## 執行時行為

SDK 負責：

- SDK 初始化與關閉
- node instance 生命週期
- 訊息傳輸整合
- 定期 heartbeats 與狀態發佈
- process signal 處理
- handler 監控與重啟

handler 作者負責：

- 從 `ctx.Messages()` 讀訊息
- 實作業務邏輯
- 正確發布 output 與回報錯誤
- 用 `ctx.Context()` 作為 worker、HTTP、DB、gRPC 等長生命週期工作的 root context
- 需要 node-scoped log 時使用 `ctx.Logger()`
- 在 `ctx.Messages()` 關閉後正常 return

執行規則：

- 每個被匹配到的 node 都會在自己的 goroutine 裡執行 `Handle(ctx)`
- 若 handler panic，SDK 會 recover 並把它視為 node failure
- 若 handler 在 node 還活著時提早 return，SDK 會視為異常並重啟
- 若是正常關閉，訊息 channel 關閉後 handler 應直接 return
- 若 handler 在初始化階段發現無法繼續執行的 fatal error，應先 `ctx.ReportError(neoedgex.CodeInitializationError, err)`，再 `ctx.Stop()`，最後 return
- 呼叫 `ctx.Stop()` 同時取消 `ctx.Context()`
- 訊息 channel buffer 大小為 4096；buffer 滿時訊息會被 drop，SDK 同時呼叫 `ReportError`，但被 drop 的訊息無法復原

例如：

```go
if _, err := ParseSettings(ctx.NodeConfig()); err != nil {
	ctx.ReportError(neoedgex.CodeInitializationError, err)
	ctx.Stop()
	return
}
```

## 常見錯誤

- `Handle` 太早 return。正常 steady-state 寫法通常是持續讀 `ctx.Messages()`。
- 把 `msg.Data` 裡的 missing key 和 present-but-`nil` 當成同一種情況。
- 因為 repo 看得到就直接依賴未公開的內部路徑。
- 正式版程式碼忘記拿掉 mock mode。
- 以為 `msg.Data` 裡的每個 input tag 都一定會有可直接使用的值；實際上某些欄位可能是 `nil`，需要由 app 自己決定怎麼處理。

## 版本變更紀錄

本 SDK 遵循 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)。最新版本列在最前面。

### v2.0.0 — 2026-07-09

**BREAKING。** tag、parameter、port 欄位現在只用 `type` 描述，獨立的 `format` 概念已移除。

- **移除** `DataFormat` 型別及其整個 `Format*` enum，連同 `ConvertValueByFormat`、`TypeFormatMap`、`DataFormat.GetType()`，以及 `PortFieldData` / `PortFieldSchema` 上的 `Format` 欄位。wire payload 與 schema 現在只帶 `type` 與 `value`。
- **value API 改為 type-based。** 使用 `ConvertValueByType(value string, t DataType) (any, error)` 與 `func (DataType) CanConvertTo(DataType) bool`。`ConvertAnyValue` 現在回傳 `(string, DataType, error)`。
- **移除的 format。** time format `second` / `millisecond` / `datetime` 已移除；時間值以 `string` 欄位傳遞，字串格式由應用自行決定，SDK 不作限制。`base64` 由 `raw` 型別取代。`raw` 在**兩個方向**都是 `[]byte`——handler 從 `msg.Data` 讀到 `[]byte`，傳入 `Publish` 的 `[]byte` 由 SDK 在 wire 上做 base64 encode；`raw` 只能轉 `raw`。
- **新增** `jsonObject` 與 `jsonArray` DataType。其 `value` 是 JSON 編碼字串，採嚴格驗證：拒絕 `null`，並強制 object 與 array 之分，且不與其他型別互轉。handler 自 `Message.Data` 讀到的 json 欄位預設解碼成 `map[string]any` / `[]any`（呼叫 `UseRawJson()` 後為 `json.RawMessage`）。傳入 `Publish` 的 json 欄位只接受三種 Go 形式：`map[string]any` 與 `[]any`（由 SDK marshal），以及 `json.RawMessage`（先嚴格驗證，再逐字寫入，讓大整數保留完整精度）。struct 不會自動 marshal，須自行 marshal 成 `json.RawMessage`。
- **新增** `(*App).UseRawJson()` 選項。啟用後，handler 自 `Message.Data` 讀到的 `jsonObject` / `jsonArray` 欄位會以 `json.RawMessage`（驗證過的原始 bytes）交付，而非 `map[string]any` / `[]any`，可保留大整數精度並在 forwarder 中逐字重新 marshal。驗證行為不變；非 JSON 型別不受影響。
- **新增** `Publish` 支援預先建好的 `PortFieldData` 直通。`Publish` data map 中的值本身可以是已帶 wire-form `type`/`value` 的 `PortFieldData`（或 `*PortFieldData`）；SDK 會先以其型別驗證內含的 value，當欄位型別相同時逐字使用（byte-exact，json 大整數維持完整精度），型別不同則依既有的 `CanConvertTo` 矩陣轉換（json 不跨型別互轉）。`TypeUndefined`（空）的 `PortFieldData` 行為與 `nil` 值完全相同，會送出空欄位且不報錯。
- **import 路徑。** 依 Go Semantic Import Versioning，模組路徑改為 `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2`。以 `go get github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2` 安裝，並自 `.../neoedgex-v4-app-sdk-go/v2/neoedgex` import。
- **遷移。** 所有欄位改為只用 `DataType` 描述，並移除 schema 與 payload 中所有 `format` 欄位。把 `ConvertValueByFormat` 呼叫改成 `ConvertValueByType`，並以 `DataType.CanConvertTo` 判斷是否可轉換。

### v1.1.1 — 2026-06-29

- 還原了 v1.1.0 引入的 JSON 資料格式（移除 `FormatJson` 與 JSON payload 轉換）。與 Python SDK v1.1.1 對齊。

### v1.1.0 — 2026-05-20

- 新增 input 與 output schema 的多 handle 支援。`ctx.Publish` 改為必須明確指定目的 handle（`Publish(handle string, data map[string]any) error`），handler 以 `switch msg.Handle` 進行分派。
- 新增可承載任意 JSON payload 的 JSON 資料格式（已於 v1.1.1 還原）。

### v1.0.0 — 2026-05-05

- 首次公開發行。
