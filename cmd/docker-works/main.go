package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/indikaimk/docker_works/internal/compose"
	"github.com/indikaimk/docker_works/internal/config"
	"github.com/indikaimk/docker_works/internal/engine"
	"github.com/indikaimk/docker_works/internal/gitops"
	"github.com/indikaimk/docker_works/internal/registry"
)

var (
	Version   = "0.1.0"
	GitCommit = "dev"
	BuildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "daemon":
		runDaemon(os.Args[2:])
	case "sync":
		runSync(os.Args[2:])
	case "validate":
		runValidate(os.Args[2:])
	case "doctor":
		runDoctor(os.Args[2:])
	case "version":
		fmt.Printf("DockerWorks version %s (%s, %s)\n", Version, GitCommit, BuildDate)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`DockerWorks - GitOps & Auto-Update Daemon for Docker Compose

Usage:
  docker-works <command> [flags]

Commands:
  daemon     Run the background watcher and webhook service
  sync       Perform an immediate sync and deployment
  validate   Validate a configuration file
  doctor     Inspect host environment, Docker, and Compose installation
  version    Show version information
  help       Show this help message

Use "docker-works <command> --help" for more information about a command.`)
}

func findDefaultConfig(specified string) string {
	if specified != "" {
		return specified
	}
	candidates := []string{
		"config.yaml",
		"config.yml",
		"/etc/docker-works/config.yaml",
		"/etc/docker-works/config.yml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "config.yaml"
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file")
	logLevel := fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	_ = fs.Parse(args)

	logger := setupLogger(*logLevel)
	resolvedPath := findDefaultConfig(*configPath)

	logger.Info("Loading configuration", "path", resolvedPath)
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := &compose.DefaultCommandRunner{}
	composeSvc := compose.NewService(runner, logger)
	gitSyncer := gitops.NewSyncer(runner, logger)
	regChecker := registry.NewRemoteChecker(logger)

	eng := engine.NewEngine(cfg, composeSvc, gitSyncer, regChecker, logger)

	logger.Info("Starting DockerWorks daemon", "version", Version)
	if err := eng.Start(ctx); err != nil {
		logger.Error("Daemon encountered error", "error", err)
		os.Exit(1)
	}
}

func runSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file")
	appName := fs.String("app", "", "Target application name (all if not specified)")
	logLevel := fs.String("log-level", "info", "Log level (debug, info, warn, error)")
	_ = fs.Parse(args)

	logger := setupLogger(*logLevel)
	resolvedPath := findDefaultConfig(*configPath)

	cfg, err := config.Load(resolvedPath)
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &compose.DefaultCommandRunner{}
	composeSvc := compose.NewService(runner, logger)
	gitSyncer := gitops.NewSyncer(runner, logger)
	regChecker := registry.NewRemoteChecker(logger)
	eng := engine.NewEngine(cfg, composeSvc, gitSyncer, regChecker, logger)

	if *appName != "" {
		logger.Info("Triggering manual sync", "app", *appName)
		if err := eng.SyncApp(ctx, *appName); err != nil {
			logger.Error("Sync failed", "app", *appName, "error", err)
			os.Exit(1)
		}
		logger.Info("Sync completed successfully", "app", *appName)
		return
	}

	// Sync all apps
	for _, app := range cfg.Apps {
		if !app.IsEnabled() {
			continue
		}
		logger.Info("Triggering manual sync", "app", app.Name)
		if err := eng.SyncApp(ctx, app.Name); err != nil {
			logger.Error("Sync failed", "app", app.Name, "error", err)
			os.Exit(1)
		}
	}
	logger.Info("All apps synced successfully")
}

func runValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file")
	_ = fs.Parse(args)

	resolvedPath := findDefaultConfig(*configPath)
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config validation FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config '%s' is VALID (%d apps configured):\n", resolvedPath, len(cfg.Apps))
	for _, app := range cfg.Apps {
		watchers := []string{}
		if app.Git != nil {
			watchers = append(watchers, fmt.Sprintf("git: %s#%s", app.Git.Repo, app.Git.Branch))
		}
		if app.Registry != nil {
			watchers = append(watchers, fmt.Sprintf("registry: %d images", len(app.Registry.WatchImages)))
		}
		if app.OCI != nil {
			watchers = append(watchers, fmt.Sprintf("oci: %s", app.OCI.Artifact))
		}
		fmt.Printf("  - %s (work_dir: %s, watchers: %v)\n", app.Name, app.WorkDir, watchers)
	}
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file")
	runtimeFlag := fs.String("runtime", "", "Container runtime to check (docker or podman)")
	_ = fs.Parse(args)

	fmt.Println("DockerWorks Environment Check (Doctor)")
	fmt.Println("======================================")

	allPass := true

	// Resolve configuration early so we know which runtime to inspect
	resolvedPath := findDefaultConfig(*configPath)
	var loadedCfg *config.Config
	var cfgLoadErr error
	_, cfgStatErr := os.Stat(resolvedPath)
	if cfgStatErr == nil {
		loadedCfg, cfgLoadErr = config.Load(resolvedPath)
	}

	containerRuntime := strings.ToLower(strings.TrimSpace(*runtimeFlag))
	if containerRuntime == "" && loadedCfg != nil {
		containerRuntime = loadedCfg.Defaults.Runtime
	}
	if containerRuntime == "" {
		containerRuntime = config.RuntimeDocker
	}

	// 1. OS & Architecture
	fmt.Printf("[✓] OS / Architecture: %s / %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("[i] Container Runtime: %s\n", containerRuntime)

	// 2. Git CLI
	gitOut, err := exec.Command("git", "--version").Output()
	if err == nil {
		fmt.Printf("[✓] Git CLI:           %s", string(gitOut))
	} else {
		fmt.Println("[✗] Git CLI:           Not found on PATH")
		allPass = false
	}

	if containerRuntime == config.RuntimePodman {
		// 3. Podman CLI
		podmanOut, err := exec.Command("podman", "--version").Output()
		if err == nil {
			fmt.Printf("[✓] Podman CLI:        %s", string(podmanOut))
		} else {
			fmt.Println("[✗] Podman CLI:        Not found on PATH")
			fmt.Println("    -> Install Podman using your distribution package manager (e.g. apt/dnf install podman)")
			allPass = false
		}

		// 4. Podman Compose
		composeOut, err := exec.Command("podman", "compose", "version").Output()
		if err == nil {
			fmt.Printf("[✓] Podman Compose:    %s", string(composeOut))
		} else {
			fmt.Println("[✗] Podman Compose:    'podman compose' failed or compose provider not found")
			fmt.Println("    -> Ensure podman 4.1+ and an external compose provider (e.g. docker-compose) are installed")
			allPass = false
		}

		// 5. Podman Engine & Socket
		infoOut, err := exec.Command("podman", "info", "--format", "{{.Version.Version}}").Output()
		if err == nil {
			fmt.Printf("[✓] Podman Engine:     Ready (Version %s)\n", strings.TrimSpace(string(infoOut)))
		} else {
			fmt.Println("[!] Podman Engine:     Cannot query 'podman info'")
			fmt.Println("    -> For rootful:  sudo systemctl enable --now podman.socket")
			fmt.Println("    -> For rootless: systemctl --user enable --now podman.socket")
		}
	} else {
		// 3. Docker CLI
		dockerOut, err := exec.Command("docker", "--version").Output()
		if err == nil {
			fmt.Printf("[✓] Docker CLI:        %s", string(dockerOut))
		} else {
			fmt.Println("[✗] Docker CLI:        Not found on PATH")
			fmt.Println("    -> Install Docker using: sudo ./scripts/setup-docker.sh (Ubuntu, Debian, Fedora)")
			allPass = false
		}

		// 4. Docker Compose Plugin
		composeOut, err := exec.Command("docker", "compose", "version").Output()
		if err == nil {
			fmt.Printf("[✓] Docker Compose:    %s", string(composeOut))
		} else {
			fmt.Println("[✗] Docker Compose:    'docker compose' plugin not found")
			fmt.Println("    -> Install Compose using: sudo ./scripts/setup-docker.sh (Ubuntu, Debian, Fedora)")
			allPass = false
		}

		// 5. Docker Daemon Socket Connectivity
		infoOut, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Output()
		if err == nil {
			fmt.Printf("[✓] Docker Daemon:     Connected (Server Version %s)\n", strings.TrimSpace(string(infoOut)))
		} else {
			fmt.Println("[!] Docker Daemon:     Cannot communicate with Docker daemon socket (/var/run/docker.sock)")
			fmt.Println("    -> If service is stopped, run: sudo systemctl enable --now docker")
			fmt.Println("    -> If permission denied, add user to group: sudo usermod -aG docker $USER")
		}
	}

	// 6. Configuration Check
	if cfgStatErr == nil {
		if cfgLoadErr == nil {
			fmt.Printf("[✓] Configuration:     Valid (%s, %d apps configured)\n", resolvedPath, len(loadedCfg.Apps))
		} else {
			fmt.Printf("[✗] Configuration:     Invalid syntax in %s: %v\n", resolvedPath, cfgLoadErr)
			allPass = false
		}
	} else {
		fmt.Printf("[-] Configuration:     No config file found at %s (run validate once created)\n", resolvedPath)
	}

	fmt.Println("======================================")
	if allPass {
		fmt.Println("Status: Host environment is ready for DockerWorks.")
	} else {
		fmt.Println("Status: Some requirements are missing. Review items marked with [✗].")
	}
}

func setupLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}
