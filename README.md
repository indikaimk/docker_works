# DockerWorks

A lightweight, zero-dependency GitOps and auto-update daemon for **Docker Compose** and **Podman Compose**, written in Go and designed to run as a native `systemd` service on Linux hosts.

---

## ✨ Features

- **🚀 Native Compose Engine**: Invokes `docker compose` or `podman compose` directly via `defaults.runtime` (`docker` or `podman`), supporting multiple compose files (`-f`) and `.env` files.
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
  - Automatically cleans up orphaned images with `docker image prune -f` or `podman image prune -f` (matching `defaults.runtime`) only after successful deployments.
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

### 1. Install Pre-built Binary (Linux Intel & ARM)

DockerWorks releases standalone static binaries and native Linux packages for **Linux x86_64 (Intel/AMD)** and **Linux ARM64**. No source code, Git, or Go compiler required on the host.

#### Option A: One-Line Quick Install (Recommended)
Automatically detects your CPU architecture (Intel or ARM), downloads the latest binary, installs systemd service, and sets up default configuration:
```bash
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash
```

Custom options can be passed via `bash -s --`:
```bash
# Install a specific version
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --version 0.1.0

# Skip host container runtime & Compose prerequisite checks
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --skip-docker

# Install and configure systemd unit for Podman
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --runtime podman
```

> [!NOTE]
> When using Docker, `install.sh` automatically checks for Docker CE and Docker Compose on the host and invokes `scripts/setup-docker.sh` if missing (Ubuntu, Debian, Fedora). When `--runtime podman` is specified (or Podman is auto-detected when Docker is absent), `install.sh` verifies `podman` and `podman compose` and installs the Podman systemd unit (`docker-works-podman.service`).

#### Option B: Native Linux Packages (.deb / .rpm)
Download and install the native package for your architecture:

| Platform | Architecture | Package Command |
| :--- | :--- | :--- |
| **Ubuntu / Debian** | **Intel (amd64)** | `sudo dpkg -i docker-works_0.1.0_linux_amd64.deb` |
| **Ubuntu / Debian** | **ARM (arm64)** | `sudo dpkg -i docker-works_0.1.0_linux_arm64.deb` |
| **Fedora / RHEL** | **Intel (amd64)** | `sudo rpm -ivh docker-works_0.1.0_linux_amd64.rpm` |
| **Fedora / RHEL** | **ARM (arm64)** | `sudo rpm -ivh docker-works_0.1.0_linux_arm64.rpm` |

