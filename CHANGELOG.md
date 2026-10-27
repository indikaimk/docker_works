# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-27

### Added
- **Native Docker Compose Engine**: Direct invocation of host `docker compose` CLI supporting multi-file configurations (`-f`) and `.env` files.
- **GitOps Mode**: Automated pull-based deployment watching Git repositories with low-overhead remote commit checking (`git ls-remote`), SSH deploy keys, and monorepo subdirectory targeting.
- **Registry Watcher**: Remote manifest digest checking (SHA256) via HTTP `HEAD`/`GET` requests without downloading image layers.
- **Automated Rollback & Health Checks**: Post-deploy container health evaluations via `docker compose ps --format json` with automatic rollback to previous stable commit and state upon failure, plus flapping/loop prevention.
- **Webhook & Health Server**: Embedded HTTP server supporting GitHub HMAC-SHA256 (`X-Hub-Signature-256`), GitLab secret tokens, bearer auth for instant deployments, and `/healthz` liveness checks.
- **Lifecycle & Rollback Hooks**: Execution of custom shell scripts at `pre_deploy`, `post_deploy`, and `on_rollback` lifecycle phases.
- **Automated Resource Pruning**: Optional dangling Docker image pruning (`docker image prune -f`) after successful deployments.
- **CLI Commands**: Added `daemon`, `sync`, `doctor`, `validate`, and `version` subcommands.
- **Automated Host Setup**: Added `scripts/setup-docker.sh` and `scripts/install.sh` for bootstrapping Docker CE, Docker Compose, and `systemd` service integration on Ubuntu, Debian, and Fedora.
- **Packaging & CI/CD**: Multi-arch GoReleaser pipeline producing static binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`), Debian (`.deb`), and RedHat (`.rpm`) packages with automated systemd unit installation.

