package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
server:
  enabled: true
  listen: ":9090"
  webhook_secret: "supersecret"

defaults:
  git_poll_interval: "30s"
  registry_poll_interval: "1m"
  state_dir: "/tmp/docker-works"

apps:
  - name: "my-app"
    git:
      repo: "https://github.com/example/repo.git"
      branch: "production"
    registry:
      watch_images:
        - "myorg/my-app:latest"
    hooks:
      pre_deploy: "echo pre"
      post_deploy: "echo post"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Server.Listen)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}

	app := cfg.Apps[0]
	if app.Name != "my-app" {
		t.Errorf("expected app name my-app, got %s", app.Name)
	}
	if app.Git.Branch != "production" {
		t.Errorf("expected branch production, got %s", app.Git.Branch)
	}
	if app.Git.PollInterval != 30*time.Second {
		t.Errorf("expected git poll interval 30s, got %v", app.Git.PollInterval)
	}
	if app.Registry.PollInterval != 1*time.Minute {
		t.Errorf("expected registry poll interval 1m, got %v", app.Registry.PollInterval)
	}
	if app.WorkDir != "/tmp/docker-works/apps/my-app" {
		t.Errorf("expected default workdir /tmp/docker-works/apps/my-app, got %s", app.WorkDir)
	}
}

func TestConfigValidation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "invalid.yaml")

	// Missing watcher
	yamlContent := `
apps:
  - name: "invalid-app"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected validation error for app with no watchers, got nil")
	}
}
