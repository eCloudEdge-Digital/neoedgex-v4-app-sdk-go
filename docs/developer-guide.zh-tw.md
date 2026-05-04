# NeoEdgeX App SDK v4 第三方開發指南

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

- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex`
- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/mock`

測試程式可額外 import：

- `github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/testutil`

即使 repo 裡看得到其他路徑，也不要 import 內部或不穩定的實作套件。

## 最小可用範例

實作 `neoedgex.NodeHandler`，透過 `neoedgex.New(...).Run()` 啟動。

```go
package main

import (
	"log"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex"
)

type ExampleApp struct{}

func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for range ctx.Messages() {
		if err := ctx.Publish(map[string]any{
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
      { "key": "temperature", "type": "double", "format": "double" },
      { "key": "running", "type": "bool", "format": "bool" },
      { "key": "capturedAt", "type": "string", "format": "datetime" }
    ]
  }
}
```
<img width="200" height="102" src="./assets/node-input-config.png" />

目前 input handle 只支援 `input1`，schema 應定義在 `input1` 下。

input schema 描述 handler 從 `ctx.Messages()` 讀到的欄位。每個欄位包含：

- `key`：出現在 `msg.Data` 裡的欄位名稱
- `type`：欄位的基本資料型態
- `format`：該欄位的具體表示方式

實際的 Go 型別由 SDK 解碼決定。

### Output Schema

output schema 定義在 `config.data.outputs`：

```json
{
  "outputs": {
    "output1": [
      { "key": "power", "type": "double", "format": "double" },
      { "key": "status", "type": "string", "format": "string" }
    ]
  }
}
```

目前 output handle 只支援 `output1`，schema 應定義在 `output1` 下。

這份 schema 決定 `ctx.Publish(map[string]any{...})` 的驗證與轉換行為：

- publish 的 map key 需和 `output1` 定義的 key 一致
- destination `format` 決定可接受哪些 Go 值，以及如何轉換
- schema 中被省略或為 `nil` 的欄位，SDK 補空欄位（`type=""`、`format=""`、`value=""`）

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

SDK 不會替你決定這個優先順序；這是 app 自己的 contract，也應該在你的 app 文件中明確說明。

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
- `Publish(data map[string]any)`：送出到 `output1`
- `ReportError(code, err)`：回報平台可見的 node error
- `Stop()`：要求 SDK 停止這個 node，用於 handler 遭遇無法繼續的 fatal error

`neoedgex.Message` 包含：

- `Handle`：觸發此訊息的 input handle 名稱
- `Data`：`map[string]any`，已從 NeoFlow 欄位解碼的 Go 原生值
- `Source`：來源節點 ID
- `Timestamp`：上游 publish 時間，RFC3339 格式；若上游 payload 未提供則為空字串

### 讀取 Input 值

`msg.Data` 已包含 Go 原生值。讀取時需判斷 key 是否存在，以及 value 是否為 `nil`。

假設收到的 input payload 是：

```go
neoedgex.Message{
	Handle:    "input1",
	Source:    "upstream-node",
	Timestamp: "2026-03-31T09:10:11Z",
	Data: map[string]any{
		"temperature": 25.5,
		"running":     true,
		"capturedAt":  nil,
	},
}
```

讀取範例：

```go
func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for msg := range ctx.Messages() {
		// 範例：只處理 handle 為 input1 的訊息，其他 handle 的訊息不處理。
		// (目前只有 input1 這個 handle 的訊息)
		if msg.Handle != "input1" {
			continue
		}

		// 範例：從 input1 的訊息中讀取 temperature 欄位
		var temperature float64
		if value, exists := msg.Data["temperature"]; !exists {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 沒有定義 tag temperature"))
			continue
		} else if value == nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("temperature 沒有由上游節點成功輸出"))
			// 或者選擇給予預設值，取決於實作者
			continue
		} else if castedValue, ok := value.(float64); !ok {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 未定義 tag temperature 為 float64"))
			continue
		} else {
			temperature = castedValue
		}

		// 範例：從 input1 的訊息中讀取 running 欄位
		var running bool
		if value, exists := msg.Data["running"]; !exists {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 沒有定義 tag running"))
			continue
		} else if value == nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("running 沒有由上游節點成功輸出"))
			continue
		} else if castedValue, ok := value.(bool); !ok {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 未定義 tag running 為 bool"))
			continue
		} else {
			running = castedValue
		}

		// 範例：從 input1 的訊息中讀取 capturedAt 欄位
		var capturedAt time.Time
		if value, exists := msg.Data["capturedAt"]; !exists {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 沒有定義 tag capturedAt"))
			continue
		} else if value == nil {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("capturedAt 沒有由上游節點成功輸出"))
			continue
		} else if castedValue, ok := value.(time.Time); !ok {
			ctx.ReportError(neoedgex.CodeProcessError, fmt.Errorf("internal error: input schema 未定義 tag capturedAt 為 datetime"))
			continue
		} else {
			capturedAt = castedValue
		}

		_ = temperature
		_ = running
		_ = capturedAt
		// ...
	}
}
```

