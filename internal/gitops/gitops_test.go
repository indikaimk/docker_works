package gitops

import (
	"context"
	"strings"
	"testing"

	"github.com/indikaimk/docker_works/internal/config"
)

type mockGitRunner struct {
	lsRemoteOutput string
	revParseOutput string
	executed       []string
}

func (m *mockGitRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	m.executed = append(m.executed, cmdStr)

	if len(args) > 0 && args[0] == "ls-remote" {
		return m.lsRemoteOutput, nil
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return m.revParseOutput, nil
	}
	return "", nil
}

func TestGetRemoteHeadCommit(t *testing.T) {
	runner := &mockGitRunner{
		lsRemoteOutput: "9a016629ec23fbfda93245050f24e93f3c1f0a7c\trefs/heads/main\n",
	}
	syncer := NewSyncer(runner, nil)

	gitCfg := &config.GitConfig{
		Repo:   "https://github.com/example/repo.git",
		Branch: "main",
	}

	commit, err := syncer.GetRemoteHeadCommit(context.Background(), gitCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "9a016629ec23fbfda93245050f24e93f3c1f0a7c"
	if commit != expected {
		t.Errorf("expected %s, got %s", expected, commit)
	}
}

func TestBuildAuthRepoURL(t *testing.T) {
	auth := &config.GitAuthConfig{
		Token: "my-secret-token",
	}
	raw := "https://github.com/example/repo.git"
	res := buildAuthRepoURL(raw, auth)

	if !strings.Contains(res, "x-access-token:my-secret-token@github.com") {
		t.Errorf("expected token injected into URL, got %s", res)
	}
}

func TestSyncerRollback(t *testing.T) {
	runner := &mockGitRunner{}
	syncer := NewSyncer(runner, nil)

	gitCfg := &config.GitConfig{
		Repo:   "https://github.com/example/repo.git",
		Branch: "main",
	}

	err := syncer.Rollback(context.Background(), "/tmp/app", gitCfg, "targetcommit123")
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	expectedReset := "git reset --hard targetcommit123"
	if len(runner.executed) < 2 || runner.executed[0] != expectedReset {
		t.Errorf("expected '%s', got %v", expectedReset, runner.executed)
	}
}
