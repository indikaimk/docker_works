# Configuration

DockerWorks is configured via a YAML file (default search order: `config.yaml`, `config.yml`, `/etc/docker-works/config.yaml`, `/etc/docker-works/config.yml`). Environment variables such as `${GITHUB_TOKEN}` or `$VAR` are expanded when loading the file.

## Full Example

```yaml
server:
  enabled: true
  listen: "127.0.0.1:8080"
  webhook_secret: "secret-token"

defaults:
  runtime: "docker"                 # "docker" (default) or "podman"
  git_poll_interval: "2m"
  registry_poll_interval: "5m"
  compose_command: "docker compose" # Defaults to "podman compose" when runtime is "podman"
  state_dir: "/var/lib/docker-works"
  health_check_timeout: "60s"
  auto_rollback: true

apps:
  - name: "my-web-service"
    enabled: true
    work_dir: "/var/lib/docker-works/apps/my-web-service"
    compose_files:
      - "docker-compose.yml"
    env_files:
      - "/etc/docker-works/secrets/my-web-service.env"
    git:
      repo: "https://github.com/myorg/infra.git"
      branch: "main"
      path: "services/web"
      poll_interval: "1m"
      auth:
        token: "${GITHUB_TOKEN}"
    hooks:
      pre_deploy: "echo 'Starting deployment...'"
      post_deploy: "docker compose exec -T web ./db-migrate.sh"
      on_rollback: "echo 'Rolled back after healthcheck failure'"
    auto_rollback: true
    auto_prune: true
```

## Reference

### `server`

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `enabled` | `bool` | `false` | Enables the HTTP server for `/healthz` and `/webhooks/{app}`. |
| `listen` | `string` | `"127.0.0.1:8080"` | Address and port to bind. |
| `webhook_secret` | `string` | `""` | Default webhook secret used when an app does not define `webhook.secret`. |
| `metrics_enabled` | `bool` | `false` | Exposes Prometheus metrics at `GET /metrics` on `server.listen`. |

### `defaults`

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `runtime` | `string` | `"docker"` | Container runtime: `"docker"` or `"podman"`. |
| `git_poll_interval` | `duration` | `"2m"` | Default polling interval for Git watchers. |
| `registry_poll_interval` | `duration` | `"5m"` | Default polling interval for registry watchers. |
| `compose_command` | `string` | `"docker compose"` / `"podman compose"` | Compose CLI invocation. Automatically defaults to `"podman compose"` when `runtime: "podman"`. |
| `state_dir` | `string` | `"/var/lib/docker-works"` | Directory for `state.json` and default app `work_dir` paths. |
| `health_check_timeout` | `duration` | `"60s"` | Maximum time to wait for containers to report healthy after `up`. |
| `auto_rollback` | `bool` | `true` | Automatically roll back on failed health checks. |

### `apps[]`

Each app must specify at least one watcher (`git` or `registry`) and a unique `name`.

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `name` | `string` | *(required)* | Unique application identifier (also used in `/webhooks/{name}`). |
| `enabled` | `bool` | `true` | Set to `false` to disable watching/syncing this app. |
| `mode` | `string` | `"compose"` | Deployment mode: `"compose"` or `"quadlet"` (auto-defaults to `"quadlet"` if `quadlet:` is set; requires `defaults.runtime: "podman"`). |
| `work_dir` | `string` | `<state_dir>/apps/<name>` | Working directory for the project (relative paths resolve from config file directory). |
| `compose_files` | `[]string` | `["docker-compose.yml"]` | Compose files passed with `-f` (used in `compose` mode). |
| `env_files` | `[]string` | `[]` | Env files passed with `--env-file` (used in `compose` mode). |
| `quadlet` | `object` | `{}` | Optional Quadlet settings (`dir`, `files`, `services`, `user_mode`) used in `quadlet` mode. See [Podman Guide](podman.md#podman-quadlets-mode-quadlet). |
| `auto_prune` | `bool` | `false` | Runs `<runtime> image prune -f` after a healthy deployment. |
| `health_check_timeout` | `duration` | `defaults.health_check_timeout` | Per-app override for health check timeout. |
| `auto_rollback` | `bool` | `defaults.auto_rollback` | Per-app override for automatic rollback. |

#### `apps[].git`

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `repo` | `string` | *(required)* | Git repository URL (HTTPS or SSH). |
| `branch` | `string` | `"main"` | Branch to track. |
| `path` | `string` | `"."` | Subdirectory inside the repository containing compose files. |
| `poll_interval` | `duration` | `defaults.git_poll_interval` | How often to run `git ls-remote`. |
| `auth.ssh_key_path` | `string` | `""` | Path to SSH private key for private repos. |
| `auth.token` | `string` | `""` | HTTPS personal access token. |
| `auth.username` / `password` | `string` | `""` | HTTPS basic credentials. |

#### `apps[].registry`

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `watch_images` | `[]string` | *(required)* | Image references to monitor (e.g. `ghcr.io/myorg/api:latest`). |
| `poll_interval` | `duration` | `defaults.registry_poll_interval` | How often to inspect remote manifest digests. |
| `auth.<domain>` | `object` | `{}` | Optional per-registry credentials (`token` or `username`/`password`). Falls back to `~/.docker/config.json` and Podman `auth.json`. |

#### `apps[].oci` *(Gitless Config Bundles)*

Pulls Compose, Quadlet, and config files directly from an OCI registry (pushed via `oras push` or as an OCI config image) into `work_dir`, enabling a completely gitless workflow with digest-pinned rollbacks.

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `artifact` | `string` | *(required)* | OCI artifact reference (e.g. `ghcr.io/myorg/my-app-config:latest`). |
| `poll_interval` | `duration` | `defaults.registry_poll_interval` | How often to inspect the remote artifact manifest digest. |
| `auth.<domain>` | `object` | `{}` | Optional per-registry credentials (`token` or `username`/`password`). Falls back to `~/.docker/config.json` and Podman `auth.json`. |

#### `apps[].hooks`

Shell commands executed via `/bin/sh -c` in `work_dir` with `DOCKER_WORKS_APP=<name>`:

- `pre_deploy`: Runs before `pull` and `up`.
- `post_deploy`: Runs only after containers pass the health check.
- `on_rollback`: Runs when a deployment fails health checks and triggers a rollback.

#### `apps[].sops` *(Encrypted Secrets in Git / OCI)*

Decrypts [SOPS](https://github.com/getsops/sops)-encrypted files into `work_dir` with `0600` permissions before `pre_deploy` and container startup (and re-decrypts on rollback). Requires the `sops` CLI on `PATH`. See [Secrets Management](secrets.md) for full `age` and AWS KMS workflows.

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `age_key` | `string` | `""` | Inline/env-expanded `age` private key (`SOPS_AGE_KEY`), e.g. `"${SOPS_AGE_KEY}"`. |
| `age_key_file` | `string` | `""` | Path to an `age` key file (`SOPS_AGE_KEY_FILE`). |
| `files[].source` | `string` | *(required)* | Encrypted file path relative to `work_dir` (e.g. `secrets.enc.env`). |
| `files[].target` | `string` | *(auto-derived)* | Decrypted output path relative to `work_dir` (strips `.enc.` / `.enc`, or defaults to `.env`). |
| `files[].output_type` | `string` | `""` | Optional SOPS `--output-type`: `dotenv`, `yaml`, `json`, or `binary`. |
