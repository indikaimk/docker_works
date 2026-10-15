package compose

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/indikaimk/docker_works/internal/config"
)

type mockRunner struct {
	commands []string
	psOutput string
}

func (m *mockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmdStr)

	if len(args) > 0 && args[len(args)-1] == "json" {
		if m.psOutput != "" {
			return m.psOutput, nil
		}
		return `[{"Name":"web-1","Service":"web","State":"running","Health":"healthy"}]`, nil
	}
	return "ok", nil
}

func TestComposeDeploySuccess(t *testing.T) {
	runner := &mockRunner{}
	service := NewService(runner, nil)

	tempDir := t.TempDir()
	app := &config.AppConfig{
		Name:               "test-app",
		WorkDir:            tempDir,
		ComposeFiles:       []string{"docker-compose.yml"},
		HealthCheckTimeout: 2 * time.Second,
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
		"docker compose -f docker-compose.yml ps --format json",
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

func TestComposeDeployHealthCheckFailure(t *testing.T) {
	runner := &mockRunner{
		psOutput: `[{"Name":"web-1","Service":"web","State":"running","Health":"unhealthy"}]`,
	}
	service := NewService(runner, nil)

	tempDir := t.TempDir()
	app := &config.AppConfig{
		Name:               "failing-app",
		WorkDir:            tempDir,
		ComposeFiles:       []string{"docker-compose.yml"},
		HealthCheckTimeout: 2 * time.Second,
		Hooks: config.HooksConfig{
			PostDeploy: "echo post-should-not-run",
		},
		AutoPrune: true,
	}

	err := service.Deploy(context.Background(), app, "docker compose")
	if err == nil {
		t.Fatalf("expected deployment to fail due to unhealthy container, but succeeded")
	}

	// Verify post_deploy and prune were NOT executed
	for _, cmd := range runner.commands {
		if strings.Contains(cmd, "echo post-should-not-run") {
			t.Errorf("post_deploy hook should not have run on health check failure")
		}
		if strings.Contains(cmd, "prune") {
			t.Errorf("auto_prune should not have run on health check failure")
		}
	}
}

func TestComposeRollback(t *testing.T) {
	runner := &mockRunner{}
	service := NewService(runner, nil)

	tempDir := t.TempDir()
	app := &config.AppConfig{
		Name:               "rollback-app",
		WorkDir:            tempDir,
		ComposeFiles:       []string{"docker-compose.yml"},
		HealthCheckTimeout: 2 * time.Second,
		Hooks: config.HooksConfig{
			OnRollback: "echo on-rollback",
		},
	}

	err := service.Rollback(context.Background(), app, "docker compose")
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	expectedSeq := []string{
		"/bin/sh -c echo on-rollback",
		"docker compose -f docker-compose.yml up -d --remove-orphans",
		"docker compose -f docker-compose.yml ps --format json",
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

func TestEvaluateStatuses(t *testing.T) {
	// Healthy container
	eval1 := EvaluateStatuses([]ContainerStatus{
		{Name: "c1", Service: "s1", State: "running", Health: "healthy"},
	})
	if !eval1.AllHealthy || eval1.Failed {
		t.Errorf("expected all healthy, got %+v", eval1)
	}

	// Unhealthy container
	eval2 := EvaluateStatuses([]ContainerStatus{
		{Name: "c1", Service: "s1", State: "running", Health: "unhealthy"},
	})
	if !eval2.Failed {
		t.Errorf("expected failed evaluation for unhealthy container, got %+v", eval2)
	}

	// Container exited with error
	eval3 := EvaluateStatuses([]ContainerStatus{
		{Name: "c1", Service: "s1", State: "exited", ExitCode: 1},
	})
	if !eval3.Failed {
		t.Errorf("expected failed evaluation for exited non-zero container, got %+v", eval3)
	}

	// Container still starting
	eval4 := EvaluateStatuses([]ContainerStatus{
		{Name: "c1", Service: "s1", State: "running", Health: "starting"},
	})
	if eval4.AllHealthy || eval4.Failed || eval4.StartingCount != 1 {
		t.Errorf("expected in-progress starting state, got %+v", eval4)
	}
}

func TestParseContainerStatuses(t *testing.T) {
	// 1. JSON Array
	jsonArr := `[{"ID":"1","Name":"c1","Service":"s1","State":"running","Health":"healthy"}]`
	resArr, err := ParseContainerStatuses(jsonArr)
	if err != nil || len(resArr) != 1 {
		t.Fatalf("failed to parse json array: %v", err)
	}

	// 2. Line-by-line JSON
	ndJson := "{\"ID\":\"1\",\"Name\":\"c1\",\"Service\":\"s1\",\"State\":\"running\",\"Health\":\"healthy\"}\n{\"ID\":\"2\",\"Name\":\"c2\",\"Service\":\"s2\",\"State\":\"running\",\"Health\":\"\"}"
	resND, err := ParseContainerStatuses(ndJson)
	if err != nil || len(resND) != 2 {
		t.Fatalf("failed to parse ndjson: %v", err)
	}
}
