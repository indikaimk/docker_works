package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/indikaimk/docker_works/internal/compose"
	"github.com/indikaimk/docker_works/internal/config"
	"github.com/indikaimk/docker_works/internal/gitops"
)

type mockRunner struct {
	commands []string
}

func (m *mockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmdStr)

	if len(args) > 0 && args[0] == "ls-remote" {
		return "abc123commit456\trefs/heads/main\n", nil
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return "abc123commit456\n", nil
	}
	return "ok", nil
}

type mockRegistryChecker struct {
	digest string
}

func (m *mockRegistryChecker) GetRemoteDigest(ctx context.Context, imageRef string, authMap map[string]config.RegistryAuth) (string, error) {
	return m.digest, nil
}

func TestEngineCheckAndDeployGit(t *testing.T) {
	runner := &mockRunner{}
	composeSvc := compose.NewService(runner, nil)
	gitSyncer := gitops.NewSyncer(runner, nil)
	regChecker := &mockRegistryChecker{digest: "sha256:111"}

	tempDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			StateDir:       tempDir,
			ComposeCommand: "docker compose",
		},
		Apps: []config.AppConfig{
			{
				Name:    "demo-app",
				WorkDir: tempDir + "/app",
				Git: &config.GitConfig{
					Repo:         "https://github.com/test/repo.git",
					Branch:       "main",
					PollInterval: time.Minute,
				},
			},
		},
	}

	eng := NewEngine(cfg, composeSvc, gitSyncer, regChecker, nil)

	// First run should detect the commit "abc123commit456" and deploy
	deployed, err := eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deployed {
		t.Fatalf("expected deployment to be triggered on new commit")
	}

	// Second run with same commit should NOT trigger deploy
	deployed2, err := eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if deployed2 {
		t.Fatalf("expected no deployment when commit is unchanged")
	}
}

func TestEngineCheckAndDeployRegistry(t *testing.T) {
	runner := &mockRunner{}
	composeSvc := compose.NewService(runner, nil)
	gitSyncer := gitops.NewSyncer(runner, nil)
	regChecker := &mockRegistryChecker{digest: "sha256:first-digest"}

	tempDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			StateDir:       tempDir,
			ComposeCommand: "docker compose",
		},
		Apps: []config.AppConfig{
			{
				Name:    "reg-app",
				WorkDir: tempDir + "/reg-app",
				Registry: &config.RegistryConfig{
					WatchImages:  []string{"myorg/app:latest"},
					PollInterval: time.Minute,
				},
			},
		},
	}

	eng := NewEngine(cfg, composeSvc, gitSyncer, regChecker, nil)

	// First run: records initial digest without deploying
	deployed1, err := eng.CheckAndDeployRegistry(context.Background(), &cfg.Apps[0])
	if err != nil {
		t.Fatalf("first registry check failed: %v", err)
	}
	if deployed1 {
		t.Errorf("expected first check to record baseline digest without redeploying")
	}

	// Update remote digest
	regChecker.digest = "sha256:second-digest"

	// Second run: detects digest change and deploys
	deployed2, err := eng.CheckAndDeployRegistry(context.Background(), &cfg.Apps[0])
	if err != nil {
		t.Fatalf("second registry check failed: %v", err)
	}
	if !deployed2 {
		t.Errorf("expected deployment when image digest changes")
	}
}
