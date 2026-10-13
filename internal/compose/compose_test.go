package compose

import (
	"context"
	"strings"
	"testing"

	"github.com/indikaimk/docker_works/internal/config"
)

type mockRunner struct {
	commands []string
}

func (m *mockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmdStr)
	return "ok", nil
}

func TestComposeDeploy(t *testing.T) {
	runner := &mockRunner{}
	service := NewService(runner, nil)

	tempDir := t.TempDir()
	app := &config.AppConfig{
		Name:         "test-app",
		WorkDir:      tempDir,
		ComposeFiles: []string{"docker-compose.yml"},
		Hooks: config.HooksConfig{
			PreDeploy:  "echo pre",
			PostDeploy: "echo post",
		},
		AutoPrune: true,
	}

	err := service.Deploy(context.Background(), app, "docker compose")
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	expectedSeq := []string{
		"/bin/sh -c echo pre",
		"docker compose -f docker-compose.yml pull",
		"docker compose -f docker-compose.yml up -d --remove-orphans",
		"docker compose -f docker-compose.yml ps",
		"/bin/sh -c echo post",
		"docker image prune -f",
	}

	if len(runner.commands) != len(expectedSeq) {
		t.Fatalf("expected %d commands, got %d:\n%v", len(expectedSeq), len(runner.commands), runner.commands)
	}

	for i, exp := range expectedSeq {
		if runner.commands[i] != exp {
			t.Errorf("step %d: expected '%s', got '%s'", i, exp, runner.commands[i])
		}
	}
}
