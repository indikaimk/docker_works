package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/indikaimk/docker_works/internal/compose"
	"github.com/indikaimk/docker_works/internal/config"
	"github.com/indikaimk/docker_works/internal/gitops"
	"github.com/indikaimk/docker_works/internal/metrics"
	"github.com/indikaimk/docker_works/internal/registry"
	"github.com/indikaimk/docker_works/internal/server"
)

type AppState struct {
	LastGitCommit       string            `json:"last_git_commit"`
	LastFailedCommit    string            `json:"last_failed_commit,omitempty"`
	LastOCIDigest       string            `json:"last_oci_digest,omitempty"`
	LastFailedOCIDigest string            `json:"last_failed_oci_digest,omitempty"`
	ImageDigests        map[string]string `json:"image_digests"`
	LastFailedDigests   map[string]string `json:"last_failed_digests,omitempty"`
	LastDeployed        time.Time         `json:"last_deployed"`
	LastRollback        *RollbackEvent    `json:"last_rollback,omitempty"`
}

type RollbackEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Target    string    `json:"target"`
}

type EngineState struct {
	Apps map[string]*AppState `json:"apps"`
}

type Engine struct {
	cfg             *config.Config
	composeService  *compose.Service
	gitSyncer       *gitops.Syncer
	registryChecker registry.DigestChecker
	metrics         *metrics.Registry
	logger          *slog.Logger

	appLocks map[string]*sync.Mutex
	stateMu  sync.Mutex
	state    EngineState
	stateDir string
}

func NewEngine(
	cfg *config.Config,
	composeService *compose.Service,
	gitSyncer *gitops.Syncer,
	registryChecker registry.DigestChecker,
	logger *slog.Logger,
) *Engine {
	if logger == nil {
		logger = slog.Default()
	}

	appLocks := make(map[string]*sync.Mutex)
	for _, app := range cfg.Apps {
		appLocks[app.Name] = &sync.Mutex{}
	}

	e := &Engine{
		cfg:             cfg,
		composeService:  composeService,
		gitSyncer:       gitSyncer,
		registryChecker: registryChecker,
		metrics:         metrics.NewRegistry(),
		logger:          logger,
		appLocks:        appLocks,
		stateDir:        cfg.Defaults.StateDir,
		state: EngineState{
			Apps: make(map[string]*AppState),
		},
	}

	e.loadState()
	return e
}

func (e *Engine) stateFilePath() string {
	return filepath.Join(e.stateDir, "state.json")
}

func (e *Engine) loadState() {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	data, err := os.ReadFile(e.stateFilePath())
	if err != nil {
		return // state file doesn't exist yet, start fresh
	}

	var state EngineState
	if err := json.Unmarshal(data, &state); err == nil && state.Apps != nil {
		e.state = state
	}
}