#### Option C: Direct Binary Download & Extract
Download the pre-compiled binary archive directly from the [latest release](https://github.com/indikaimk/docker_works/releases/latest):

```bash
# For Linux Intel (x86_64 / amd64):
curl -fsSL https://github.com/indikaimk/docker_works/releases/latest/download/docker-works_0.1.0_linux_amd64.tar.gz | sudo tar -xz -C /usr/local/bin docker-works

# For Linux ARM (aarch64 / arm64):
curl -fsSL https://github.com/indikaimk/docker_works/releases/latest/download/docker-works_0.1.0_linux_arm64.tar.gz | sudo tar -xz -C /usr/local/bin docker-works
```


### 2. Configuration (`/etc/docker-works/config.yaml`)

```yaml
server:
  enabled: true
  listen: "127.0.0.1:8080"
  webhook_secret: "my-webhook-secret"

defaults:
  runtime: "docker"             # "docker" (default) or "podman"
  git_poll_interval: "2m"
  registry_poll_interval: "5m"
  compose_command: "docker compose" # Auto-defaults to "podman compose" when runtime is "podman"
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

## 🦭 Podman Support (Rootful & Rootless)

DockerWorks supports **Podman** via `podman compose` (Podman 4.1+ with an external compose provider such as `docker-compose`) and ships dedicated systemd units for both rootful and rootless deployments.

### Configuration

Set `defaults.runtime` to `"podman"` in your `config.yaml`:

```yaml
defaults:
  runtime: "podman"             # Automatically sets compose_command to "podman compose"
  state_dir: "/var/lib/docker-works" # Use a user-writable path for rootless mode
```

When `defaults.runtime: "podman"` is configured:
- `defaults.compose_command` automatically defaults to `"podman compose"` (unless explicitly overridden).
- `auto_prune: true` runs `podman image prune -f`.
- `docker-works doctor` checks `podman --version`, `podman compose version`, and `podman info`.
- Registry digest polling checks Podman's `auth.json` (`$REGISTRY_AUTH_FILE`, `$XDG_RUNTIME_DIR/containers/auth.json`, `~/.config/containers/auth.json`) in addition to `~/.docker/config.json`.

### Option A: Rootful Podman (System Service)

1. Enable the Podman system socket:
   ```bash
   sudo systemctl enable --now podman.socket
   ```
2. Install the rootful Podman systemd unit (automatically selected by `install.sh --runtime podman`, or available at `/usr/local/share/docker-works/docker-works-podman.service` from `.deb`/`.rpm` packages):
   ```bash
   sudo cp packaging/systemd/docker-works-podman.service /etc/systemd/system/docker-works.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now docker-works
   ```
3. Verify the environment:
   ```bash
   sudo docker-works doctor --config /etc/docker-works/config.yaml
   ```

### Option B: Rootless Podman (User Service)

1. Enable the user-level Podman socket and lingering (so services stay running after logout):
   ```bash
   systemctl --user enable --now podman.socket
   loginctl enable-linger "$USER"
   ```
2. Create your user config at `~/.config/docker-works/config.yaml` with `runtime: "podman"` and a user-writable `state_dir` (e.g. `~/.local/share/docker-works`):
   ```yaml
   defaults:
     runtime: "podman"
     state_dir: "/home/youruser/.local/share/docker-works"
   ```
3. Install the rootless user unit ([docker-works-podman-user.service](packaging/systemd/docker-works-podman-user.service)):
   ```bash
   mkdir -p ~/.config/systemd/user
   cp packaging/systemd/docker-works-podman-user.service ~/.config/systemd/user/docker-works.service
   systemctl --user daemon-reload
   systemctl --user enable --now docker-works
   ```
4. Verify and monitor:
   ```bash
   docker-works doctor --config ~/.config/docker-works/config.yaml
   journalctl --user -u docker-works -f
   ```

---

## 💻 CLI Commands

| Command | Description | Example |
| :--- | :--- | :--- |
| `daemon` | Runs background watchers and webhook server | `docker-works daemon --config /etc/docker-works/config.yaml` |
| `sync` | Manually syncs and deploys an app immediately | `docker-works sync --config /etc/docker-works/config.yaml --app my-app` |
| `doctor` | Checks host environment, runtime (`--runtime docker\|podman`), Compose, and config | `docker-works doctor --config /etc/docker-works/config.yaml` |
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

---

## 🛠️ Development & CI/CD

### Local Development Commands

A root [Makefile](Makefile) is provided for developer velocity:

```bash
# Run tests with race detection and code coverage
make test

# Lint and verify code formatting
make lint

# Check local host prerequisites
make doctor

# Build release binary into bin/docker-works
make build
```

### Automated Releases & Packages

Releases are automated via GitHub Actions and GoReleaser:

1. Push a semantic version tag:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```
2. The [release.yml](.github/workflows/release.yml) workflow triggers, generating:
   - Multi-architecture static binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`)
   - Native Linux packages: `.deb` (Ubuntu/Debian) and `.rpm` (Fedora/RHEL) with systemd integration
   - Checksums (`checksums.txt`) and automatic release notes on GitHub

---

## 📄 License

MIT License. See [LICENSE.txt](LICENSE.txt) for details.

> **Disclaimer:** DockerWorks is an independent, non-commercial open-source project and is not affiliated with, sponsored by, or endorsed by Docker, Inc. "Docker" is a registered trademark of Docker, Inc.
