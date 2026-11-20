package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if !app.IsAutoRollbackEnabled(true) {
		t.Errorf("expected auto_rollback to be true by default")
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

func TestPodmanRuntimeDefaults(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "podman.yaml")

	yamlContent := `
defaults:
  runtime: "PODMAN"

apps:
  - name: "podman-app"
    git:
      repo: "https://github.com/example/repo.git"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load podman config: %v", err)
	}

	if cfg.Defaults.Runtime != RuntimePodman {
		t.Errorf("expected normalized runtime %q, got %q", RuntimePodman, cfg.Defaults.Runtime)
	}
	if cfg.Defaults.ComposeCommand != "podman compose" {
		t.Errorf("expected default compose_command 'podman compose', got %q", cfg.Defaults.ComposeCommand)
	}
}

func TestInvalidRuntimeValidation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "bad-runtime.yaml")

	yamlContent := `
defaults:
  runtime: "containerd"

apps:
  - name: "my-app"
    git:
      repo: "https://github.com/example/repo.git"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatalf("expected validation error for unsupported runtime, got nil")
	}
}

func TestQuadletConfigDefaultsAndValidation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "quadlet.yaml")

	yamlContent := `
defaults:
  runtime: "podman"

apps:
  - name: "web-quadlet"
    quadlet:
      user_mode: false
    git:
      repo: "https://github.com/example/quadlets.git"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load quadlet config: %v", err)
	}

	app := cfg.Apps[0]
	if app.Mode != ModeQuadlet {
		t.Errorf("expected mode %q, got %q", ModeQuadlet, app.Mode)
	}
	if app.Quadlet == nil || app.Quadlet.Dir != "/etc/containers/systemd" {
		t.Errorf("expected quadlet dir /etc/containers/systemd, got %+v", app.Quadlet)
	}

	// Quadlet mode with docker runtime should fail validation
	badCfgPath := filepath.Join(tempDir, "bad-quadlet.yaml")
	badYaml := `
defaults:
  runtime: "docker"

apps:
  - name: "web-quadlet"
    mode: "quadlet"
    git:
      repo: "https://github.com/example/quadlets.git"
`
	if err := os.WriteFile(badCfgPath, []byte(badYaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	if _, err := Load(badCfgPath); err == nil {
		t.Fatalf("expected validation error for quadlet mode with docker runtime")
	}
}

func TestSecurityValidation(t *testing.T) {
	tempDir := t.TempDir()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "path traversal in app name",
			yaml: "apps:\n  - name: \"../evil\"\n    git:\n      repo: \"https://github.com/example/repo.git\"\n",
		},
		{
			name: "slash in app name",
			yaml: "apps:\n  - name: \"foo/bar\"\n    git:\n      repo: \"https://github.com/example/repo.git\"\n",
		},
		{
			name: "git repo flag injection",
			yaml: "apps:\n  - name: \"app1\"\n    git:\n      repo: \"--upload-pack=evil\"\n",
		},
		{
			name: "git branch flag injection",
			yaml: "apps:\n  - name: \"app1\"\n    git:\n      repo: \"https://github.com/example/repo.git\"\n      branch: \"--evil\"\n",
		},
		{
			name: "git path traversal",
			yaml: "apps:\n  - name: \"app1\"\n    git:\n      repo: \"https://github.com/example/repo.git\"\n      path: \"../outside\"\n",
		},
	}

	for _, tc := range cases {
		cfgPath := filepath.Join(tempDir, strings.ReplaceAll(tc.name, " ", "_")+".yaml")
		if err := os.WriteFile(cfgPath, []byte(tc.yaml), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}
		if _, err := Load(cfgPath); err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}
}
