# DockerWorks

**DockerWorks** is a lightweight, zero-dependency GitOps and auto-update daemon for **Docker Compose** and **Podman Compose**, written in Go and designed to run as a native `systemd` service on Linux hosts.

!!! note "Disclaimer"
    DockerWorks is an independent, non-commercial open-source project and is not affiliated with, sponsored by, or endorsed by Docker, Inc. "Docker" is a registered trademark of Docker, Inc.

## Key Features

- **Native Compose Engine**: Invokes `docker compose` or `podman compose` directly via `defaults.runtime` (`docker` or `podman`), supporting multiple compose files (`-f`) and `.env` files.
- **GitOps Mode (Pull-based)**:
  - Watches Git repositories (GitHub, GitLab, Gitea, etc.) for new commits on target branches.
  - Checks remote heads via `git ls-remote` without cloning on every poll.
  - Supports SSH deploy keys (`ssh_key_path`), HTTPS tokens, username/password auth, and monorepo subdirectories (`path`).
- **Container Registry Watcher**:
  - Monitors image tags (`latest`, semver tags) on Docker Hub, GHCR, AWS ECR, and private registries.
  - Inspects remote manifest digests (SHA256) using lightweight `HEAD`/`GET` requests without pulling image layers.
  - Triggers compose pull and redeployment when an image digest changes.
- **Automated Rollback & Health Checks**:
  - Polls container health (`<compose_command> ps --format json`) after deployment.
  - Rolls back to the previous working commit and container state if any container crashes or fails its healthcheck within `health_check_timeout`.
  - Skips known failing commits/digests to prevent crash-loop flapping.
- **Webhook & Health Endpoints**:
  - Built-in HTTP server with `/healthz` and `/webhooks/{app}`.
  - Supports GitHub HMAC-SHA256 (`X-Hub-Signature-256`), GitLab (`X-Gitlab-Token`), Bearer tokens, and `?secret=` query parameters.
- **Lifecycle Hooks, SOPS Secrets & Auto Prune**:
  - Shell hooks for `pre_deploy`, `post_deploy`, and `on_rollback` (with `DOCKER_WORKS_APP` set).
  - Native [SOPS](secrets.md) decryption (`age` / AWS KMS) for encrypted `.env` and config files in Git or OCI artifacts.
  - Optional `auto_prune` runs `docker image prune -f` or `podman image prune -f` after healthy deployments.

## Architecture

```text
+---------------------------------------+
|             Remote Sources            |
|  - GitHub / GitLab (Compose files)    |
|  - Container Registry (Images)        |
+---------------------------------------+
                 |      ^
Webhook (Push) / |      | Polling (Pull)
Digest Check     v      |
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
|   |   - pre/post/rollback hooks   |   |
|   |   - compose pull / up / ps    |   |
|   |   - health check & prune      |   |
|   +-------------------------------+   |
+---------------------------------------+
                 |
                 v
+---------------------------------------+
|      Host Runtime (Docker / Podman)   |
|         (Running Containers)          |
+---------------------------------------+
```
