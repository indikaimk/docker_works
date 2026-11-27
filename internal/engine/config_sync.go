package engine

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/indikaimk/docker_works/internal/compose"
	"github.com/indikaimk/docker_works/internal/config"
	"github.com/indikaimk/docker_works/internal/gitops"
	"github.com/indikaimk/docker_works/internal/registry"
)

func toGitConfig(cg *config.ConfigGitConfig) *config.GitConfig {
	return &config.GitConfig{
		Repo:         cg.Repo,
		Branch:       cg.Branch,
		Path:         cg.Path,
		PollInterval: cg.PollInterval,
		Auth:         cg.Auth,
	}
}

func mergeBootstrapFallback(bootstrap, remote *config.Config) *config.Config {
	if remote.Server.Enabled == false && bootstrap.Server.Enabled {
		remote.Server = bootstrap.Server
	}
	if remote.Defaults.StateDir == "/var/lib/docker-works" && bootstrap.Defaults.StateDir != "" {
		remote.Defaults.StateDir = bootstrap.Defaults.StateDir
	}
	return remote
}

// ConfigSyncer manages polling a remote Git repository for docker-works's own configuration.
type ConfigSyncer struct {
	bootstrapCfg     *config.Config
	gitSyncer        *gitops.Syncer
	logger           *slog.Logger
	repoDir          string
	lastCommit       string
	lastFailedCommit string
}

func NewConfigSyncer(bootstrapCfg *config.Config, gitSyncer *gitops.Syncer, logger *slog.Logger) *ConfigSyncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigSyncer{
		bootstrapCfg: bootstrapCfg,
		gitSyncer:    gitSyncer,
		logger:       logger,
		repoDir:      filepath.Join(bootstrapCfg.Defaults.StateDir, "config-repo"),
	}
}

// CheckAndLoad checks the remote config_git repo and returns a newly loaded Config if a new valid commit is found.
func (cs *ConfigSyncer) CheckAndLoad(ctx context.Context) (*config.Config, string, bool, error) {
	if cs.bootstrapCfg.ConfigGit == nil {
		return nil, "", false, nil
	}

	gitCfg := toGitConfig(cs.bootstrapCfg.ConfigGit)
	remoteCommit, err := cs.gitSyncer.GetRemoteHeadCommit(ctx, gitCfg)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to check config_git remote head: %w", err)
	}

	if remoteCommit == cs.lastCommit || remoteCommit == cs.lastFailedCommit {
		return nil, "", false, nil
	}

	cs.logger.Info("New config_git commit detected", "old", cs.lastCommit, "new", remoteCommit)

	syncedCommit, err := cs.gitSyncer.Sync(ctx, cs.repoDir, gitCfg)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to sync config_git repo: %w", err)
	}
	if syncedCommit == "" {
		syncedCommit = remoteCommit
	}

	cfgPath := filepath.Join(cs.repoDir, cs.bootstrapCfg.ConfigGit.Path, cs.bootstrapCfg.ConfigGit.ConfigFile)
	loaded, err := config.Load(cfgPath)
	if err != nil {
		cs.lastFailedCommit = remoteCommit
		if cs.lastCommit != "" {
			_ = cs.gitSyncer.Rollback(ctx, cs.repoDir, gitCfg, cs.lastCommit)
		}
		return nil, "", false, fmt.Errorf("new config_git commit %s is invalid (keeping active config): %w", remoteCommit, err)
	}

	loaded = mergeBootstrapFallback(cs.bootstrapCfg, loaded)
	cs.lastCommit = syncedCommit
	cs.lastFailedCommit = ""
	return loaded, syncedCommit, true, nil
}

// RunWithConfigSync runs the Engine and hot-reloads it whenever config_git publishes a new valid commit.
func RunWithConfigSync(
	ctx context.Context,
	bootstrapCfg *config.Config,
	composeSvc *compose.Service,
	gitSyncer *gitops.Syncer,
	regChecker registry.DigestChecker,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	if bootstrapCfg.ConfigGit == nil {
		return NewEngine(bootstrapCfg, composeSvc, gitSyncer, regChecker, logger).Start(ctx)
	}

	syncer := NewConfigSyncer(bootstrapCfg, gitSyncer, logger)
	activeCfg := bootstrapCfg

	if newCfg, commit, changed, err := syncer.CheckAndLoad(ctx); err != nil {
		logger.Error("Initial config_git sync failed; falling back to bootstrap config", "error", err)
	} else if changed {
		logger.Info("Loaded initial configuration from config_git", "commit", commit, "apps", len(newCfg.Apps))
		activeCfg = newCfg
	}

	startEngine := func(cfg *config.Config) (context.CancelFunc, <-chan error) {
		engCtx, engCancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		eng := NewEngine(cfg, composeSvc, gitSyncer, regChecker, logger)
		go func() {
			done <- eng.Start(engCtx)
		}()
		return engCancel, done
	}

	engCancel, engDone := startEngine(activeCfg)
	ticker := time.NewTicker(bootstrapCfg.ConfigGit.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			engCancel()
			return <-engDone
		case err := <-engDone:
			return err
		case <-ticker.C:
			newCfg, commit, changed, err := syncer.CheckAndLoad(ctx)
			if err != nil {
				logger.Error("config_git poll failed", "error", err)
				continue
			}
			if !changed {
				continue
			}

			logger.Info("Configuration updated from config_git; hot-reloading engine", "commit", commit, "apps", len(newCfg.Apps))
			engCancel()
			<-engDone
			activeCfg = newCfg
			engCancel, engDone = startEngine(activeCfg)
		}
	}
}
