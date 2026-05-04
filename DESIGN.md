# NeoEdgeX App SDK — Design Document

> External developer guides / 第三方開發指南:
> - [docs/developer-guide.en.md](./docs/developer-guide.en.md)
> - [docs/developer-guide.zh-tw.md](./docs/developer-guide.zh-tw.md)
>
> This document is kept as an internal design and implementation note. / 本文件保留為內部設計與實作背景說明。

## 目的

此 SDK 供外部客戶開發 NeoEdgeX 節點應用程式（如 OPC-UA、Modbus 等 driver），讓應用程式能以標準化方式接收來自 NeoFlow 的資料並將結果回傳至系統。

客戶**只需關注業務邏輯**，SDK 負責處理：
- MQTT 連線與重連
- NeoFlow 訊息格式的解析
- 節點生命週期管理（心跳、狀態回報、crash recovery）
- OS Signal 處理與優雅關閉

---

## 架構

```
┌─────────────────────────────────────────────────────┐
│                     客戶程式碼                        │
│  import "neoedgex-v4-app-sdk-go/neoedgex"           │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│                  neoedgex/  (公開 API)               │
│   App  ·  NodeHandler  ·  NodeContext  ·  Node       │
│   Message  ·  EnableMock()                           │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│            neoedgex/contract/  (共用合約層)           │
│   提供 SDK 內部與其他 repo 共用的底層型別定義          │
│   DataType · DataFormat · PortFieldData · Node 等    │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│                  internal/  (實作，外部不可 import)   │
│   sdk/  ·  node/  ·  messenger/  ·  logger/         │
└─────────────────────────────────────────────────────┘
```

### 套件職責

| 套件 | 可見性 | 職責 |
|------|--------|------|
| `neoedgex/` | **公開** | 客戶唯一需要 import 的主要套件，提供 App、NodeHandler、NodeContext、Node、Message、EnableMock 等高階 API |
| `neoedgex/contract/` | **公開** | 共用型別定義（DataType、DataFormat、PortFieldData、Node 等）的底層實作，供 SDK 內部與外部 repo（如 common-go）引用；第三方 app 不需要直接 import |
| `neoedgex/mock/` | **公開** | Mock 模式設定型別（MockConfig）與 LoadConfig 檔案解析 |
| `internal/` | **私有** | SDK 實作細節，Go toolchain 強制禁止外部 import |

---

## 公開 API

### `NodeHandler` — 使用者必須實作的介面

```go
type NodeHandler interface {
    // Handle 處理一個 node。每個匹配的 node 會在獨立 goroutine 中呼叫一次。
    // 當 ctx.Messages() 關閉時，handler 應該 return。
    Handle(ctx NodeContext)
}
```

### `App` — 應用程式入口

```go
// 建立 App，只需指定 handler 實作
app := neoedgex.New(&MyApp{})

// 啟動（阻塞至 SIGTERM / SIGINT）
err := app.Run()

// 開發時可開啟 mock 模式（見 Mock 模式章節）
app.EnableMock(config)
```

> **注意：** SDK 的內部配置（MQTT 連線、設定檔路徑等）完全由 `internal/` 處理，使用者不需要也無法直接設定。

### `NodeContext` — Handler 執行環境

```go
type NodeContext interface {
    // 原始 platform 節點設定（含 Settings、Outputs schema 等）
    NodeConfig() Node

    // 接收上游傳入的訊息，channel 關閉代表節點已停止
    Messages() <-chan Message

    // 發布資料至下游節點（自動依 output schema 轉換型別）
    Publish(data map[string]any) error

    // 回報節點事件（錯誤、狀態）
    ReportError(code ErrorCode, err error)
}
```

`Publish(data map[string]any)` 的 output 行為如下：

- SDK 會依 `output1` schema 逐欄位建構 payload。
- SDK 會在最終送出的 top-level payload 加上 `timestamp`，值為 publish 當下的 RFC3339 字串。
- 若 schema 欄位在 `data` 中缺少，SDK 會自動補上一個 empty field（序列化後是 `type=""`、`format=""`、`value=""`）。
- 若 schema 欄位有提供，但 value 是 `nil`，SDK 也會自動補成 empty field。
- 最終送出的 payload 會帶齊 schema 中的所有 output keys。

### `Message` — 收到的訊息

```go
type Message struct {
    Handle string                // 觸發的 input handle 名稱
    Data   map[string]any        // 已解碼的資料欄位；undefined 或解碼失敗時 value 為 nil
    Source string                // 來源節點 ID
    Timestamp string             // 上游 publish 時間；RFC3339，若舊 payload 未提供則為空字串
}
```