`msg.Data` 的語意：

- `!exists`：tag 不在 input schema 定義中，屬於 internal error
- `exists && value == nil`：前一個 node 未成功輸出該 tag，由 app 決定套預設值、跳過或回報 error
- `exists && value != nil`：可進行型別判斷，Go 型別與 input schema 的 format 一致，見下表：

| format | handler 讀到的 Go 型別 |
| --- | --- |
| `bool` | `bool` |
| `int16` | `int16` |
| `int32` | `int32` |
| `int64` | `int64` |
| `second` | `time.Time` |
| `millisecond` | `time.Time` |
| `uint16` | `uint16` |
| `uint32` | `uint32` |
| `uint64` | `uint64` |
| `float` | `float32` |
| `double` | `float64` |
| `string` | `string` |
| `datetime` | `time.Time` |
| `base64` | `[]byte` |

### Publish 規則

`Publish` 的行為：

- 依節點的 `output1` schema 建構 payload
- schema 中的欄位若未出現在 `data` 裡，SDK 補空欄位
- 明確提供但值為 `nil` 的欄位，同樣補空欄位
- `data` 裡不在 `output1` schema 中的 key 一律忽略

缺少 output 欄位時的具體例子：

```go
// output1 schema:
// - power: type=double, format=double
// - status: type=string, format=string

_ = ctx.Publish(map[string]any{
	"power": 42.0,
})
```

SDK 用傳入的值建立 `power`，`status` 因未在 `data` 中出現而補空欄位。明確傳 `status: nil` 結果相同：

```go
err := ctx.Publish(map[string]any{
	"power": 42.0,
	"status": nil,
})
if err != nil {
	ctx.ReportError(neoedgex.CodeProcessError, err)
}
```

### Go 值轉換

`ctx.Publish` 的轉換行為由 `output1` 定義的 destination format 決定。

<table>
  <thead>
    <tr>
      <th>Destination format</th>
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
      <td><code>bool</code> 轉成 <code>1</code> 或 <code>0</code>。數字字串必須能被 parse 成目標格式。</td>
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
      <td><code>bool</code> 轉成 <code>1</code> 或 <code>0</code>。數字字串必須能被 parse 成目標格式。</td>
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
      <td><code>bool</code> 轉成 <code>1.0</code> 或 <code>0.0</code>。數字字串必須能被 parse 成目標浮點格式。</td>
      <td>destination <code>double</code> + <code>true -&gt; "1e+00"</code>；destination <code>double</code> + <code>"3.14" -&gt; "3.14e+00"</code></td>
    </tr>
    <tr>
      <td rowspan="2"><code>second</code>、<code>millisecond</code></td>
      <td>有號整數、無號整數、浮點</td>
      <td>浮點值會先截斷成 <code>int64</code>。接著數值會依 magnitude 推斷 epoch 單位：<code>&gt;= 1e17</code> 視為 ns、<code>&gt;= 1e14</code> 視為 us、<code>&gt;= 1e11</code> 視為 ms，其他視為 s。最後再序列化成 Unix seconds 或 Unix milliseconds。</td>
      <td>destination <code>second</code> + <code>float64(1711094400.9)</code> 會先截斷</td>
      <td rowspan="2">destination time formats 不接受普通 <code>string</code>。</td>
    </tr>
    <tr>
      <td><code>time.Time</code></td>
      <td>依 destination format 直接轉成 Unix seconds 或 Unix milliseconds。</td>
      <td>destination <code>millisecond</code> + <code>time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)</code> 會序列化成 Unix milliseconds</td>
    </tr>
    <tr>
      <td rowspan="2"><code>datetime</code></td>
      <td>有號整數、無號整數、浮點</td>
      <td>浮點值會先截斷成 <code>int64</code>。接著數值會依 magnitude 推斷 epoch 單位：<code>&gt;= 1e17</code> 視為 ns、<code>&gt;= 1e14</code> 視為 us、<code>&gt;= 1e11</code> 視為 ms，其他視為 s。最後會序列化成 RFC3339。</td>
      <td>destination <code>datetime</code> + <code>int64(1711094400)</code> 會轉成 <code>"2024-03-22T00:00:00Z"</code></td>
      <td rowspan="2">destination <code>datetime</code> 不接受普通 <code>string</code>。</td>
    </tr>
    <tr>
      <td><code>time.Time</code></td>
      <td>直接轉成 RFC3339，例如 <code>2026-03-22T10:30:00Z</code>。</td>
      <td>destination <code>datetime</code> + <code>time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC) -&gt; "2026-03-22T10:30:00Z"</code></td>
    </tr>
    <tr>
      <td rowspan="4"><code>string</code></td>
      <td><code>string</code></td>
      <td>原樣保留。</td>
      <td><code>"neoedgex" -&gt; "neoedgex"</code></td>
      <td rowspan="4"><code>time.Time</code> 不會自動轉成 plain <code>string</code>；若要輸出時間文字，schema 應該用 <code>datetime</code>。</td>
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
      <td><code>base64</code></td>
      <td><code>[]byte</code></td>
      <td>做 base64 encode。</td>
      <td><code>[]byte("hello") -&gt; "aGVsbG8="</code></td>
      <td>其他 Go 型別都不支援。</td>
    </tr>
  </tbody>
