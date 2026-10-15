package gitops

import (
	"context"
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

// buildGitEnv constructs environment variables for git commands (such as SSH keys or credentials)
func buildGitEnv(auth *config.GitAuthConfig) ([]string, string, error) {
	var env []string

	if auth == nil {
		return env, "", nil
	}

	if auth.SSHKeyPath != "" {
		sshCmd := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new", auth.SSHKeyPath)
		env = append(env, fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))
	}

	return env, "", nil
}

// buildAuthRepoURL injects token or user:pass into HTTPS URLs if provided
func buildAuthRepoURL(rawURL string, auth *config.GitAuthConfig) string {
	if auth == nil || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if auth.Token != "" {
		u.User = url.UserPassword("x-access-token", auth.Token)
		return u.String()
	} else if auth.Username != "" {
		u.User = url.UserPassword(auth.Username, auth.Password)
		return u.String()
	}

	return rawURL
}

// GetRemoteHeadCommit checks the latest commit SHA of the specified branch on the remote repository.
func (s *Syncer) GetRemoteHeadCommit(ctx context.Context, gitCfg *config.GitConfig) (string, error) {
	env, _, err := buildGitEnv(&gitCfg.Auth)
	if err != nil {
		return "", err
	}

	repoURL := buildAuthRepoURL(gitCfg.Repo, &gitCfg.Auth)
	refTarget := gitCfg.Branch

	output, err := s.runner.Run(ctx, "", env, "git", "ls-remote", repoURL, refTarget)
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
	env, _, err := buildGitEnv(&gitCfg.Auth)
	if err != nil {
		return "", err
	}

	repoURL := buildAuthRepoURL(gitCfg.Repo, &gitCfg.Auth)
	gitDir := filepath.Join(targetDir, ".git")

	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		s.logger.Info("Cloning repository", "repo", gitCfg.Repo, "branch", gitCfg.Branch, "dir", targetDir)
		if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		_, err := s.runner.Run(ctx, "", env, "git", "clone", "--branch", gitCfg.Branch, "--depth", "1", repoURL, targetDir)
		if err != nil {
			return "", fmt.Errorf("git clone failed: %w", err)
		}
	} else {
		s.logger.Info("Updating existing repository", "dir", targetDir, "branch", gitCfg.Branch)
		_, err := s.runner.Run(ctx, targetDir, env, "git", "fetch", "--depth", "1", "origin", gitCfg.Branch)
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
	if strings.TrimSpace(targetCommit) == "" {
		return fmt.Errorf("cannot rollback: target commit is empty")
	}

	env, _, err := buildGitEnv(&gitCfg.Auth)
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