---

## 客戶使用範例

```go
package main

import (
    "log"

    "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex"
    "github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/mock"
)

// 實作 NodeHandler
type OpcuaApp struct{}

func (a *OpcuaApp) Handle(ctx neoedgex.NodeContext) {
    config := ctx.NodeConfig()
    // 可從 config.Data.Settings 取得節點設定

    for msg := range ctx.Messages() {
        // 處理收到的訊息
        result, err := processData(msg.Data)
        if err != nil {
            ctx.ReportError(neoedgex.CodeProcessError, err)
            continue
        }

        // 發布結果（自動轉換型別，無需手動建構 typed output payload）
        ctx.Publish(map[string]any{
            "value": result,
        })
    }
}

func main() {
    app := neoedgex.New(&OpcuaApp{})

    // === 開發時加上這段，正式版移除 ===
    config, _ := mock.LoadConfig("./mock-config.json")
    app.EnableMock(config)
    // === 開發時加上這段，正式版移除 ===

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

---

## 生命週期

```
app.Run()
  │
  ├─ SDK 初始化（讀設定檔、建立 MQTT client）
  ├─ 每個 NodeConfig 啟動 instance.Run(handler)
  │   ├─ go runLoop()             ← MQTT 訂閱、心跳（每 5 秒）
  │   └─ superviseHandler()       ← 監控 handler lifecycle
  │       ├─ handler.Handle(ctx)  ← 客戶 handler
  │       ├─ panic → recover, 回報 platform, backoff, 重啟
  │       ├─ 提早 return → 視為 crash, 重啟
  │       └─ context cancel → 正常結束
  │
  └─ 阻塞等待 SIGTERM / SIGINT
       │
       └─ context cancel → 所有 Instance 結束 → MQTT 斷線 → 返回
```

### Handler Crash Recovery

SDK 內建 handler 監控，當 handler panic 或提早 return 時：
1. **Panic recovery** — 攔截 panic，log 並回報 platform
2. **Exponential backoff** — 重啟間隔從 1s 到 30s 遞增
3. **Backoff reset** — handler 穩定運行超過 30s 後重置 backoff
4. **正常結束** — context cancel 時不重啟

---

## Mock 模式

SDK 內建 mock 模式，讓開發者不需要 MQTT broker 或 NeoEdgeX 平台環境即可在本地開發測試。

### 使用方式

在 `app.Run()` 之前呼叫 `app.EnableMock()`，正式部署時移除即可：

```go
func main() {
    app := neoedgex.New(&OpcuaApp{})

    // === 開發時加上這段，正式版移除 ===
    config, _ := mock.LoadConfig("./mock-config.json")
    app.EnableMock(config)
    // === 開發時加上這段，正式版移除 ===

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

mock-config.json 範例：

```json
{
  "nodes": [{
    "id": "node-1",
    "type": "app",
    "data": {
      "name": "test-node",
      "inputs":  { "input1": [{"key": "temperature", "type": "number", "format": "double"}] },
      "outputs": { "output1": [{"key": "result", "type": "number", "format": "double"}] },
      "settings": {}
    }
  }],
  "mock": {
    "messageInterval": "3s",
    "messages": [{
      "nodeID": "node-1",
      "handle": "input1",
      "data": { "temperature": {"type": "number", "format": "double", "value": "25.5"} }
    }]
  }
}
```

### MockConfig 結構

| 欄位 | 說明 |
|------|------|
| `Nodes` | 與平台 config.json 相同格式的節點設定 |
| `Mock.MessageInterval` | 訊息注入間隔（如 `"3s"`，預設 3 秒） |
| `Mock.Messages` | 定期注入的假輸入訊息，按 round-robin 輪流送出 |

### Mock 模式行為

- **Messenger**：使用記憶體內 messenger，不需要 MQTT broker
- **Publish 輸出**：所有 `Publish()` 的訊息會印到 stdout（`[MOCK PUBLISH]`）
- **訊息注入**：按 `Messages` 定義的順序與間隔，定期將假訊息送入指定節點
- **向下相容**：不呼叫 `EnableMock` 時行為完全不變

### 套件結構

| 套件 | 可見性 | 職責 |
|------|--------|------|
| `neoedgex/mock/` | **公開** | Mock 模式設定型別（MockConfig）與 LoadConfig 檔案解析 |

---

## 設定檔（MountPath 預設 `/opt/neoedgex`）

| 檔案 | 內容 |
|------|------|
| `config/config.json` | NodeConfig 陣列（由 platform 寫入） |
| `common/parameters.json` | 全域參數 |
| `config/messenger.json` | MQTT 帳號密碼 |
