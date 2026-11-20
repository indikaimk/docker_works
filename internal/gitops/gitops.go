package gitops

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/indikaimk/docker_works/internal/compose"
	"github.com/indikaimk/docker_works/internal/config"
)

type Syncer struct {
	runner compose.CommandRunner
	logger *slog.Logger
}

func NewSyncer(runner compose.CommandRunner, logger *slog.Logger) *Syncer {
	if runner == nil {
		runner = &compose.DefaultCommandRunner{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{
		runner: runner,
		logger: logger,
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildGitEnv constructs environment variables for git commands (such as SSH keys or HTTPS auth headers)
// without exposing credentials in process command-line arguments.
func buildGitEnv(rawRepoURL string, auth *config.GitAuthConfig) ([]string, error) {
	env := []string{"GIT_TERMINAL_PROMPT=0"}

	if auth == nil {
		return env, nil
	}

	if auth.SSHKeyPath != "" {
		if strings.ContainsAny(auth.SSHKeyPath, "\x00\r\n") {
			return nil, fmt.Errorf("invalid ssh_key_path: contains control characters")
		}
		sshCmd := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", shellQuote(auth.SSHKeyPath))
		env = append(env, fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))
	}

	if strings.HasPrefix(rawRepoURL, "http://") || strings.HasPrefix(rawRepoURL, "https://") {
		var user, pass string
		if auth.Token != "" {
			user, pass = "x-access-token", auth.Token
		} else if auth.Username != "" {
			user, pass = auth.Username, auth.Password
		}

		if user != "" {
			u, err := url.Parse(rawRepoURL)
			if err != nil {
				return nil, fmt.Errorf("invalid git repo URL: %w", err)
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			headerKey := fmt.Sprintf("http.%s://%s.extraHeader", u.Scheme, u.Host)
			env = append(env,
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0="+headerKey,
				"GIT_CONFIG_VALUE_0=Authorization: Basic "+encoded,
			)
		}
	}

	return env, nil
}

// GetRemoteHeadCommit checks the latest commit SHA of the specified branch on the remote repository.
func (s *Syncer) GetRemoteHeadCommit(ctx context.Context, gitCfg *config.GitConfig) (string, error) {
	if strings.HasPrefix(gitCfg.Repo, "-") || strings.HasPrefix(gitCfg.Branch, "-") {
		return "", fmt.Errorf("invalid git repo or branch: must not start with '-'")
	}

	env, err := buildGitEnv(gitCfg.Repo, &gitCfg.Auth)
	if err != nil {
		return "", err
	}

	output, err := s.runner.Run(ctx, "", env, "git", "ls-remote", "--", gitCfg.Repo, gitCfg.Branch)
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed for %s: %w", gitCfg.Repo, err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			// e.g. "a1b2c3d4... refs/heads/main"
			ref := parts[1]
			if ref == "refs/heads/"+gitCfg.Branch || ref == gitCfg.Branch {
				return parts[0], nil
			}
		}
	}

	// If single line returned without refs/heads prefix
	if len(lines) > 0 && len(strings.Fields(lines[0])) >= 1 {
		fields := strings.Fields(lines[0])
		if len(fields[0]) == 40 || len(fields[0]) == 64 { // valid SHA1 or SHA256
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("could not find branch '%s' in remote repository %s", gitCfg.Branch, gitCfg.Repo)
}

// GetLocalHeadCommit returns the current commit SHA of the local repository directory.
func (s *Syncer) GetLocalHeadCommit(ctx context.Context, dir string) (string, error) {
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return "", nil // Not cloned yet
	}

	output, err := s.runner.Run(ctx, dir, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed in %s: %w", dir, err)
	}

	return strings.TrimSpace(output), nil
}

// Sync ensures the local directory contains the latest code from the remote branch.
// Returns the commit SHA after sync.
func (s *Syncer) Sync(ctx context.Context, targetDir string, gitCfg *config.GitConfig) (string, error) {
	if strings.HasPrefix(gitCfg.Repo, "-") || strings.HasPrefix(gitCfg.Branch, "-") {
		return "", fmt.Errorf("invalid git repo or branch: must not start with '-'")
	}

	env, err := buildGitEnv(gitCfg.Repo, &gitCfg.Auth)
	if err != nil {
		return "", err
	}

	gitDir := filepath.Join(targetDir, ".git")

	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		s.logger.Info("Cloning repository", "repo", gitCfg.Repo, "branch", gitCfg.Branch, "dir", targetDir)
		if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		_, err := s.runner.Run(ctx, "", env, "git", "clone", "--branch", gitCfg.Branch, "--depth", "1", "--", gitCfg.Repo, targetDir)
		if err != nil {
			return "", fmt.Errorf("git clone failed: %w", err)
		}
	} else {
		s.logger.Info("Updating existing repository", "dir", targetDir, "branch", gitCfg.Branch)
		_, err := s.runner.Run(ctx, targetDir, env, "git", "fetch", "--depth", "1", "origin", "--", gitCfg.Branch)
		if err != nil {
			return "", fmt.Errorf("git fetch failed: %w", err)
		}

		_, err = s.runner.Run(ctx, targetDir, env, "git", "reset", "--hard", "origin/"+gitCfg.Branch)
		if err != nil {
			return "", fmt.Errorf("git reset --hard failed: %w", err)
		}
	}

	return s.GetLocalHeadCommit(ctx, targetDir)
}

// Rollback resets the local repository to the target commit SHA and cleans untracked files.
func (s *Syncer) Rollback(ctx context.Context, targetDir string, gitCfg *config.GitConfig, targetCommit string) error {
	if strings.TrimSpace(targetCommit) == "" || strings.HasPrefix(strings.TrimSpace(targetCommit), "-") {
		return fmt.Errorf("cannot rollback: invalid target commit")
	}

	env, err := buildGitEnv(gitCfg.Repo, &gitCfg.Auth)
	if err != nil {
		return err
	}

	s.logger.Warn("Rolling back local git repository", "dir", targetDir, "target_commit", targetCommit)

	_, err = s.runner.Run(ctx, targetDir, env, "git", "reset", "--hard", targetCommit)
	if err != nil {
		return fmt.Errorf("git reset --hard failed: %w", err)
	}

	_, _ = s.runner.Run(ctx, targetDir, env, "git", "clean", "-fd")
	return nil
}
