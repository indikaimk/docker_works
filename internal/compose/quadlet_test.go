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
			if strings.HasSuffix(a, ".service") || strings.HasSuffix(a, ".timer") {
				if out, ok := m.showOutputs[a]; ok {
					return out, nil
				}
			}
		}
		for _, a := range args {
			if a == "show" {
				return "simple\nactive\nrunning\nsuccess\n", nil
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
		"systemctl --user show app-network.service --property=Type,ActiveState,SubState,Result --value",
		"systemctl --user show web.service --property=Type,ActiveState,SubState,Result --value",
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
			"web.service": "simple\nfailed\nfailed\nexit-code\n",
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

func TestQuadletBatchOneshotSuccess(t *testing.T) {
	workDir := t.TempDir()
	quadletDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(workDir, "migrate.container"), []byte("[Service]\nType=oneshot\n[Container]\nImage=ghcr.io/myorg/migrate:latest\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userMode := true
	app := &config.AppConfig{
		Name:               "batch-migrate",
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
			"migrate.service": "oneshot\ninactive\ndead\nsuccess\n",
		},
	}
	service := NewService(runner, nil)

	if err := service.Deploy(context.Background(), app, "podman compose"); err != nil {
		t.Fatalf("expected completed oneshot batch task to succeed, got: %v", err)
	}
}

func TestQuadletTimerBatchJob(t *testing.T) {
	workDir := t.TempDir()
	quadletDir := t.TempDir()
	systemdDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(workDir, "backup.container"), []byte("[Service]\nType=oneshot\n[Container]\nImage=ghcr.io/myorg/backup:latest\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "backup.timer"), []byte("[Timer]\nOnCalendar=daily\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userMode := true
	app := &config.AppConfig{
		Name:               "nightly-backup",
		Mode:               config.ModeQuadlet,
		WorkDir:            workDir,
		HealthCheckTimeout: 2 * time.Second,
		Quadlet: &config.QuadletConfig{
			Dir:        quadletDir,
			SystemdDir: systemdDir,
			UserMode:   &userMode,
		},
	}

	runner := &quadletMockRunner{}
	service := NewService(runner, nil)

	if err := service.Deploy(context.Background(), app, "podman compose"); err != nil {
		t.Fatalf("expected timer batch deploy to succeed, got: %v", err)
	}

	expectedSeq := []string{
		"podman pull ghcr.io/myorg/backup:latest",
		"systemctl --user daemon-reload",
		"systemctl --user enable --now backup.timer",
		"systemctl --user restart backup.timer",
		"systemctl --user show backup.timer --property=Type,ActiveState,SubState,Result --value",
	}

	if len(runner.commands) != len(expectedSeq) {
		t.Fatalf("expected %d commands, got %d:\n%v", len(expectedSeq), len(runner.commands), runner.commands)
	}
	for i, exp := range expectedSeq {
		if runner.commands[i] != exp {
			t.Errorf("step %d: expected %q, got %q", i, exp, runner.commands[i])
		}
	}

	if _, err := os.Stat(filepath.Join(systemdDir, "backup.timer")); err != nil {
		t.Errorf("expected backup.timer to be synced to %s: %v", systemdDir, err)
	}
}

func TestResolveQuadletRelativePaths(t *testing.T) {
	workDir := "/var/lib/docker-works/apps/kafka"
	input := `[Container]
Image=prom/prometheus:latest
Volume=./prometheus.yml:/etc/prometheus/prometheus.yml:Z,U
Volume=/data/kafka:/prometheus:Z,U
Volume=named-vol:/var/lib/data
EnvironmentFile=./app.env
EnvironmentFile=-./optional.env
EnvironmentFile=/etc/docker-works/secrets/prod.env
SeccompProfile=./seccomp.json
`
	got := resolveQuadletRelativePaths(input, workDir)

	checks := []string{
		"Volume=/var/lib/docker-works/apps/kafka/prometheus.yml:/etc/prometheus/prometheus.yml:Z,U",
		"Volume=/data/kafka:/prometheus:Z,U",
		"Volume=named-vol:/var/lib/data",
		"EnvironmentFile=/var/lib/docker-works/apps/kafka/app.env",
		"EnvironmentFile=-/var/lib/docker-works/apps/kafka/optional.env",
		"EnvironmentFile=/etc/docker-works/secrets/prod.env",
		"SeccompProfile=/var/lib/docker-works/apps/kafka/seccomp.json",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in resolved output:\n%s", want, got)
		}
	}
}
