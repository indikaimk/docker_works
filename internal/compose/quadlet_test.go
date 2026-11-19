package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/indikaimk/docker_works/internal/config"
)

type quadletMockRunner struct {
	commands    []string
	showOutputs map[string]string
}

func (m *quadletMockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmdStr := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmdStr)

	if name == "systemctl" {
		for _, a := range args {
			if strings.HasSuffix(a, ".service") {
				if out, ok := m.showOutputs[a]; ok {
					return out, nil
				}
			}
		}
		for _, a := range args {
			if a == "show" {
				return "active\nrunning\nsuccess\n", nil
			}
		}
	}
	return "ok", nil
}

func TestDeriveQuadletServiceName(t *testing.T) {
	cases := []struct {
		file     string
		override string
		want     string
	}{
		{"web.container", "", "web.service"},
		{"app.pod", "", "app-pod.service"},
		{"backend.network", "", "backend-network.service"},
		{"data.volume", "", "data-volume.service"},
		{"k8s.kube", "", "k8s.service"},
		{"base.image", "", "base-image.service"},
		{"custom.container", "my-custom", "my-custom.service"},
	}

	for _, tc := range cases {
		got := deriveQuadletServiceName(tc.file, tc.override)
		if got != tc.want {
			t.Errorf("deriveQuadletServiceName(%q, %q) = %q, want %q", tc.file, tc.override, got, tc.want)
		}
	}
}

func TestQuadletDeploySuccess(t *testing.T) {
	workDir := t.TempDir()
	quadletDir := t.TempDir()

	// Pre-populate a stale quadlet file to ensure cleanup works
	appTargetDir := filepath.Join(quadletDir, "my-quadlet-app")
	if err := os.MkdirAll(appTargetDir, 0755); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(appTargetDir, "old.container")
	if err := os.WriteFile(staleFile, []byte("[Container]\nImage=old:latest\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write .network and .container in workDir
	if err := os.WriteFile(filepath.Join(workDir, "app.network"), []byte("[Network]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "web.container"), []byte("[Container]\nImage=ghcr.io/myorg/web:latest\nNetwork=app.network\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userMode := true
	app := &config.AppConfig{
		Name:               "my-quadlet-app",
		Mode:               config.ModeQuadlet,
		WorkDir:            workDir,
		HealthCheckTimeout: 2 * time.Second,
		AutoPrune:          true,
		Quadlet: &config.QuadletConfig{
			Dir:      quadletDir,
			UserMode: &userMode,
		},
		Hooks: config.HooksConfig{
			PreDeploy:  "echo pre",
			PostDeploy: "echo post",
		},
	}

	runner := &quadletMockRunner{}
	service := NewService(runner, nil)

	if err := service.Deploy(context.Background(), app, "podman compose"); err != nil {
		t.Fatalf("quadlet deploy failed: %v", err)
	}

	expectedSeq := []string{
		"/bin/sh -c echo pre",
		"podman pull ghcr.io/myorg/web:latest",
		"systemctl --user daemon-reload",
		"systemctl --user restart app-network.service web.service",
		"systemctl --user show app-network.service --property=ActiveState,SubState,Result --value",
		"systemctl --user show web.service --property=ActiveState,SubState,Result --value",
		"/bin/sh -c echo post",
		"podman image prune -f",
	}

	if len(runner.commands) != len(expectedSeq) {
		t.Fatalf("expected %d commands, got %d:\n%v", len(expectedSeq), len(runner.commands), runner.commands)
	}
	for i, exp := range expectedSeq {
		if runner.commands[i] != exp {
			t.Errorf("step %d: expected %q, got %q", i, exp, runner.commands[i])
		}
	}

	// Verify stale file was removed and new files were synced
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("expected stale file %s to be removed", staleFile)
	}
	if _, err := os.Stat(filepath.Join(appTargetDir, "web.container")); err != nil {
		t.Errorf("expected web.container to be synced: %v", err)
	}
}

func TestQuadletDeployFailedUnit(t *testing.T) {
	workDir := t.TempDir()
	quadletDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(workDir, "web.container"), []byte("[Container]\nImage=nginx:latest\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userMode := false
	app := &config.AppConfig{
		Name:               "failing-quadlet",
		Mode:               config.ModeQuadlet,
		WorkDir:            workDir,
		HealthCheckTimeout: 2 * time.Second,
		Quadlet: &config.QuadletConfig{
			Dir:      quadletDir,
			UserMode: &userMode,
		},
	}

	runner := &quadletMockRunner{
		showOutputs: map[string]string{
			"web.service": "failed\nfailed\nexit-code\n",
		},
	}
	service := NewService(runner, nil)

	err := service.Deploy(context.Background(), app, "podman compose")
	if err == nil {
		t.Fatalf("expected error when quadlet unit fails, got nil")
	}
	if !strings.Contains(err.Error(), "web.service failed") {
		t.Errorf("expected error mentioning web.service failed, got %v", err)
	}
}
