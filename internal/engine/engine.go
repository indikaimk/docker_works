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
	"github.com/indikaimk/docker_works/internal/registry"
	"github.com/indikaimk/docker_works/internal/server"
)

type AppState struct {
	LastGitCommit string            `json:"last_git_commit"`
	ImageDigests  map[string]string `json:"image_digests"`
	LastDeployed  time.Time         `json:"last_deployed"`
}

type EngineState struct {
	Apps map[string]*AppState `json:"apps"`
}

type Engine struct {
	cfg             *config.Config
	composeService  *compose.Service
	gitSyncer       *gitops.Syncer
	registryChecker registry.DigestChecker
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

	_ = os.WriteFile(e.stateFilePath(), data, 0644)
}

func (e *Engine) getAppState(appName string) *AppState {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	appState, ok := e.state.Apps[appName]
	if !ok {
		appState = &AppState{
			ImageDigests: make(map[string]string),
		}
		e.state.Apps[appName] = appState
	}
	if appState.ImageDigests == nil {
		appState.ImageDigests = make(map[string]string)
	}
	return appState
}

// CheckAndDeployGit checks if git commit changed, updates local repo, and deploys.
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
		return false, fmt.Errorf("failed to check remote git commit for %s: %w", app.Name, err)
	}

	if appState.LastGitCommit == remoteCommit {
		e.logger.Debug("Git repo up to date", "app", app.Name, "commit", remoteCommit)
		return false, nil
	}

	e.logger.Info("New Git commit detected", "app", app.Name, "old", appState.LastGitCommit, "new", remoteCommit)

	// Sync local repository
	targetRepoDir := app.WorkDir
	if app.Git.Path != "" && app.Git.Path != "." {
		// If Compose file is in a subdirectory, the clone target is the root repo dir
		targetRepoDir = filepath.Dir(app.WorkDir)
	}

	syncedCommit, err := e.gitSyncer.Sync(ctx, targetRepoDir, app.Git)
	if err != nil {
		return false, fmt.Errorf("failed to sync repo for %s: %w", app.Name, err)
	}

	// Deploy compose
	if err := e.composeService.Deploy(ctx, app, e.cfg.Defaults.ComposeCommand); err != nil {
		return false, fmt.Errorf("compose deployment failed after git update: %w", err)
	}

	if syncedCommit == "" {
		syncedCommit = remoteCommit
	}
	appState.LastGitCommit = syncedCommit
	appState.LastDeployed = time.Now()
	e.saveState()

	return true, nil
}

// CheckAndDeployRegistry checks if any watched image has a newer digest and triggers deploy.
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
			e.logger.Warn("Failed to check remote digest", "app", app.Name, "image", imageRef, "error", err)
			continue
		}

		currentDigest := appState.ImageDigests[imageRef]
		if currentDigest == "" {
			// First time tracking this image, record initial digest without triggering unneeded redeploy
			appState.ImageDigests[imageRef] = remoteDigest
			continue
		}

		if currentDigest != remoteDigest {
			e.logger.Info("New image digest detected", "app", app.Name, "image", imageRef, "old", currentDigest, "new", remoteDigest)
			changedImages = append(changedImages, imageRef)
			appState.ImageDigests[imageRef] = remoteDigest
		}
	}

	if len(changedImages) == 0 {
		return false, nil
	}

	// Deploy compose
	if err := e.composeService.Deploy(ctx, app, e.cfg.Defaults.ComposeCommand); err != nil {
		return false, fmt.Errorf("compose deployment failed after registry update: %w", err)
	}

	appState.LastDeployed = time.Now()
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

	if targetApp.Git != nil {
		targetRepoDir := targetApp.WorkDir
		if targetApp.Git.Path != "" && targetApp.Git.Path != "." {
			targetRepoDir = filepath.Dir(targetApp.WorkDir)
		}
		commit, err := e.gitSyncer.Sync(ctx, targetRepoDir, targetApp.Git)
		if err != nil {
			return fmt.Errorf("git sync failed: %w", err)
		}
		e.getAppState(appName).LastGitCommit = commit
	}

	if err := e.composeService.Deploy(ctx, targetApp, e.cfg.Defaults.ComposeCommand); err != nil {
		return fmt.Errorf("compose deploy failed: %w", err)
	}

	e.getAppState(appName).LastDeployed = time.Now()
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
	}

	<-ctx.Done()
	e.logger.Info("Shutting down DockerWorks engine...")
	wg.Wait()
	return nil
}
