package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var validAppNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Defaults  DefaultsConfig   `yaml:"defaults"`
	ConfigGit *ConfigGitConfig `yaml:"config_git"`
	AppsDir   string           `yaml:"apps_dir"`
	Apps      []AppConfig      `yaml:"apps"`
}

type ConfigGitConfig struct {
	Repo         string        `yaml:"repo"`
	Branch       string        `yaml:"branch"`
	Path         string        `yaml:"path"`
	ConfigFile   string        `yaml:"config_file"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Auth         GitAuthConfig `yaml:"auth"`
}

type ServerConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Listen         string `yaml:"listen"`
	WebhookSecret  string `yaml:"webhook_secret"`
	MetricsEnabled bool   `yaml:"metrics_enabled"`
}

const (
	RuntimeDocker = "docker"
	RuntimePodman = "podman"

	ModeCompose = "compose"
	ModeQuadlet = "quadlet"
)

type DefaultsConfig struct {
	Runtime              string        `yaml:"runtime"`
	GitPollInterval      time.Duration `yaml:"git_poll_interval"`
	RegistryPollInterval time.Duration `yaml:"registry_poll_interval"`
	ComposeCommand       string        `yaml:"compose_command"`
	StateDir             string        `yaml:"state_dir"`
	HealthCheckTimeout   time.Duration `yaml:"health_check_timeout"`
	AutoRollback         *bool         `yaml:"auto_rollback"`
}

func (d *DefaultsConfig) IsAutoRollbackEnabled() bool {
	if d.AutoRollback == nil {
		return true
	}
	return *d.AutoRollback
}

type AppConfig struct {
	Name               string           `yaml:"name"`
	Enabled            *bool            `yaml:"enabled"`
	Mode               string           `yaml:"mode"`
	WorkDir            string           `yaml:"work_dir"`
	ComposeFiles       []string         `yaml:"compose_files"`
	EnvFiles           []string         `yaml:"env_files"`
	Quadlet            *QuadletConfig   `yaml:"quadlet"`
	Sops               *SopsConfig      `yaml:"sops"`
	Templates          *TemplatesConfig `yaml:"templates"`
	Git                *GitConfig       `yaml:"git"`
	Registry           *RegistryConfig  `yaml:"registry"`
	OCI                *OCIConfig       `yaml:"oci"`
	Webhook            *WebhookConfig   `yaml:"webhook"`
	Hooks              HooksConfig      `yaml:"hooks"`
	AutoPrune          bool             `yaml:"auto_prune"`
	HealthCheckTimeout time.Duration    `yaml:"health_check_timeout"`
	AutoRollback       *bool            `yaml:"auto_rollback"`
}

type QuadletConfig struct {
	Dir        string   `yaml:"dir"`
	SystemdDir string   `yaml:"systemd_dir"`
	Files      []string `yaml:"files"`
	Services   []string `yaml:"services"`
	UserMode   *bool    `yaml:"user_mode"`
}

func (q *QuadletConfig) IsUserMode() bool {
	if q != nil && q.UserMode != nil {
		return *q.UserMode
	}
	return os.Geteuid() != 0
}

func (a *AppConfig) IsEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

func (a *AppConfig) IsAutoRollbackEnabled(defaultVal bool) bool {
	if a.AutoRollback != nil {
		return *a.AutoRollback
	}
	return defaultVal
}

type GitConfig struct {
	Repo         string        `yaml:"repo"`
	Branch       string        `yaml:"branch"`
	Path         string        `yaml:"path"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Auth         GitAuthConfig `yaml:"auth"`
}

