package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex"
	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/neoedgex/testutil"
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

func TestExampleAppPublishesTemperatureWithMockNodeEnv(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	t.Setenv("HTTP_ENDPOINT", server.URL)

	messages := make(chan neoedgex.Message, 1)
	messages <- neoedgex.Message{
		Handle: "input1",
		Data: map[string]any{
			"temperature": int32(25),
		},
	}
	close(messages)

	ctx := &testutil.MockNodeEnv{
		MessageChan: messages,
	}

	(&ExampleApp{}).Handle(ctx)

	if requestBody != `{"number": 25}` {
		t.Fatalf("unexpected request body: %s", requestBody)
	}
	if len(ctx.ReportedErrors) != 0 {
		t.Fatalf("expected no reported errors, got %d", len(ctx.ReportedErrors))
	}
	if len(ctx.PublishedData) != 1 {
		t.Fatalf("expected one published payload, got %d", len(ctx.PublishedData))
	}

	published := ctx.PublishedData[0]
	if got := published["temperature"]; got != int32(25) {
		t.Fatalf("unexpected published temperature: %#v", got)
	}
	if got := published["response_status"]; got != http.StatusCreated {
		t.Fatalf("unexpected published response_status: %#v", got)
	}
}
