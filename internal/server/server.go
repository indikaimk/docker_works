package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/indikaimk/docker_works/internal/config"
)

type TriggerFunc func(appName string) error

type Server struct {
	cfg     *config.ServerConfig
	apps    map[string]*config.AppConfig
	trigger TriggerFunc
	logger  *slog.Logger
	server  *http.Server
}

func NewServer(cfg *config.ServerConfig, apps []config.AppConfig, trigger TriggerFunc, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	appMap := make(map[string]*config.AppConfig)
	for i := range apps {
		appMap[apps[i].Name] = &apps[i]
	}

	s := &Server{
		cfg:     cfg,
		apps:    appMap,
		trigger: trigger,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/webhooks/", s.handleWebhook)

	s.server = &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
	}

	return s
}

func (s *Server) Start() error {
	s.logger.Info("Starting HTTP webhook/health server", "listen", s.cfg.Listen)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failed: %w", err)
	}
	return nil
}

func (s *Server) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract app name from path /webhooks/{app}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 {
		http.Error(w, "missing app name", http.StatusBadRequest)
		return
	}
	appName := pathParts[1]

	app, exists := s.apps[appName]
	if !exists {
		s.logger.Warn("Webhook received for unknown app", "app", appName)
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Secret validation
	expectedSecret := ""
	if app.Webhook != nil && app.Webhook.Secret != "" {
		expectedSecret = app.Webhook.Secret
	} else if s.cfg.WebhookSecret != "" {
		expectedSecret = s.cfg.WebhookSecret
	}

	if expectedSecret != "" {
		if !verifyAuth(r, body, expectedSecret) {
			s.logger.Warn("Unauthorized webhook request", "app", appName)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	s.logger.Info("Webhook triggered deployment", "app", appName)
	if s.trigger != nil {
		go func() {
			if err := s.trigger(appName); err != nil {
				s.logger.Error("Async webhook deploy failed", "app", appName, "error", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "triggered",
		"app":     appName,
		"message": "Deployment initiated",
	})
}

func verifyAuth(r *http.Request, body []byte, secret string) bool {
	// 1. GitHub HMAC-SHA256: X-Hub-Signature-256: sha256=<hex>
	ghSig := r.Header.Get("X-Hub-Signature-256")
	if ghSig != "" {
		if strings.HasPrefix(ghSig, "sha256=") {
			sigHex := strings.TrimPrefix(ghSig, "sha256=")
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			expectedHex := hex.EncodeToString(mac.Sum(nil))
			return hmac.Equal([]byte(sigHex), []byte(expectedHex))
		}
	}

	// 2. GitLab token: X-Gitlab-Token
	glToken := r.Header.Get("X-Gitlab-Token")
	if glToken != "" && glToken == secret {
		return true
	}

	// 3. Authorization header: Bearer <secret>
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] == secret {
			return true
		}
	}

	// 4. Query param ?secret=<secret>
	if r.URL.Query().Get("secret") == secret {
		return true
	}

	return false
}