type GitAuthConfig struct {
	SSHKeyPath string `yaml:"ssh_key_path"`
	Token      string `yaml:"token"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
}

type RegistryConfig struct {
	WatchImages  []string                `yaml:"watch_images"`
	PollInterval time.Duration           `yaml:"poll_interval"`
	Auth         map[string]RegistryAuth `yaml:"auth"`
}

type OCIConfig struct {
	Artifact     string                  `yaml:"artifact"`
	PollInterval time.Duration           `yaml:"poll_interval"`
	Auth         map[string]RegistryAuth `yaml:"auth"`
}

type TemplatesConfig struct {
	VarsFiles []string           `yaml:"vars_files"`
	Vars      map[string]any     `yaml:"vars"`
	Files     []TemplateFileSpec `yaml:"files"`
}

type TemplateFileSpec struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type SopsConfig struct {
	Files      []SopsFileConfig `yaml:"files"`
	AgeKeyFile string           `yaml:"age_key_file"`
	AgeKey     string           `yaml:"age_key"`
}

type SopsFileConfig struct {
	Source     string `yaml:"source"`
	Target     string `yaml:"target"`
	OutputType string `yaml:"output_type"`
}

type RegistryAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
}

type WebhookConfig struct {
	Secret string `yaml:"secret"`
}

type HooksConfig struct {
	PreDeploy  string `yaml:"pre_deploy"`
	PostDeploy string `yaml:"post_deploy"`
	OnRollback string `yaml:"on_rollback"`
}

// Load reads and parses a YAML configuration file with environment variable expansion.
func Load(path string) (*Config, error) {
	if info, err := os.Stat(path); err == nil && runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm&0077 != 0 {
			slog.Warn("Configuration file has group/world permissions; consider restricting with chmod 0600", "path", path, "mode", fmt.Sprintf("%04o", perm))
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand environment variables like ${VAR} or $VAR
	expanded := os.ExpandEnv(string(data))

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if err := cfg.loadAppsDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("failed to load apps_dir: %w", err)
	}

	cfg.applyDefaults(filepath.Dir(path))
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func (c *Config) loadAppsDir(configDir string) error {
	if strings.TrimSpace(c.AppsDir) == "" {
		return nil
	}
	if strings.Contains(c.AppsDir, "..") {
		return fmt.Errorf("apps_dir %q must not contain '..'", c.AppsDir)
	}
	dir := c.AppsDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(configDir, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read apps_dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read app config %s: %w", filePath, err)
		}
		expanded := os.ExpandEnv(string(raw))

		var wrapper struct {
			Apps []AppConfig `yaml:"apps"`
		}
		if err := yaml.Unmarshal([]byte(expanded), &wrapper); err == nil && len(wrapper.Apps) > 0 {
			c.Apps = append(c.Apps, wrapper.Apps...)
			continue
		}

		var single AppConfig
		if err := yaml.Unmarshal([]byte(expanded), &single); err != nil {
			return fmt.Errorf("failed to parse app config %s: %w", filePath, err)
		}
		c.Apps = append(c.Apps, single)
	}
	return nil
}

func (c *Config) applyDefaults(configDir string) {
	if c.Server.Listen == "" {
		c.Server.Listen = "127.0.0.1:8080"
	}

	c.Defaults.Runtime = strings.ToLower(strings.TrimSpace(c.Defaults.Runtime))
	if c.Defaults.Runtime == "" {
		c.Defaults.Runtime = RuntimeDocker
	}

	if c.Defaults.GitPollInterval <= 0 {
		c.Defaults.GitPollInterval = 2 * time.Minute
	}
	if c.Defaults.RegistryPollInterval <= 0 {
		c.Defaults.RegistryPollInterval = 5 * time.Minute
	}
	if c.Defaults.ComposeCommand == "" {
		if c.Defaults.Runtime == RuntimePodman {
			c.Defaults.ComposeCommand = "podman compose"
		} else {
			c.Defaults.ComposeCommand = "docker compose"
		}
	}
	if c.Defaults.StateDir == "" {
		c.Defaults.StateDir = "/var/lib/docker-works"
	}
	if c.Defaults.HealthCheckTimeout <= 0 {
		c.Defaults.HealthCheckTimeout = 60 * time.Second
	}
	if c.Defaults.AutoRollback == nil {
		defaultRollback := true
		c.Defaults.AutoRollback = &defaultRollback
	}

	if c.ConfigGit != nil {
		if c.ConfigGit.Branch == "" {
			c.ConfigGit.Branch = "main"
		}
		if c.ConfigGit.Path == "" {
			c.ConfigGit.Path = "."
		} else {
			c.ConfigGit.Path = filepath.Clean(c.ConfigGit.Path)
		}
		if c.ConfigGit.ConfigFile == "" {
			c.ConfigGit.ConfigFile = "config.yaml"
		}
		if c.ConfigGit.PollInterval <= 0 {
			c.ConfigGit.PollInterval = c.Defaults.GitPollInterval
		}
	}

	for i := range c.Apps {
		app := &c.Apps[i]

		app.Mode = strings.ToLower(strings.TrimSpace(app.Mode))
		if app.Mode == "" {
			if app.Quadlet != nil {
				app.Mode = ModeQuadlet
			} else {
				app.Mode = ModeCompose
			}
		}

		if app.Mode == ModeQuadlet {
			if app.Quadlet == nil {
				app.Quadlet = &QuadletConfig{}
			}
			if app.Quadlet.UserMode == nil {
				userMode := os.Geteuid() != 0
				app.Quadlet.UserMode = &userMode
			}
			if app.Quadlet.Dir == "" {
				if *app.Quadlet.UserMode {
					app.Quadlet.Dir = defaultUserQuadletDir()
				} else {
					app.Quadlet.Dir = "/etc/containers/systemd"
				}
			}
			if app.Quadlet.SystemdDir == "" {
				if *app.Quadlet.UserMode {
					app.Quadlet.SystemdDir = defaultUserSystemdDir()
				} else {
					app.Quadlet.SystemdDir = "/etc/systemd/system"
				}
			}
		} else if len(app.ComposeFiles) == 0 {
			app.ComposeFiles = []string{"docker-compose.yml"}
		}

		if app.WorkDir == "" {
			app.WorkDir = filepath.Join(c.Defaults.StateDir, "apps", app.Name)
		} else if !filepath.IsAbs(app.WorkDir) {
			app.WorkDir = filepath.Join(configDir, app.WorkDir)
		}

		if app.HealthCheckTimeout <= 0 {
			app.HealthCheckTimeout = c.Defaults.HealthCheckTimeout
		}

		if app.AutoRollback == nil {
			app.AutoRollback = c.Defaults.AutoRollback
		}

		if app.Git != nil {
			if app.Git.Branch == "" {
				app.Git.Branch = "main"
			}
			if app.Git.Path == "" {
				app.Git.Path = "."
			} else {
				app.Git.Path = filepath.Clean(app.Git.Path)
			}
			if app.Git.PollInterval <= 0 {
				app.Git.PollInterval = c.Defaults.GitPollInterval
			}
		}

		if app.Registry != nil {
			if app.Registry.PollInterval <= 0 {
				app.Registry.PollInterval = c.Defaults.RegistryPollInterval
			}
		}

		if app.OCI != nil {
			if app.OCI.PollInterval <= 0 {
				app.OCI.PollInterval = c.Defaults.RegistryPollInterval
			}
		}

		if app.Templates != nil {
			for j := range app.Templates.Files {
				tf := &app.Templates.Files[j]
				tf.Source = strings.TrimSpace(tf.Source)
				tf.Target = strings.TrimSpace(tf.Target)
				if tf.Target == "" && tf.Source != "" {
					tf.Target = defaultTemplateTarget(tf.Source)
				}
			}
		}

		if app.Sops != nil {
			for j := range app.Sops.Files {
				sf := &app.Sops.Files[j]
				sf.Source = strings.TrimSpace(sf.Source)
				sf.Target = strings.TrimSpace(sf.Target)
				sf.OutputType = strings.ToLower(strings.TrimSpace(sf.OutputType))
				if sf.Target == "" && sf.Source != "" {
					sf.Target = defaultSopsTarget(sf.Source)
				}
			}
		}
	}
}

func (c *Config) Validate() error {
	if c.Defaults.Runtime != RuntimeDocker && c.Defaults.Runtime != RuntimePodman {
		return fmt.Errorf("defaults.runtime must be '%s' or '%s', got %q", RuntimeDocker, RuntimePodman, c.Defaults.Runtime)
	}

	if c.ConfigGit != nil {
		if strings.TrimSpace(c.ConfigGit.Repo) == "" {
			return fmt.Errorf("config_git.repo must not be empty")
		}
		if strings.HasPrefix(strings.TrimSpace(c.ConfigGit.Repo), "-") {
			return fmt.Errorf("config_git.repo must not start with '-'")
		}
		if strings.HasPrefix(strings.TrimSpace(c.ConfigGit.Branch), "-") {
			return fmt.Errorf("config_git.branch must not start with '-'")
		}
		if filepath.IsAbs(c.ConfigGit.Path) || strings.Contains(c.ConfigGit.Path, "..") {
			return fmt.Errorf("config_git.path %q must be a relative path without '..'", c.ConfigGit.Path)
		}
		if filepath.IsAbs(c.ConfigGit.ConfigFile) || strings.Contains(c.ConfigGit.ConfigFile, "..") {
			return fmt.Errorf("config_git.config_file %q must be a relative path without '..'", c.ConfigGit.ConfigFile)
		}
	}

	names := make(map[string]struct{})

	for i, app := range c.Apps {
		if strings.TrimSpace(app.Name) == "" {
			return fmt.Errorf("app[%d] must have a non-empty name", i)
		}
		if !validAppNameRe.MatchString(app.Name) || strings.Contains(app.Name, "..") {
			return fmt.Errorf("app %q has invalid name: must match [a-zA-Z0-9][a-zA-Z0-9._-]* without '..'", app.Name)
		}
		if _, exists := names[app.Name]; exists {
			return fmt.Errorf("duplicate app name: %s", app.Name)
		}
		names[app.Name] = struct{}{}

		if app.Mode != ModeCompose && app.Mode != ModeQuadlet {
			return fmt.Errorf("app %s has unsupported mode %q (expected '%s' or '%s')", app.Name, app.Mode, ModeCompose, ModeQuadlet)
		}
		if app.Mode == ModeQuadlet && c.Defaults.Runtime != RuntimePodman {
			return fmt.Errorf("app %s uses mode '%s', which requires defaults.runtime: '%s'", app.Name, ModeQuadlet, RuntimePodman)
		}
		if app.Mode == ModeQuadlet && app.Quadlet != nil {
			for _, qf := range app.Quadlet.Files {
				if filepath.IsAbs(qf) || strings.Contains(qf, "..") {
					return fmt.Errorf("app %s quadlet file %q must be a relative path without '..'", app.Name, qf)
				}
			}
		}

		if app.Git == nil && app.Registry == nil && app.OCI == nil {
			return fmt.Errorf("app %s must specify at least one watcher: 'git', 'registry', or 'oci'", app.Name)
		}

		if app.OCI != nil && strings.TrimSpace(app.OCI.Artifact) == "" {
			return fmt.Errorf("app %s has oci config but artifact is empty", app.Name)
		}

		if app.Templates != nil {
			for j, vf := range app.Templates.VarsFiles {
				if strings.TrimSpace(vf) == "" || strings.Contains(vf, "..") {
					return fmt.Errorf("app %s templates.vars_files[%d] %q is invalid", app.Name, j, vf)
				}
			}
			for j, tf := range app.Templates.Files {
				if tf.Source == "" {
					return fmt.Errorf("app %s templates.files[%d].source must not be empty", app.Name, j)
				}
				if filepath.IsAbs(tf.Source) || strings.Contains(tf.Source, "..") {
					return fmt.Errorf("app %s templates.files[%d].source %q must be a relative path without '..'", app.Name, j, tf.Source)
				}
				if tf.Target == "" || tf.Target == tf.Source {
					return fmt.Errorf("app %s templates.files[%d].target must be specified when source %q does not end in .tmpl, .tpl, or .gotmpl", app.Name, j, tf.Source)
				}
				if filepath.IsAbs(tf.Target) || strings.Contains(tf.Target, "..") {
					return fmt.Errorf("app %s templates.files[%d].target %q must be a relative path without '..'", app.Name, j, tf.Target)
				}
			}
		}

		if app.Sops != nil {
			if strings.ContainsAny(app.Sops.AgeKeyFile, "\x00\r\n") {
				return fmt.Errorf("app %s sops.age_key_file contains invalid control characters", app.Name)
			}
			for j, sf := range app.Sops.Files {
				if sf.Source == "" {
					return fmt.Errorf("app %s sops.files[%d].source must not be empty", app.Name, j)
				}
				if filepath.IsAbs(sf.Source) || strings.Contains(sf.Source, "..") {
					return fmt.Errorf("app %s sops.files[%d].source %q must be a relative path without '..'", app.Name, j, sf.Source)
				}
				if filepath.IsAbs(sf.Target) || strings.Contains(sf.Target, "..") {
					return fmt.Errorf("app %s sops.files[%d].target %q must be a relative path without '..'", app.Name, j, sf.Target)
				}
				switch sf.OutputType {
				case "", "dotenv", "yaml", "json", "binary":
				default:
					return fmt.Errorf("app %s sops.files[%d].output_type %q must be dotenv, yaml, json, or binary", app.Name, j, sf.OutputType)
				}
			}
		}

		if app.Git != nil {
			if strings.TrimSpace(app.Git.Repo) == "" {
				return fmt.Errorf("app %s has git config but repo is empty", app.Name)
			}
			if strings.HasPrefix(strings.TrimSpace(app.Git.Repo), "-") {
				return fmt.Errorf("app %s git.repo must not start with '-'", app.Name)
			}
			if strings.HasPrefix(strings.TrimSpace(app.Git.Branch), "-") {
				return fmt.Errorf("app %s git.branch must not start with '-'", app.Name)
			}
			if filepath.IsAbs(app.Git.Path) || strings.Contains(app.Git.Path, "..") {
				return fmt.Errorf("app %s git.path %q must be a relative path without '..'", app.Name, app.Git.Path)
			}
		}

		if app.Registry != nil && len(app.Registry.WatchImages) == 0 {
			return fmt.Errorf("app %s has registry config but watch_images is empty", app.Name)
		}
	}

	return nil
}

func defaultUserQuadletDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "containers", "systemd")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "containers", "systemd")
	}
	return "/etc/containers/systemd"
}

func defaultUserSystemdDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "systemd", "user")
	}
	return "/etc/systemd/system"
}

func defaultTemplateTarget(source string) string {
	for _, ext := range []string{".tmpl", ".tpl", ".gotmpl"} {
		if strings.HasSuffix(source, ext) {
			return strings.TrimSuffix(source, ext)
		}
	}
	return ""
}

func defaultSopsTarget(source string) string {
	if strings.Contains(source, ".enc.") {
		return strings.Replace(source, ".enc.", ".", 1)
	}
	if strings.HasSuffix(source, ".enc") {
		return strings.TrimSuffix(source, ".enc")
	}
	return ".env"
}
