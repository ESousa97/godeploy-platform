package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Event is a normalized push notification from GitHub or GitLab.
type Event struct {
	// Provider is "github" or "gitlab".
	Provider string
	// Type is the logical event name (for example "push" or "ping").
	Type string

	AppName   string
	Domain    string
	CloneURL  string
	Ref       string
	CommitSHA string
}

// Parser validates webhook signatures when Secret is non-empty and decodes provider payloads.
type Parser struct {
	// Secret is the shared HMAC key (GitHub) or static token (GitLab).
	Secret string
}

// Parse reads the request body, verifies the provider signature when configured,
// and returns an [Event]. Unsupported event kinds yield a non-nil error.
func (p Parser) Parse(r *http.Request) (Event, error) {
	ghEvent := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	glEvent := strings.TrimSpace(r.Header.Get("X-Gitlab-Event"))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return Event{}, fmt.Errorf("falha ao ler body: %w", err)
	}

	if ghEvent != "" {
		if p.Secret != "" {
			if err := verifyGitHubSignature256([]byte(p.Secret), r.Header.Get("X-Hub-Signature-256"), body); err != nil {
				return Event{}, err
			}
		}
		return parseGitHub(ghEvent, body)
	}
	if glEvent != "" {
		if p.Secret != "" {
			if token := strings.TrimSpace(r.Header.Get("X-Gitlab-Token")); token == "" || token != p.Secret {
				return Event{}, errors.New("gitlab token invalido")
			}
		}
		return parseGitLab(glEvent, body)
	}

	return Event{}, errors.New("headers de webhook nao reconhecidos (esperado GitHub ou GitLab)")
}

func verifyGitHubSignature256(secret []byte, sigHeader string, body []byte) error {
	sigHeader = strings.TrimSpace(sigHeader)
	if sigHeader == "" {
		return errors.New("assinatura GitHub ausente (X-Hub-Signature-256)")
	}
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return errors.New("assinatura GitHub invalida (esperado sha256=...)")
	}
	wantHex := strings.TrimPrefix(sigHeader, prefix)
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		return errors.New("assinatura GitHub invalida (hex)")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(body); err != nil {
		return fmt.Errorf("hmac write: %w", err)
	}
	got := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return errors.New("assinatura GitHub invalida")
	}
	return nil
}

func parseGitHub(eventType string, body []byte) (Event, error) {
	if strings.EqualFold(eventType, "ping") {
		return Event{Provider: "github", Type: "ping"}, nil
	}
	if !strings.EqualFold(eventType, "push") {
		return Event{}, fmt.Errorf("evento GitHub nao suportado: %s", eventType)
	}

	var payload struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			Name     string `json:"name"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Event{}, fmt.Errorf("json invalido: %w", err)
	}

	app := strings.TrimSpace(payload.Repository.Name)
	if app == "" {
		return Event{}, errors.New("payload sem repository.name")
	}
	clone := strings.TrimSpace(payload.Repository.CloneURL)
	if clone == "" {
		return Event{}, errors.New("payload sem repository.clone_url")
	}
	ref := strings.TrimSpace(payload.Ref)
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return Event{
		Provider:  "github",
		Type:      "push",
		AppName:   app,
		CloneURL:  clone,
		Ref:       ref,
		CommitSHA: strings.TrimSpace(payload.After),
	}, nil
}

func parseGitLab(eventType string, body []byte) (Event, error) {
	if !strings.EqualFold(eventType, "Push Hook") && !strings.EqualFold(eventType, "push hook") {
		return Event{}, fmt.Errorf("evento GitLab nao suportado: %s", eventType)
	}

	var payload struct {
		Ref      string `json:"ref"`
		Checkout string `json:"checkout_sha"`
		Project  struct {
			Path    string `json:"path_with_namespace"`
			Name    string `json:"name"`
			HTTPURL string `json:"http_url"`
			GitHTTP string `json:"git_http_url"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Event{}, fmt.Errorf("json invalido: %w", err)
	}

	app := strings.TrimSpace(payload.Project.Name)
	if app == "" {
		// fallback: usa a parte final de path_with_namespace
		if p := strings.TrimSpace(payload.Project.Path); p != "" {
			parts := strings.Split(p, "/")
			app = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	if app == "" {
		return Event{}, errors.New("payload sem project.name")
	}
	clone := strings.TrimSpace(payload.Project.GitHTTP)
	if clone == "" {
		clone = strings.TrimSpace(payload.Project.HTTPURL)
	}
	if clone == "" {
		return Event{}, errors.New("payload sem project.(git_http_url|http_url)")
	}
	ref := strings.TrimSpace(payload.Ref)
	ref = strings.TrimPrefix(ref, "refs/heads/")
	sha := strings.TrimSpace(payload.Checkout)

	return Event{
		Provider:  "gitlab",
		Type:      "push",
		AppName:   app,
		CloneURL:  clone,
		Ref:       ref,
		CommitSHA: sha,
	}, nil
}
