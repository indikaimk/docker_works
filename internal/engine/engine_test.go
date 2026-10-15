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

type conditionalRunner struct {
	commands      []string
	headCommit    string
	currentCommit string
}

func (c *conditionalRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	c.commands = append(c.commands, cmdStr)

	if len(args) > 0 && args[0] == "ls-remote" {
		return c.headCommit + "\trefs/heads/main\n", nil
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return c.currentCommit + "\n", nil
	}
	if len(args) >= 3 && args[0] == "reset" && args[1] == "--hard" {
		if strings.HasPrefix(args[2], "origin/") {
			c.currentCommit = c.headCommit
		} else {
			c.currentCommit = args[2]
		}
		return "HEAD is now at " + c.currentCommit, nil
	}
	if len(args) >= 4 && args[0] == "clone" {
		c.currentCommit = c.headCommit
		return "cloned", nil
	}
	if len(args) > 0 && args[len(args)-1] == "json" {
		if c.currentCommit == "badcommit222" {
			return `[{"Name":"web-1","Service":"web","State":"running","Health":"unhealthy"}]`, nil
		}
		return `[{"Name":"web-1","Service":"web","State":"running","Health":"healthy"}]`, nil
	}
	return "ok", nil
}

func TestEngineRollbackOnHealthCheckFailure(t *testing.T) {
	runner := &conditionalRunner{
		headCommit:    "goodcommit111",
		currentCommit: "goodcommit111",
	}
	composeSvc := compose.NewService(runner, nil)
	gitSyncer := gitops.NewSyncer(runner, nil)
	regChecker := &mockRegistryChecker{}

	tempDir := t.TempDir()
	autoRollback := true
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			StateDir:           tempDir,
			ComposeCommand:     "docker compose",
			HealthCheckTimeout: 1 * time.Second,
			AutoRollback:       &autoRollback,
		},
		Apps: []config.AppConfig{
			{
				Name:               "rollback-demo",
				WorkDir:            tempDir + "/rollback-demo",
				HealthCheckTimeout: 1 * time.Second,
				Git: &config.GitConfig{
					Repo:         "https://github.com/test/repo.git",
					Branch:       "main",
					PollInterval: time.Minute,
				},
			},
		},
	}

	eng := NewEngine(cfg, composeSvc, gitSyncer, regChecker, nil)

	// Step 1: Deploy good commit
	deployed, err := eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err != nil || !deployed {
		t.Fatalf("expected successful initial deployment of good commit: %v", err)
	}

	appState := eng.getAppState("rollback-demo")
	if appState.LastGitCommit != "goodcommit111" {
		t.Fatalf("expected LastGitCommit to be goodcommit111, got %s", appState.LastGitCommit)
	}

	// Step 2: New commit arrives, but containers fail health check
	runner.headCommit = "badcommit222"

	_, err = eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err == nil {
		t.Fatalf("expected deployment to fail and trigger rollback, but got nil error")
	}

	// Verify rollback took place: LastGitCommit remains goodcommit111, LastFailedCommit is badcommit222
	appState = eng.getAppState("rollback-demo")
	if appState.LastGitCommit != "goodcommit111" {
		t.Errorf("expected LastGitCommit to remain goodcommit111 after rollback, got %s", appState.LastGitCommit)
	}
	if appState.LastFailedCommit != "badcommit222" {
		t.Errorf("expected LastFailedCommit to be badcommit222, got %s", appState.LastFailedCommit)
	}
	if appState.LastRollback == nil || appState.LastRollback.Target != "goodcommit111" {
		t.Errorf("expected rollback event targeting goodcommit111, got %+v", appState.LastRollback)
	}

	// Step 3: Verify loop prevention - next poll of badcommit222 does NOT re-trigger deploy
	runner.commands = nil
	deployedAgain, err := eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err != nil {
		t.Fatalf("unexpected error on loop check: %v", err)
	}
	if deployedAgain {
		t.Errorf("expected failed commit badcommit222 to be skipped to prevent deploy loop")
	}

	// Step 4: Developer fixes issue and pushes fixcommit333
	runner.headCommit = "fixcommit333"

	deployedFix, err := eng.CheckAndDeployGit(context.Background(), &cfg.Apps[0])
	if err != nil || !deployedFix {
		t.Fatalf("expected fixcommit333 to deploy successfully: %v", err)
	}
	appState = eng.getAppState("rollback-demo")
	if appState.LastGitCommit != "fixcommit333" {
		t.Errorf("expected LastGitCommit to be fixcommit333, got %s", appState.LastGitCommit)
	}
	if appState.LastFailedCommit != "" {
		t.Errorf("expected LastFailedCommit to be cleared after successful deployment, got %s", appState.LastFailedCommit)
	}
}

