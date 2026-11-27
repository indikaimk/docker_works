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

type sopsMockRunner struct {
	commands []string
	envs     [][]string
}

func (m *sopsMockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	m.envs = append(m.envs, env)

	if name == "sops" {
		return "DB_PASS=supersecret\n", nil
	}
	if len(args) > 0 && args[len(args)-1] == "json" {
		return `[{"Name":"web-1","Service":"web","State":"running","Health":"healthy"}]`, nil
	}
	return "ok", nil
}

func TestSopsDecryptDuringComposeDeployAndRollback(t *testing.T) {
	workDir := t.TempDir()
	runner := &sopsMockRunner{}
	service := NewService(runner, nil)

	app := &config.AppConfig{
		Name:               "sops-app",
		WorkDir:            workDir,
		ComposeFiles:       []string{"docker-compose.yml"},
		HealthCheckTimeout: 2 * time.Second,
		Sops: &config.SopsConfig{
			AgeKeyFile: "/etc/docker-works/keys/age.key",
			Files: []config.SopsFileConfig{
				{
					Source:     "secrets.enc.env",
					Target:     ".env",
					OutputType: "dotenv",
				},
			},
		},
	}

	if err := service.Deploy(context.Background(), app, "docker compose"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	expectedSopsCmd := "sops --decrypt --output-type dotenv -- " + filepath.Join(workDir, "secrets.enc.env")
	if len(runner.commands) < 1 || runner.commands[0] != expectedSopsCmd {
		t.Fatalf("expected first command %q, got %v", expectedSopsCmd, runner.commands)
	}
	if len(runner.envs[0]) == 0 || runner.envs[0][0] != "SOPS_AGE_KEY_FILE=/etc/docker-works/keys/age.key" {
		t.Errorf("expected SOPS_AGE_KEY_FILE in env, got %v", runner.envs[0])
	}

	// Verify decrypted file exists with 0600 permissions
	dst := filepath.Join(workDir, ".env")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("expected decrypted .env at %s: %v", dst, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions on decrypted file, got %04o", perm)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "DB_PASS=supersecret\n" {
		t.Errorf("unexpected decrypted content: %q", string(data))
	}
}
