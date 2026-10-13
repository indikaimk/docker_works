package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/indikaimk/docker_works/internal/config"
)

func TestHealthz(t *testing.T) {
	srv := NewServer(&config.ServerConfig{Listen: ":8080"}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.handleHealthz(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestWebhookAuth(t *testing.T) {
	var mu sync.Mutex
	triggered := ""
	trigger := func(appName string) error {
		mu.Lock()
		triggered = appName
		mu.Unlock()
		return nil
	}

	appCfg := config.AppConfig{
		Name: "my-app",
		Webhook: &config.WebhookConfig{
			Secret: "secret123",
		},
	}

	srv := NewServer(&config.ServerConfig{Listen: ":8080"}, []config.AppConfig{appCfg}, trigger, nil)

	// 1. Unauthorized request
	req1 := httptest.NewRequest(http.MethodPost, "/webhooks/my-app", strings.NewReader(`{}`))
	w1 := httptest.NewRecorder()
	srv.handleWebhook(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 unauthorized, got %d", w1.Code)
	}

	// 2. Authorized via GitHub HMAC
	payload := `{"ref":"refs/heads/main"}`
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/my-app", strings.NewReader(payload))
	req2.Header.Set("X-Hub-Signature-256", sig)
	w2 := httptest.NewRecorder()
	srv.handleWebhook(w2, req2)

	if w2.Code != http.StatusAccepted {
		t.Errorf("expected 202 accepted, got %d", w2.Code)
	}

	mu.Lock()
	_ = triggered
	mu.Unlock()
}
