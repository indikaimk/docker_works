package compose

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/indikaimk/docker_works/internal/config"
)

// CommandRunner executes commands with a context and working directory.
type CommandRunner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error)
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("command '%s %s' failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(output))
	}
	return output, nil
}

type Service struct {
	runner CommandRunner
	logger *slog.Logger
}

func NewService(runner CommandRunner, logger *slog.Logger) *Service {
	if runner == nil {
		runner = &DefaultCommandRunner{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		runner: runner,
		logger: logger,
	}
}

// buildComposeArgs returns the base arguments for docker compose: -f file.yml --env-file .env ...
func buildComposeArgs(composeFiles []string, envFiles []string, subCmd string, extraArgs ...string) []string {
	var args []string
	for _, f := range composeFiles {
		args = append(args, "-f", f)
	}
	for _, e := range envFiles {
		args = append(args, "--env-file", e)
	}
	args = append(args, subCmd)
	args = append(args, extraArgs...)
	return args
}

func (s *Service) getBinaryAndBaseArgs(composeCmd string) (string, []string) {
	parts := strings.Fields(composeCmd)
	if len(parts) == 0 {
		return "docker", []string{"compose"}
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], parts[1:]
}

func (s *Service) executeCompose(ctx context.Context, app *config.AppConfig, composeCmd string, subCmd string, extraArgs ...string) (string, error) {
	bin, baseArgs := s.getBinaryAndBaseArgs(composeCmd)
	args := append(baseArgs, buildComposeArgs(app.ComposeFiles, app.EnvFiles, subCmd, extraArgs...)...)

	s.logger.Info("Executing compose command", "app", app.Name, "command", bin+" "+strings.Join(args, " "), "dir", app.WorkDir)
	return s.runner.Run(ctx, app.WorkDir, nil, bin, args...)
}

func (s *Service) Pull(ctx context.Context, app *config.AppConfig, composeCmd string) (string, error) {
	return s.executeCompose(ctx, app, composeCmd, "pull")
}

func (s *Service) Up(ctx context.Context, app *config.AppConfig, composeCmd string) (string, error) {
	return s.executeCompose(ctx, app, composeCmd, "up", "-d", "--remove-orphans")
}

func (s *Service) Down(ctx context.Context, app *config.AppConfig, composeCmd string) (string, error) {
	return s.executeCompose(ctx, app, composeCmd, "down")
}

func (s *Service) Status(ctx context.Context, app *config.AppConfig, composeCmd string) (string, error) {
	return s.executeCompose(ctx, app, composeCmd, "ps")
}

func (s *Service) Prune(ctx context.Context) (string, error) {
	return s.runner.Run(ctx, "", nil, "docker", "image", "prune", "-f")
}

func (s *Service) RunHook(ctx context.Context, hookCmd string, dir string, appName string) error {
	if strings.TrimSpace(hookCmd) == "" {
		return nil
	}
	s.logger.Info("Running hook", "app", appName, "hook", hookCmd)
	env := []string{fmt.Sprintf("DOCKER_WORKS_APP=%s", appName)}
	output, err := s.runner.Run(ctx, dir, env, "/bin/sh", "-c", hookCmd)
	if err != nil {
		s.logger.Error("Hook failed", "app", appName, "error", err, "output", output)
		return fmt.Errorf("hook failed: %w", err)
	}
	s.logger.Debug("Hook output", "app", appName, "output", output)
	return nil
}

// Deploy coordinates the full deployment cycle: pre-deploy hook -> pull -> up -> health check -> post-deploy hook -> prune.
func (s *Service) Deploy(ctx context.Context, app *config.AppConfig, composeCmd string) error {
	s.logger.Info("Starting deployment for app", "app", app.Name, "work_dir", app.WorkDir)

	// Ensure work directory exists
	if err := os.MkdirAll(app.WorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create app workdir %s: %w", app.WorkDir, err)
	}

	// 1. Pre-deploy hook
	if app.Hooks.PreDeploy != "" {
		if err := s.RunHook(ctx, app.Hooks.PreDeploy, app.WorkDir, app.Name); err != nil {
			return fmt.Errorf("pre-deploy hook failed: %w", err)
		}
	}

	// 2. Pull images
	s.logger.Info("Pulling containers", "app", app.Name)
	if out, err := s.Pull(ctx, app, composeCmd); err != nil {
		s.logger.Warn("docker compose pull returned error (might be using local build images)", "app", app.Name, "output", out, "err", err)
	}

	// 3. Up containers
	s.logger.Info("Starting containers", "app", app.Name)
	if out, err := s.Up(ctx, app, composeCmd); err != nil {
		return fmt.Errorf("failed to start containers: %w, output: %s", err, out)
	}

	// 4. Verify status
	if statusOut, err := s.Status(ctx, app, composeCmd); err == nil {
		s.logger.Info("Container status after deployment", "app", app.Name, "status", strings.TrimSpace(statusOut))
	}

	// 5. Post-deploy hook
	if app.Hooks.PostDeploy != "" {
		if err := s.RunHook(ctx, app.Hooks.PostDeploy, app.WorkDir, app.Name); err != nil {
			return fmt.Errorf("post-deploy hook failed: %w", err)
		}
	}

	// 6. Optional auto prune
	if app.AutoPrune {
		s.logger.Info("Pruning unused docker images", "app", app.Name)
		if _, err := s.Prune(ctx); err != nil {
			s.logger.Warn("Failed to prune docker images", "error", err)
		}
	}

	s.logger.Info("Deployment successfully finished", "app", app.Name)
	return nil
}
