package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/indikaimk/docker_works/internal/config"
	"github.com/indikaimk/docker_works/internal/gitops"
)

type configSyncMockRunner struct {
	headCommit string
	onSync     func(dir string)
}

func (m *configSyncMockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "ls-remote" {
		return m.headCommit + "\trefs/heads/main\n", nil
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return m.headCommit + "\n", nil
	}
	if len(args) > 0 && (args[0] == "clone" || args[0] == "fetch") && m.onSync != nil {
		target := dir
		if args[0] == "clone" {
			target = args[len(args)-1]
		}
		m.onSync(target)
	}
	return "ok", nil
}

func TestConfigSyncerLoadsAndRejectsBrokenCommit(t *testing.T) {
	stateDir := t.TempDir()
	bootstrap := &config.Config{
		Defaults: config.DefaultsConfig{
			StateDir: stateDir,
		},
		ConfigGit: &config.ConfigGitConfig{
			Repo:         "https://github.com/example/infra.git",
			Branch:       "main",
			Path:         "hosts/prod",
			ConfigFile:   "config.yaml",
			PollInterval: time.Minute,
		},
	}

	writeRemoteConfig := func(dir, content string) {
		cfgDir := filepath.Join(dir, "hosts", "prod")
		_ = os.MkdirAll(cfgDir, 0755)
		_ = os.MkdirAll(filepath.Join(dir, ".git"), 0755)
		_ = os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0644)
	}

	runner := &configSyncMockRunner{
		headCommit: "commit111",
		onSync: func(dir string) {
			writeRemoteConfig(dir, `
apps:
  - name: "app-one"
    git:
      repo: "https://github.com/example/app-one.git"
`)
		},
	}

	syncer := NewConfigSyncer(bootstrap, gitops.NewSyncer(runner, nil), nil)
	cfg1, commit1, changed1, err := syncer.CheckAndLoad(context.Background())
	if err != nil || !changed1 || commit1 != "commit111" || len(cfg1.Apps) != 1 {
		t.Fatalf("expected valid config load on commit111, changed=%v commit=%q err=%v", changed1, commit1, err)
	}

	// Same commit should be a no-op
	_, _, changedSame, err := syncer.CheckAndLoad(context.Background())
	if err != nil || changedSame {
		t.Fatalf("expected no-op on same commit, changed=%v err=%v", changedSame, err)
	}

	// Broken commit should fail validation and be skipped on subsequent polls
	runner.headCommit = "badcommit222"
	runner.onSync = func(dir string) {
		writeRemoteConfig(dir, `apps: [invalid yaml`)
	}
	_, _, changedBad, err := syncer.CheckAndLoad(context.Background())
	if err == nil || changedBad {
		t.Fatalf("expected error on broken config commit, changed=%v err=%v", changedBad, err)
	}
	if !strings.Contains(err.Error(), "keeping active config") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Next poll of same broken commit should be skipped without error
	_, _, changedSkipped, err := syncer.CheckAndLoad(context.Background())
	if err != nil || changedSkipped {
		t.Fatalf("expected broken commit to be skipped, changed=%v err=%v", changedSkipped, err)
	}
}
