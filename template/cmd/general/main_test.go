package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/testutil"
)

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
	messages <- neoedgex.Message{
		Handle: "input1",
		Data:   map[string]any{"temperature": int32(25)},
	}
	messages <- neoedgex.Message{
		Handle: "input2",
		Data:   map[string]any{"running": true},
	}
	messages <- neoedgex.Message{
		Handle: "input3",
		Data:   map[string]any{"message": "hello"},
	}
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

	wantPublished := []testutil.PublishedMessage{
		{Handle: "output1", Data: map[string]any{"api_path": "/temperature", "response_status": int32(http.StatusCreated)}},
		{Handle: "output1", Data: map[string]any{"api_path": "/status", "response_status": int32(http.StatusCreated)}},
		{Handle: "output1", Data: map[string]any{"api_path": "/event", "response_status": int32(http.StatusCreated)}},
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
			t.Fatalf("published[%d] api_path mismatch: got %#v, want %#v", i, got.Data["api_path"], want.Data["api_path"])
		}
		if got.Data["response_status"] != want.Data["response_status"] {
			t.Fatalf("published[%d] response_status mismatch: got %#v, want %#v", i, got.Data["response_status"], want.Data["response_status"])
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
	messages <- neoedgex.Message{
		Handle: "input4",
		Data:   map[string]any{"foo": "bar"},
	}
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