</table>

SDK 依據 Go 值與 schema 的 destination format 決定是否可轉換。

假設 `output1` schema 定義了這個欄位：

```text
- enabled: type=bool, format=bool
```

如果你這樣 publish：

```go
err := ctx.Publish(map[string]any{
	"enabled": 9527,
})
```

SDK 套用 `bool` 的 zero / non-zero 規則，`enabled` 轉成 `true`。

但如果你這樣 publish：

```go
err := ctx.Publish(map[string]any{
	"enabled": "true",
})
```

`Publish` 不因此回傳 error；SDK 把 `enabled` 設為 empty field，並呼叫 `ReportError` 回報平台。`Publish` 只有在三種情況下才回傳 error：`output1` schema 不存在、JSON 序列化失敗、或 MQTT 發送失敗。型別轉換失敗不會透過回傳值傳遞。

### Publish 流程

以下是完整的 end-to-end 範例，說明 Go 值如何從 handler 流向下游節點。假設 `output1` schema 定義如下：

```text
- temperature: type=double, format=double
- running: type=bool, format=bool
- capturedAt: type=string, format=datetime
```

handler 發布 Go 值：

```go
func (app *ExampleApp) Handle(ctx neoedgex.NodeEnv) {
	for msg := range ctx.Messages() {
		if msg.Handle != "input1" {
			continue
		}

		if err := ctx.Publish(map[string]any{
			"temperature": 25.5,
			"running":     true,
			"capturedAt":  time.Now(),
		}); err != nil {
			ctx.ReportError(neoedgex.CodeProcessError, err)
		}
	}
}
```

SDK 在 publisher 這一側轉成以下 output payload：

```json
{
  "source": "publisher-node",
  "data": {
    "temperature": {
      "type": "double",
      "format": "double",
      "value": "2.55e+01"
    },
    "running": {
      "type": "bool",
      "format": "bool",
      "value": "true"
    },
    "capturedAt": {
      "type": "string",
      "format": "datetime",
      "value": "2026-03-22T10:30:00Z"
    }
  }
}
```

下游 node 在 `input1` 收到後，SDK 解碼完成，handler 看到的 `neoedgex.Message` 為：

```go
neoedgex.Message{
	Handle:    "input1",
	Source:    "publisher-node",
	Timestamp: "2026-03-22T10:30:00Z",
	Data: map[string]any{
		"temperature": 25.5,
		"running":     true,
		"capturedAt":  time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC),
	},
}
```

## Mock 開發流程

mock mode 適合本地開發與整合測試，不需要真實 NeoEdgeX 平台。

```go
package main

import (
	"log"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/mock"
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
            { "key": "temperature", "type": "double", "format": "double" }
          ]
        },
        "outputs": {
          "output1": [
            { "key": "value", "type": "string", "format": "string" }
          ]
        },
        "application": {
          "key": "demo-app",
          "version": "1.0.0"
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
            "format": "double",
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
