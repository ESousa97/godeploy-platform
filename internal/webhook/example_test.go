package webhook_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"godeploy-platform/internal/webhook"
)

func ExampleParser_Parse_githubPing() {
	body := []byte(`{"zen":"design for the beholder"}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("Content-Type", "application/json")

	ev, err := (webhook.Parser{}).Parse(req)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println(ev.Provider, ev.Type)
	// Output: github ping
}
