package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/testutil"
	"github.com/fxamacker/cbor/v2"
)

// newTestMessage 範例：組出一則測試用訊息。data 先以 CBOR 編碼成
// wire-form 的資料段，schema 則是該 handle 的 input schema，讓
// msg.ToMap() / msg.ToStruct() 能依 schema 型別解碼。
func newTestMessage(t *testing.T, handle string, schema []contract.PortFieldSchema, data map[string]any) neoedgex.Message {
	t.Helper()
	raw, err := cbor.Marshal(data)
	if err != nil {
		t.Fatalf("encode test message data: %v", err)
	}
	return contract.NewMessage("test-node", "2026-08-02T00:00:00Z", handle, contract.RawMessage(raw), contract.NewDecodePlan(schema), nil)
}

// valueWithType 把值連同動態型別一起格式化。published payload 是
// map[string]any，只印數值的話，型別不同但數值相同（例如 int(201)
// 與 int32(201)）會得到「got 201, want 201」這種看不出差異的訊息。
func valueWithType(v any) string {
	return fmt.Sprintf("%#v (%T)", v, v)
}

func TestExampleAppReportsMissingEndpoint(t *testing.T) {
	t.Setenv("HTTP_ENDPOINT", "")

	ctx := &testutil.MockNodeEnv{}

	(&ExampleApp{}).Handle(ctx)

	if len(ctx.ReportedErrors) != 1 {
		t.Fatalf("expected one reported error, got %d", len(ctx.ReportedErrors))
	}
	if ctx.ReportedErrors[0].Code != neoedgex.CodeProcessError {
		t.Fatalf("unexpected error code: %s", ctx.ReportedErrors[0].Code)
	}
	if len(ctx.PublishedData) != 0 {
		t.Fatalf("expected no published data, got %d", len(ctx.PublishedData))
	}
}

func TestExampleAppRoutesEachInputToItsOwnPath(t *testing.T) {
	type recorded struct {
		path string
		body string
	}
	var requests []recorded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requests = append(requests, recorded{path: r.URL.Path, body: string(body)})
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	t.Setenv("HTTP_ENDPOINT", server.URL)

	messages := make(chan neoedgex.Message, 3)
	messages <- newTestMessage(t, "input1",
		[]contract.PortFieldSchema{{Key: "temperature", Type: contract.TypeInt32}},
		map[string]any{"temperature": int32(25)})
	messages <- newTestMessage(t, "input2",
		[]contract.PortFieldSchema{{Key: "running", Type: contract.TypeBool}},
		map[string]any{"running": true})
	messages <- newTestMessage(t, "input3",
		[]contract.PortFieldSchema{{Key: "message", Type: contract.TypeString}},
		map[string]any{"message": "hello"})
	close(messages)

	ctx := &testutil.MockNodeEnv{MessageChan: messages}

	(&ExampleApp{}).Handle(ctx)

	if len(ctx.ReportedErrors) != 0 {
		t.Fatalf("expected no reported errors, got %d: %+v", len(ctx.ReportedErrors), ctx.ReportedErrors)
	}

	wantRequests := []recorded{
		{path: "/temperature", body: `{"value": 25}`},
		{path: "/status", body: `{"running": true}`},
		{path: "/event", body: `{"message":"hello"}`},
	}
	if len(requests) != len(wantRequests) {
		t.Fatalf("expected %d requests, got %d: %+v", len(wantRequests), len(requests), requests)
	}
	for i, want := range wantRequests {
		if requests[i] != want {
			t.Fatalf("request[%d] mismatch: got %+v, want %+v", i, requests[i], want)
		}
	}

	// MockNodeEnv.Publish 原樣記錄 handler 交出的 map，不做 output schema
	// 轉型，因此這裡的期望值型別就是 handler 端的型別：handler 直接發布
	// resp.StatusCode（int），所以用未加型別的 http.StatusCreated（預設為
	// int），不要寫成 int32。轉成 output schema 宣告的 int32 是 runtime
	// Publish 的事，不會出現在這份錄影裡。
	wantPublished := []testutil.PublishedMessage{
		{Handle: "output1", Data: map[string]any{"api_path": "/temperature", "response_status": http.StatusCreated}},
		{Handle: "output1", Data: map[string]any{"api_path": "/status", "response_status": http.StatusCreated}},
		{Handle: "output1", Data: map[string]any{"api_path": "/event", "response_status": http.StatusCreated}},
	}
	if len(ctx.PublishedData) != len(wantPublished) {
		t.Fatalf("expected %d published payloads, got %d", len(wantPublished), len(ctx.PublishedData))
	}
	for i, want := range wantPublished {
		got := ctx.PublishedData[i]
		if got.Handle != want.Handle {
			t.Fatalf("published[%d] handle mismatch: got %q, want %q", i, got.Handle, want.Handle)
		}
		if got.Data["api_path"] != want.Data["api_path"] {
			t.Fatalf("published[%d] api_path mismatch: got %s, want %s", i, valueWithType(got.Data["api_path"]), valueWithType(want.Data["api_path"]))
		}
		if got.Data["response_status"] != want.Data["response_status"] {
			t.Fatalf("published[%d] response_status mismatch: got %s, want %s", i, valueWithType(got.Data["response_status"]), valueWithType(want.Data["response_status"]))
		}
	}
}

func TestExampleAppIgnoresUnknownHandle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.Path)
	}))
	defer server.Close()
	t.Setenv("HTTP_ENDPOINT", server.URL)

	messages := make(chan neoedgex.Message, 1)
	messages <- newTestMessage(t, "input4", nil, map[string]any{"foo": "bar"})
	close(messages)

	ctx := &testutil.MockNodeEnv{MessageChan: messages}

	(&ExampleApp{}).Handle(ctx)

	if len(ctx.ReportedErrors) != 0 {
		t.Fatalf("expected no reported errors, got %d", len(ctx.ReportedErrors))
	}
	if len(ctx.PublishedData) != 0 {
		t.Fatalf("expected no published data, got %d", len(ctx.PublishedData))
	}
}
