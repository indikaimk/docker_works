# DockerWorks

A lightweight, zero-dependency GitOps and auto-update daemon for **Docker Compose**, written in Go and designed to run as a native `systemd` service on Docker hosts.

---

## ✨ Features

- **🚀 Native Docker Compose Engine**: Invokes the host's `docker compose` CLI directly, supporting all compose versions, multiple compose files (`-f`), and `.env` files.
- **🔄 GitOps Mode (Pull-based)**:
  - Watches Git repositories (GitHub, GitLab, Gitea, etc.) for new commits on target branches.
  - Efficiently checks remote heads via `git ls-remote` without cloning or downloading on every poll.
  - Supports SSH deploy keys (`id_ed25519`), HTTPS tokens (Personal Access Tokens), and subdirectories within monorepos.
- **📦 Container Registry Watcher**:
  - Monitors image tags (`latest`, semver tags) on container registries (Docker Hub, GitHub Packages GHCR, AWS ECR, private registries).
  - Inspects remote manifest digests (SHA256) using fast `HEAD`/`GET` requests without pulling gigabytes of image layers.
  - Automatically triggers compose pull and redeployment when an image digest changes.
- **🛡️ Automated Rollback & Health Checks**:
  - Automatically queries container health (`docker compose ps --format json`) post-deployment.
  - If any container crashes or fails its healthcheck within `health_check_timeout`, DockerWorks initiates an immediate rollback to the previous working git commit and container state.
  - Built-in flapping/loop prevention skips known failing commits until a new commit is pushed.
- **⚡ Webhook & Health Endpoints**:
  - Optional built-in HTTP server (`/webhooks/{app}`).
  - Validates GitHub HMAC-SHA256 signatures (`X-Hub-Signature-256`), GitLab secret tokens, and bearer tokens for immediate push-triggered deployments.
  - Health check endpoint at `/healthz`.
- **🛠️ Lifecycle & Rollback Hooks**:
  - Custom shell commands before deployment (`pre_deploy`), after successful deployment (`post_deploy` e.g., running database migrations), and on rollback events (`on_rollback`).
- **🧹 Auto Prune**:
  - Automatically cleans up orphaned images with `docker image prune -f` only after successful deployments.
- **🐧 Systemd Daemon**:
  - Single static binary, low memory footprint (~15-30 MB), logs directly to `journald`.

---

## 🏗️ Architecture

```
                       +---------------------------------------+
                       |             Remote Sources            |
                       |  - GitHub / GitLab (Compose files)    |
                       |  - Container Registry (Images)        |
                       +---------------------------------------+
                                        |      ^
                      Webhook (Push) /  |      | Polling (Pull)
                      Digest Check      v      |
                       +---------------------------------------+
                       |         DockerWorks (systemd)         |
                       |                                       |
                       |   +-------------------------------+   |
                       |   |       HTTP Server             |   |
                       |   |   /healthz, /webhooks/{app}   |   |
                       |   +-------------------------------+   |
                       |                                       |
                       |   +-------------------------------+   |
                       |   |      Watchers & Schedulers    |   |
                       |   |   - GitOps Watcher            |   |
                       |   |   - Registry Digest Watcher   |   |
                       |   +-------------------------------+   |
                       |                                       |
                       |   +-------------------------------+   |
                       |   |         Compose Engine        |   |
                       |   |   - pre/post hooks            |   |
                       |   |   - docker compose pull/up    |   |
                       |   |   - health check & prune      |   |
                       |   +-------------------------------+   |
                       +---------------------------------------+
                                        |
                                        v
                       +---------------------------------------+
                       |              Host Docker              |
                       |        (Running Containers)           |
                       +---------------------------------------+
```

---

## 🚀 Quick Start

### 1. Build & Install as a Systemd Service

```bash
git clone https://github.com/indikaimk/docker_works.git
cd docker_works
sudo ./scripts/install.sh
```

> [!NOTE]
> `install.sh` automatically checks for Docker and Docker Compose on the host. If missing, it invokes `scripts/setup-docker.sh` to install Docker CE, the Docker Compose plugin, and start the systemd service on **Ubuntu**, **Debian**, and **Fedora**.
>
> You can also run the Docker setup script standalone:
> ```bash
> sudo ./scripts/setup-docker.sh
> ```

Or build manually:
```bash
go build -o /usr/local/bin/docker-works ./cmd/docker-works
```

### 2. Configuration (`/etc/docker-works/config.yaml`)

```yaml
server:
  enabled: true
  listen: "127.0.0.1:8080"
  webhook_secret: "my-webhook-secret"

defaults:
  git_poll_interval: "2m"
  registry_poll_interval: "5m"
  compose_command: "docker compose"
  state_dir: "/var/lib/docker-works"

apps:
  # App 1: GitOps repository watcher
  - name: "my-app"
    work_dir: "/var/lib/docker-works/apps/my-app"
    compose_files:
      - "docker-compose.yml"
    env_files:
      - "/etc/docker-works/secrets/my-app.env"
    git:
      repo: "https://github.com/myorg/my-infra.git"
      branch: "main"
      path: "services/web"
      poll_interval: "1m"
      auth:
        token: "${GITHUB_TOKEN}"
    hooks:
      pre_deploy: "echo 'Starting deployment...'"
      post_deploy: "docker compose exec -T web ./migrate.sh"
    auto_prune: true

  # App 2: Container image registry watcher
  - name: "my-api"
    work_dir: "/opt/apps/my-api"
    registry:
      watch_images:
        - "ghcr.io/myorg/api:latest"
      poll_interval: "3m"
    auto_prune: true
```

### 3. Verify & Start

```bash
# Check host environment and requirements
docker-works doctor --config /etc/docker-works/config.yaml

# Validate configuration file
docker-works validate --config /etc/docker-works/config.yaml

# Enable and start systemd daemon
sudo systemctl enable --now docker-works

# View live daemon logs
journalctl -u docker-works -f
```

---

## 💻 CLI Commands

| Command | Description | Example |
| :--- | :--- | :--- |
| `daemon` | Runs background watchers and webhook server | `docker-works daemon --config /etc/docker-works/config.yaml` |
| `sync` | Manually syncs and deploys an app immediately | `docker-works sync --config /etc/docker-works/config.yaml --app my-app` |
| `doctor` | Checks host environment, Docker, Compose, and config | `docker-works doctor --config /etc/docker-works/config.yaml` |
| `validate` | Validates configuration file syntax and app definitions | `docker-works validate --config config.yaml` |
| `version` | Displays version and build metadata | `docker-works version` |

---

## 🪝 Webhook Integration

When `server.enabled: true`, DockerWorks listens on the configured address (e.g. `127.0.0.1:8080`).

### GitHub Webhooks
1. In your GitHub repository, navigate to **Settings** > **Webhooks** > **Add webhook**.
2. **Payload URL**: `https://your-server.com/webhooks/{app_name}` (routed via reverse proxy).
3. **Content type**: `application/json`.
4. **Secret**: The `webhook_secret` defined in `config.yaml`.
5. DockerWorks verifies incoming requests using `X-Hub-Signature-256`.

### Manual Curl Trigger
```bash
curl -X POST "http://127.0.0.1:8080/webhooks/my-app" \
  -H "Authorization: Bearer my-webhook-secret"
```

---

## 🧪 Testing

Run the full test suite:

```bash
go test -v ./...
```

---

## 📄 License

MIT License. See [LICENSE.txt](file:///home/indika/projects/docker_works/LICENSE.txt) for details.
