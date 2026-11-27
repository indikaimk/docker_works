# CLI Reference

```text
docker-works <command> [flags]
```

## Commands

### `daemon`

Runs the background GitOps watcher, registry digest watcher, and optional HTTP webhook server.

```bash
docker-works daemon --config /etc/docker-works/config.yaml --log-level info
```

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--config` | *(auto-discovered)* | Path to YAML configuration file. |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error`. |

### `sync`

Triggers an immediate synchronous deployment for one or all enabled apps and exits. When `config_git` is configured, `sync` first pulls and validates the latest remote configuration before syncing apps.

```bash
# Sync a single app
docker-works sync --config /etc/docker-works/config.yaml --app my-app

# Sync all enabled apps
docker-works sync --config /etc/docker-works/config.yaml
```

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--config` | *(auto-discovered)* | Path to YAML configuration file. |
| `--app` | `""` | Target application name (all enabled apps if omitted). |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error`. |

### `doctor`

Inspects the host environment, Git CLI, container runtime (`docker` or `podman`), Compose support, socket connectivity, and configuration validity.

```bash
# Uses defaults.runtime from config (or defaults to docker)
docker-works doctor --config /etc/docker-works/config.yaml

# Explicitly check Podman prerequisites
docker-works doctor --runtime podman
```

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--config` | *(auto-discovered)* | Path to YAML configuration file. |
| `--runtime` | *(from config or `docker`)* | Container runtime to check (`docker` or `podman`). |

### `validate`

Parses and validates a configuration file and prints a summary of configured apps.

```bash
docker-works validate --config /etc/docker-works/config.yaml
```

### `version`

Displays version, commit, and build date metadata.

```bash
docker-works version
```