func (e *Engine) saveState() {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	if err := os.MkdirAll(e.stateDir, 0755); err != nil {
		e.logger.Warn("Failed to create state dir", "dir", e.stateDir, "error", err)
		return
	}

	data, err := json.MarshalIndent(e.state, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(e.stateFilePath(), data, 0600)
}

func (e *Engine) getAppState(appName string) *AppState {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	appState, ok := e.state.Apps[appName]
	if !ok {
		appState = &AppState{
			ImageDigests:      make(map[string]string),
			LastFailedDigests: make(map[string]string),
		}
		e.state.Apps[appName] = appState
	}
	if appState.ImageDigests == nil {
		appState.ImageDigests = make(map[string]string)
	}
	if appState.LastFailedDigests == nil {
		appState.LastFailedDigests = make(map[string]string)
	}
	return appState
}

func appWithEffectiveWorkDir(app *config.AppConfig) *config.AppConfig {
	if app.Git == nil || app.Git.Path == "" || app.Git.Path == "." {
		return app
	}
	copyApp := *app
	copyApp.WorkDir = filepath.Join(app.WorkDir, app.Git.Path)
	return &copyApp
}

// CheckAndDeployGit checks if git commit changed, updates local repo, deploys, and automatically rolls back if health checks fail.
func (e *Engine) CheckAndDeployGit(ctx context.Context, app *config.AppConfig) (bool, error) {
	if app.Git == nil {
		return false, nil
	}

	lock := e.appLocks[app.Name]
	lock.Lock()
	defer lock.Unlock()

	appState := e.getAppState(app.Name)

	remoteCommit, err := e.gitSyncer.GetRemoteHeadCommit(ctx, app.Git)
	if err != nil {
		e.metrics.RecordPollError(app.Name, "git")
		return false, fmt.Errorf("failed to check remote git commit for %s: %w", app.Name, err)
	}

	if appState.LastGitCommit == remoteCommit {
		e.logger.Debug("Git repo up to date", "app", app.Name, "commit", remoteCommit)
		return false, nil
	}

	if appState.LastFailedCommit == remoteCommit {
		e.logger.Debug("Skipping commit that previously failed health check", "app", app.Name, "commit", remoteCommit)
		return false, nil
	}

	e.logger.Info("New Git commit detected", "app", app.Name, "old", appState.LastGitCommit, "new", remoteCommit)

	previousCommit := appState.LastGitCommit

	// Clone/sync the Git repository into app.WorkDir, and run deploy from app.WorkDir/<git.path>.
	targetRepoDir := app.WorkDir
	deployApp := appWithEffectiveWorkDir(app)

	syncedCommit, err := e.gitSyncer.Sync(ctx, targetRepoDir, app.Git)
	if err != nil {
		return false, fmt.Errorf("failed to sync repo for %s: %w", app.Name, err)
	}

	// Deploy and verify health
	deployStart := time.Now()
	deployErr := e.composeService.Deploy(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
	deployDur := time.Since(deployStart)
	if deployErr != nil {
		e.metrics.RecordDeploy(app.Name, "git", "failure", deployDur)
		appState.LastFailedCommit = remoteCommit

		autoRollback := app.IsAutoRollbackEnabled(e.cfg.Defaults.IsAutoRollbackEnabled())
		if autoRollback && previousCommit != "" {
			e.logger.Error("Deployment failed health checks; triggering automated rollback", "app", app.Name, "failed_commit", remoteCommit, "rollback_commit", previousCommit, "error", deployErr)

			if rbGitErr := e.gitSyncer.Rollback(ctx, targetRepoDir, app.Git, previousCommit); rbGitErr != nil {
				e.logger.Error("Failed to reset git repository to previous commit during rollback", "app", app.Name, "error", rbGitErr)
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and git rollback failed: %w", deployErr, rbGitErr)
			}

			if rbComposeErr := e.composeService.Rollback(ctx, deployApp, e.cfg.Defaults.ComposeCommand); rbComposeErr != nil {
				e.logger.Error("Failed to restart containers during rollback", "app", app.Name, "error", rbComposeErr)
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and compose rollback failed: %w", deployErr, rbComposeErr)
			}

			appState.LastRollback = &RollbackEvent{
				Timestamp: time.Now(),
				Reason:    deployErr.Error(),
				Target:    previousCommit,
			}
			e.metrics.RecordRollback(app.Name)
			e.saveState()

			e.logger.Info("Automated rollback succeeded, app restored to previous working commit", "app", app.Name, "commit", previousCommit)
			return false, fmt.Errorf("deployment failed healthcheck: %v (automatically rolled back to commit %s)", deployErr, previousCommit)
		}

		e.saveState()
		return false, fmt.Errorf("compose deployment failed after git update: %w", deployErr)
	}

	if syncedCommit == "" {
		syncedCommit = remoteCommit
	}
	appState.LastGitCommit = syncedCommit
	appState.LastFailedCommit = ""
	appState.LastDeployed = time.Now()
	e.metrics.RecordDeploy(app.Name, "git", "success", deployDur)
	e.saveState()

	return true, nil
}

// CheckAndDeployRegistry checks if any watched image has a newer digest, triggers deploy, and rolls back if health checks fail.
func (e *Engine) CheckAndDeployRegistry(ctx context.Context, app *config.AppConfig) (bool, error) {
	if app.Registry == nil || len(app.Registry.WatchImages) == 0 {
		return false, nil
	}

	lock := e.appLocks[app.Name]
	lock.Lock()
	defer lock.Unlock()

	appState := e.getAppState(app.Name)
	var changedImages []string

	for _, imageRef := range app.Registry.WatchImages {
		remoteDigest, err := e.registryChecker.GetRemoteDigest(ctx, imageRef, app.Registry.Auth)
		if err != nil {
			e.metrics.RecordPollError(app.Name, "registry")
			e.logger.Warn("Failed to check remote digest", "app", app.Name, "image", imageRef, "error", err)
			continue
		}

		currentDigest := appState.ImageDigests[imageRef]
		if currentDigest == "" {
			// First time tracking this image, record initial digest without triggering unneeded redeploy
			appState.ImageDigests[imageRef] = remoteDigest
			continue
		}

		if appState.LastFailedDigests != nil && appState.LastFailedDigests[imageRef] == remoteDigest {
			e.logger.Debug("Skipping image digest that previously failed health check", "app", app.Name, "image", imageRef, "digest", remoteDigest)
			continue
		}

		if currentDigest != remoteDigest {
			e.logger.Info("New image digest detected", "app", app.Name, "image", imageRef, "old", currentDigest, "new", remoteDigest)
			changedImages = append(changedImages, imageRef)
		}
	}

	if len(changedImages) == 0 {
		return false, nil
	}

	// Deploy compose
	deployApp := appWithEffectiveWorkDir(app)
	deployStart := time.Now()
	deployErr := e.composeService.Deploy(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
	deployDur := time.Since(deployStart)
	if deployErr != nil {
		e.metrics.RecordDeploy(app.Name, "registry", "failure", deployDur)
		for _, img := range changedImages {
			appState.LastFailedDigests[img] = "failed"
		}

		autoRollback := app.IsAutoRollbackEnabled(e.cfg.Defaults.IsAutoRollbackEnabled())
		if autoRollback {
			e.logger.Error("Deployment failed health checks after image update; triggering rollback", "app", app.Name, "error", deployErr)
			if rbErr := e.composeService.Rollback(ctx, deployApp, e.cfg.Defaults.ComposeCommand); rbErr != nil {
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and rollback failed: %w", deployErr, rbErr)
			}

			appState.LastRollback = &RollbackEvent{
				Timestamp: time.Now(),
				Reason:    deployErr.Error(),
				Target:    "previous container state",
			}
			e.metrics.RecordRollback(app.Name)
			e.saveState()
			return false, fmt.Errorf("deployment failed healthcheck: %v (automatically rolled back to previous state)", deployErr)
		}

		e.saveState()
		return false, fmt.Errorf("compose deployment failed after registry update: %w", deployErr)
	}

	for _, img := range changedImages {
		remoteDigest, err := e.registryChecker.GetRemoteDigest(ctx, img, app.Registry.Auth)
		if err == nil {
			appState.ImageDigests[img] = remoteDigest
		}
		delete(appState.LastFailedDigests, img)
	}

	appState.LastDeployed = time.Now()
	e.metrics.RecordDeploy(app.Name, "registry", "success", deployDur)
	e.saveState()

	return true, nil
}

// CheckAndDeployOCI checks if an OCI config/artifact digest changed, pulls it into WorkDir, deploys, and rolls back on failure.
func (e *Engine) CheckAndDeployOCI(ctx context.Context, app *config.AppConfig) (bool, error) {
	if app.OCI == nil || app.OCI.Artifact == "" {
		return false, nil
	}

	puller, ok := e.registryChecker.(registry.ArtifactPuller)
	if !ok {
		return false, fmt.Errorf("registry checker does not support OCI artifact pulling")
	}

	lock := e.appLocks[app.Name]
	lock.Lock()
	defer lock.Unlock()

	appState := e.getAppState(app.Name)

	remoteDigest, err := e.registryChecker.GetRemoteDigest(ctx, app.OCI.Artifact, app.OCI.Auth)
	if err != nil {
		e.metrics.RecordPollError(app.Name, "oci")
		return false, fmt.Errorf("failed to check remote OCI artifact digest for %s: %w", app.Name, err)
	}

	if appState.LastOCIDigest == remoteDigest {
		e.logger.Debug("OCI artifact up to date", "app", app.Name, "digest", remoteDigest)
		return false, nil
	}

	if appState.LastFailedOCIDigest == remoteDigest {
		e.logger.Debug("Skipping OCI artifact digest that previously failed health check", "app", app.Name, "digest", remoteDigest)
		return false, nil
	}

	e.logger.Info("New OCI artifact digest detected", "app", app.Name, "artifact", app.OCI.Artifact, "old", appState.LastOCIDigest, "new", remoteDigest)
	previousDigest := appState.LastOCIDigest

	pulledDigest, err := puller.PullArtifact(ctx, app.OCI.Artifact, app.OCI.Auth, app.WorkDir)
	if err != nil {
		return false, fmt.Errorf("failed to pull OCI artifact for %s: %w", app.Name, err)
	}

	deployApp := appWithEffectiveWorkDir(app)
	deployStart := time.Now()
	deployErr := e.composeService.Deploy(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
	deployDur := time.Since(deployStart)
	if deployErr != nil {
		e.metrics.RecordDeploy(app.Name, "oci", "failure", deployDur)
		appState.LastFailedOCIDigest = remoteDigest

		autoRollback := app.IsAutoRollbackEnabled(e.cfg.Defaults.IsAutoRollbackEnabled())
		if autoRollback && previousDigest != "" {
			e.logger.Error("OCI deployment failed health checks; triggering automated rollback", "app", app.Name, "failed_digest", remoteDigest, "rollback_digest", previousDigest, "error", deployErr)

			pinnedRef, pinErr := registry.PinDigest(app.OCI.Artifact, previousDigest)
			if pinErr != nil {
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and OCI rollback pin failed: %w", deployErr, pinErr)
			}
			if _, rbPullErr := puller.PullArtifact(ctx, pinnedRef, app.OCI.Auth, app.WorkDir); rbPullErr != nil {
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and OCI rollback pull failed: %w", deployErr, rbPullErr)
			}
			if rbErr := e.composeService.Rollback(ctx, deployApp, e.cfg.Defaults.ComposeCommand); rbErr != nil {
				e.saveState()
				return false, fmt.Errorf("deploy failed (%v) and rollback failed: %w", deployErr, rbErr)
			}

			appState.LastRollback = &RollbackEvent{
				Timestamp: time.Now(),
				Reason:    deployErr.Error(),
				Target:    previousDigest,
			}
			e.metrics.RecordRollback(app.Name)
			e.saveState()
			return false, fmt.Errorf("deployment failed healthcheck: %v (automatically rolled back to OCI digest %s)", deployErr, previousDigest)
		}

		e.saveState()
		return false, fmt.Errorf("deployment failed after OCI artifact update: %w", deployErr)
	}

	if pulledDigest != "" {
		remoteDigest = pulledDigest
	}
	appState.LastOCIDigest = remoteDigest
	appState.LastFailedOCIDigest = ""
	appState.LastDeployed = time.Now()
	e.metrics.RecordDeploy(app.Name, "oci", "success", deployDur)
	e.saveState()
	return true, nil
}

// SyncApp forces a git sync (if configured) and deployment for an app.
func (e *Engine) SyncApp(ctx context.Context, appName string) error {
	var targetApp *config.AppConfig
	for i := range e.cfg.Apps {
		if e.cfg.Apps[i].Name == appName {
			targetApp = &e.cfg.Apps[i]
			break
		}
	}

	if targetApp == nil {
		return fmt.Errorf("app '%s' not found in config", appName)
	}

	lock := e.appLocks[appName]
	lock.Lock()
	defer lock.Unlock()

	appState := e.getAppState(appName)
	// Clear any previous failure block on manual sync
	appState.LastFailedCommit = ""
	appState.LastFailedOCIDigest = ""
	appState.LastFailedDigests = make(map[string]string)

	previousCommit := appState.LastGitCommit
	previousOCIDigest := appState.LastOCIDigest
	var syncedCommit string
	var pulledOCIDigest string

	targetRepoDir := targetApp.WorkDir
	deployApp := appWithEffectiveWorkDir(targetApp)
	if targetApp.Git != nil {
		commit, err := e.gitSyncer.Sync(ctx, targetRepoDir, targetApp.Git)
		if err != nil {
			return fmt.Errorf("git sync failed: %w", err)
		}
		syncedCommit = commit
	} else if targetApp.OCI != nil {
		puller, ok := e.registryChecker.(registry.ArtifactPuller)
		if !ok {
			return fmt.Errorf("registry checker does not support OCI artifact pulling")
		}
		digest, err := puller.PullArtifact(ctx, targetApp.OCI.Artifact, targetApp.OCI.Auth, targetApp.WorkDir)
		if err != nil {
			return fmt.Errorf("OCI artifact pull failed: %w", err)
		}
		pulledOCIDigest = digest
	}

	deployStart := time.Now()
	deployErr := e.composeService.Deploy(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
	deployDur := time.Since(deployStart)
	if deployErr != nil {
		e.metrics.RecordDeploy(appName, "manual", "failure", deployDur)
		autoRollback := targetApp.IsAutoRollbackEnabled(e.cfg.Defaults.IsAutoRollbackEnabled())
		if autoRollback && previousCommit != "" && targetApp.Git != nil {
			e.logger.Error("Manual sync failed health check; triggering automated rollback", "app", appName, "rollback_commit", previousCommit, "error", deployErr)
			_ = e.gitSyncer.Rollback(ctx, targetRepoDir, targetApp.Git, previousCommit)
			_ = e.composeService.Rollback(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
			appState.LastRollback = &RollbackEvent{
				Timestamp: time.Now(),
				Reason:    deployErr.Error(),
				Target:    previousCommit,
			}
			e.metrics.RecordRollback(appName)
			e.saveState()
			return fmt.Errorf("deployment failed healthcheck: %v (automatically rolled back to commit %s)", deployErr, previousCommit)
		}
		if autoRollback && previousOCIDigest != "" && targetApp.OCI != nil {
			if puller, ok := e.registryChecker.(registry.ArtifactPuller); ok {
				if pinnedRef, pinErr := registry.PinDigest(targetApp.OCI.Artifact, previousOCIDigest); pinErr == nil {
					_, _ = puller.PullArtifact(ctx, pinnedRef, targetApp.OCI.Auth, targetApp.WorkDir)
					_ = e.composeService.Rollback(ctx, deployApp, e.cfg.Defaults.ComposeCommand)
					appState.LastRollback = &RollbackEvent{
						Timestamp: time.Now(),
						Reason:    deployErr.Error(),
						Target:    previousOCIDigest,
					}
					e.metrics.RecordRollback(appName)
					e.saveState()
					return fmt.Errorf("deployment failed healthcheck: %v (automatically rolled back to OCI digest %s)", deployErr, previousOCIDigest)
				}
			}
		}
		return fmt.Errorf("compose deploy failed: %w", deployErr)
	}

	if syncedCommit != "" {
		appState.LastGitCommit = syncedCommit
	}
	if pulledOCIDigest != "" {
		appState.LastOCIDigest = pulledOCIDigest
	}
	appState.LastDeployed = time.Now()
	e.metrics.RecordDeploy(appName, "manual", "success", deployDur)
	e.saveState()
	return nil
}

// Start runs background watchers and the HTTP server until the context is cancelled.
func (e *Engine) Start(ctx context.Context) error {
	e.logger.Info("Starting DockerWorks engine", "apps_count", len(e.cfg.Apps))

	var wg sync.WaitGroup

	// 1. Optional HTTP Webhook & Health server
	if e.cfg.Server.Enabled {
		srv := server.NewServer(&e.cfg.Server, e.cfg.Apps, func(appName string) error {
			return e.SyncApp(ctx, appName)
		}, e.logger)
		srv.RegisterMetrics(e.metrics)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.Start(); err != nil {
				e.logger.Error("HTTP server error", "error", err)
			}
		}()

		go func() {
			<-ctx.Done()
			_ = srv.Close()
		}()
	}

	// 2. Watcher loops per app
	for i := range e.cfg.Apps {
		app := &e.cfg.Apps[i]
		if !app.IsEnabled() {
			continue
		}

		// Git watcher loop
		if app.Git != nil {
			wg.Add(1)
			go func(a *config.AppConfig) {
				defer wg.Done()
				ticker := time.NewTicker(a.Git.PollInterval)
				defer ticker.Stop()

				// Initial check immediately
				if _, err := e.CheckAndDeployGit(ctx, a); err != nil {
					e.logger.Error("Initial Git check failed", "app", a.Name, "error", err)
				}

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, err := e.CheckAndDeployGit(ctx, a); err != nil {
							e.logger.Error("Git check failed", "app", a.Name, "error", err)
						}
					}
				}
			}(app)
		}

		// Registry watcher loop
		if app.Registry != nil {
			wg.Add(1)
			go func(a *config.AppConfig) {
				defer wg.Done()
				ticker := time.NewTicker(a.Registry.PollInterval)
				defer ticker.Stop()

				// Initial check immediately
				if _, err := e.CheckAndDeployRegistry(ctx, a); err != nil {
					e.logger.Error("Initial Registry check failed", "app", a.Name, "error", err)
				}

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, err := e.CheckAndDeployRegistry(ctx, a); err != nil {
							e.logger.Error("Registry check failed", "app", a.Name, "error", err)
						}
					}
				}
			}(app)
		}

		// OCI artifact watcher loop
		if app.OCI != nil {
			wg.Add(1)
			go func(a *config.AppConfig) {
				defer wg.Done()
				ticker := time.NewTicker(a.OCI.PollInterval)
				defer ticker.Stop()

				if _, err := e.CheckAndDeployOCI(ctx, a); err != nil {
					e.logger.Error("Initial OCI artifact check failed", "app", a.Name, "error", err)
				}

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, err := e.CheckAndDeployOCI(ctx, a); err != nil {
							e.logger.Error("OCI artifact check failed", "app", a.Name, "error", err)
						}
					}
				}
			}(app)
		}
	}

	<-ctx.Done()
	e.logger.Info("Shutting down DockerWorks engine...")
	wg.Wait()
	return nil
}
