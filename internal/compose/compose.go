package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

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

// ContainerStatus represents the JSON structure output by `docker compose ps --format json`
type ContainerStatus struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Service  string `json:"Service"`
	State    string `json:"State"`  // "running", "exited", "dead", "restarting", "created"
	Health   string `json:"Health"` // "healthy", "unhealthy", "starting", or ""
	ExitCode int    `json:"ExitCode"`
	Status   string `json:"Status"`
}

// ParseContainerStatuses parses docker compose ps --format json output.
func ParseContainerStatuses(raw string) ([]ContainerStatus, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	// Try JSON array first
	if strings.HasPrefix(trimmed, "[") {
		var list []ContainerStatus
		if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
			return list, nil
		}
	}

	// Try newline-delimited JSON
	var list []ContainerStatus
	lines := strings.Split(trimmed, "\n")
	hasValid := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item ContainerStatus
		if err := json.Unmarshal([]byte(line), &item); err == nil {
			list = append(list, item)
			hasValid = true
		}
	}

	if hasValid {
		return list, nil
	}

	return nil, fmt.Errorf("unable to parse output as container status JSON")
}

// HealthEvaluation represents the evaluation of containers
type HealthEvaluation struct {
	AllHealthy    bool
	Failed        bool
	FailureReason string
	StartingCount int
}

func EvaluateStatuses(statuses []ContainerStatus) HealthEvaluation {
	if len(statuses) == 0 {
		return HealthEvaluation{
			AllHealthy:    false,
			Failed:        false,
			FailureReason: "no containers found running",
		}
	}

	startingCount := 0
	runningCount := 0

	for _, s := range statuses {
		// 1. Explicit unhealthy
		if strings.EqualFold(s.Health, "unhealthy") {
			return HealthEvaluation{
				AllHealthy:    false,
				Failed:        true,
				FailureReason: fmt.Sprintf("service '%s' (container %s) is unhealthy", s.Service, s.Name),
			}
		}

		// 2. Dead or exited with non-zero code
		if strings.EqualFold(s.State, "dead") {
			return HealthEvaluation{
				AllHealthy:    false,
				Failed:        true,
				FailureReason: fmt.Sprintf("service '%s' (container %s) is dead", s.Service, s.Name),
			}
		}

		if strings.EqualFold(s.State, "exited") && s.ExitCode != 0 {
			return HealthEvaluation{
				AllHealthy:    false,
				Failed:        true,
				FailureReason: fmt.Sprintf("service '%s' (container %s) exited with error code %d", s.Service, s.Name, s.ExitCode),
			}
		}

		// 3. Health check still running
		if strings.EqualFold(s.Health, "starting") {
			startingCount++
			continue
		}

		// 4. Running container
		if strings.EqualFold(s.State, "running") {
			runningCount++
		}
	}

	if startingCount > 0 {
		return HealthEvaluation{
			AllHealthy:    false,
			Failed:        false,
			StartingCount: startingCount,
		}
	}

	if runningCount > 0 {
		return HealthEvaluation{
			AllHealthy: true,
			Failed:     false,
		}
	}

	return HealthEvaluation{
		AllHealthy:    false,
		Failed:        false,
		FailureReason: "containers are not yet in running state",
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

func (s *Service) WaitForHealth(ctx context.Context, app *config.AppConfig, composeCmd string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	s.logger.Info("Waiting for containers to report healthy", "app", app.Name, "timeout", timeout)

	pollInterval := 2 * time.Second
	if timeout < 5*time.Second {
		pollInterval = 50 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("health check timed out after %v", timeout)
		}

		out, err := s.executeCompose(ctx, app, composeCmd, "ps", "--format", "json")
		if err != nil {
			s.logger.Debug("Failed to query ps --format json, will retry", "app", app.Name, "error", err)
		} else {
			statuses, parseErr := ParseContainerStatuses(out)
			if parseErr != nil {
				lowerOut := strings.ToLower(out)
				if strings.Contains(lowerOut, "unhealthy") {
					return fmt.Errorf("container reported unhealthy: %s", strings.TrimSpace(out))
				}
				if strings.Contains(lowerOut, "healthy") || strings.Contains(lowerOut, "ok") {
					return nil
				}
			} else {
				eval := EvaluateStatuses(statuses)
				if eval.Failed {
					return fmt.Errorf("health check failed: %s", eval.FailureReason)
				}
				if eval.AllHealthy {
					s.logger.Info("All services reported healthy/running", "app", app.Name)
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
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

// Rollback restores containers to previous state using on_rollback hook and docker compose up.
func (s *Service) Rollback(ctx context.Context, app *config.AppConfig, composeCmd string) error {
	s.logger.Warn("Executing compose rollback for app", "app", app.Name, "work_dir", app.WorkDir)

	// 1. Run on_rollback hook if configured
	if app.Hooks.OnRollback != "" {
		if err := s.RunHook(ctx, app.Hooks.OnRollback, app.WorkDir, app.Name); err != nil {
			s.logger.Warn("on_rollback hook returned error", "app", app.Name, "error", err)
		}
	}

	// 2. Restart containers with previous configuration
	s.logger.Info("Restoring containers to previous state", "app", app.Name)
	if out, err := s.Up(ctx, app, composeCmd); err != nil {
		return fmt.Errorf("failed to restart containers during rollback: %w, output: %s", err, out)
	}

	// 3. Verify health of rolled-back containers
	if err := s.WaitForHealth(ctx, app, composeCmd, app.HealthCheckTimeout); err != nil {
		s.logger.Warn("Health check failed on rolled-back containers", "app", app.Name, "error", err)
		return fmt.Errorf("rolled-back containers failed health check: %w", err)
	}

	s.logger.Info("Rollback deployment completed and healthy", "app", app.Name)
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

	// 4. Verify health check
	if err := s.WaitForHealth(ctx, app, composeCmd, app.HealthCheckTimeout); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// 5. Post-deploy hook (only reached if deployment and health check succeed!)
	if app.Hooks.PostDeploy != "" {
		if err := s.RunHook(ctx, app.Hooks.PostDeploy, app.WorkDir, app.Name); err != nil {
			return fmt.Errorf("post-deploy hook failed: %w", err)
		}
	}

	// 6. Optional auto prune (only reached if healthy!)
	if app.AutoPrune {
		s.logger.Info("Pruning unused docker images", "app", app.Name)
		if _, err := s.Prune(ctx); err != nil {
			s.logger.Warn("Failed to prune docker images", "error", err)
		}
	}

	s.logger.Info("Deployment successfully finished", "app", app.Name)
	return nil
}
