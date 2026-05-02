package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParser_Parse_UnknownProvider(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader("{}"))
	_, err := Parser{}.Parse(req)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParser_Parse_GitHubPing(t *testing.T) {
	body := `{}`
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	ev, err := Parser{}.Parse(req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Provider != "github" || ev.Type != "ping" {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestParser_Parse_GitHubPush_Minimal(t *testing.T) {
	body := `{
		"ref": "refs/heads/main",
		"after": "abc123def456",
		"repository": {
			"name": "myapp",
			"clone_url": "https://github.com/org/myapp.git"
		}
	}`
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	ev, err := Parser{}.Parse(req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.AppName != "myapp" || ev.Ref != "main" || ev.CommitSHA != "abc123def456" {
		t.Fatalf("ev = %+v", ev)
	}
	if !strings.Contains(ev.CloneURL, "myapp") {
		t.Fatalf("clone %q", ev.CloneURL)
	}
}

func TestParser_Parse_GitHubSignature(t *testing.T) {
	secret := "testsecret"
	body := `{}`
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", sig)

	_, err := Parser{Secret: secret}.Parse(req)
	if err != nil {
		t.Fatal(err)
	}

	req2 := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "ping")
	req2.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	_, err = Parser{Secret: secret}.Parse(req2)
	if err == nil {
		t.Fatal("expected bad signature error")
	}
}

func TestParser_Parse_GitLabPush(t *testing.T) {
	body := `{
		"ref": "refs/heads/develop",
		"checkout_sha": "fedcba",
		"project": {
			"name": "glapp",
			"git_http_url": "https://gitlab.example/group/glapp.git"
		}
	}`
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	ev, err := Parser{}.Parse(req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Provider != "gitlab" || ev.AppName != "glapp" || ev.Ref != "develop" {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestParser_Parse_GitLabToken(t *testing.T) {
	body := `{"ref":"refs/heads/main","checkout_sha":"x","project":{"name":"a","git_http_url":"http://x"}}`
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "tok")

	_, err := Parser{Secret: "wrong"}.Parse(req)
	if err == nil {
		t.Fatal("expected token error")
	}
	reqOK := httptest.NewRequestWithContext(context.Background(), "POST", "/webhook", strings.NewReader(body))
	reqOK.Header.Set("X-Gitlab-Event", "Push Hook")
	reqOK.Header.Set("X-Gitlab-Token", "tok")
	_, err = Parser{Secret: "tok"}.Parse(reqOK)
	if err != nil {
		t.Fatal(err)
	}
}
