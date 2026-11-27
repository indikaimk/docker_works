# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-11-27

### Added
- **SOPS Encrypted Secrets (`apps[].sops`)**: Decrypts SOPS-encrypted `.env`, `.yaml`, `.json`, and binary files into `work_dir` with `0600` permissions before `pre_deploy` and container startup (supporting `age_key`, `age_key_file`, and AWS KMS IAM roles), with automatic re-decryption on rollback and `sops` CLI checks in `docker-works doctor`.
- **Go `text/template` Rendering (`apps[].templates`)**: Renders `.tmpl`, `.tpl`, and `.gotmpl` configuration templates from YAML/JSON `vars_files` and inline `vars` with `missingkey=error` fail-fast validation and built-in helpers (`default`, `join`, `upper`, `lower`, `trim`, `env`) for both Compose and Quadlet modes.
- **Documentation & Tutorials**: Added [Secrets Management (SOPS)](docs/secrets.md), [Gitless Mode (OCI)](docs/gitless.md), [Templates (Go)](docs/templating.md), and step-by-step Nginx tutorials for [Docker Compose](docs/tutorials/nginx-docker.md) and [Podman Quadlets](docs/tutorials/nginx-podman-quadlet.md).

## [0.5.0] - 2026-11-26

### Added
- **Gitless OCI Artifact Support (`apps[].oci`)**: Added an OCI artifact watcher and extractor (`internal/registry/oci.go`) supporting both ORAS-style raw file layers and tar/tar+gzip OCI image layers, with Zip-Slip path-traversal protection and a 64 MiB extraction cap.
- **Digest-Pinned Rollback**: On healthcheck failure, OCI-backed apps automatically pull the previous immutable `@sha256:...` artifact digest and roll back, with Prometheus `trigger="oci"` metrics and `docker-works validate` reporting.

## [0.4.0] - 2026-11-23

### Added
- **Quadlet Batch Task Support**: Added `Type=oneshot` completion handling (`ActiveState=inactive`, `Result=success`) and systemd `.timer` unit syncing (`quadlet.systemd_dir`) so scheduled batch jobs are enabled/restarted without firing on every deploy.
- **Portable `./` Paths in Quadlets**: Automatically resolves `./` relative paths in `Volume`, `EnvironmentFile`, `Yaml`, `ConfigMap`, and `SeccompProfile` inside Quadlet unit files to absolute paths under `work_dir`.
- **Prometheus Metrics Endpoint**: Added a zero-dependency `GET /metrics` endpoint (`server.metrics_enabled: true`) exporting deployment counts, rollbacks, poll errors, last deploy status/timestamp, and duration.
- **Expanded Quadlet Documentation**: Documented unit activation order, portable `./` paths, `Type=oneshot` and `.timer` batch tasks, and unit name overrides in `docs/podman.md`.

### Fixed
- **Git Subdirectory Resolution (`git.path`)**: Cloned repositories consistently into `app.WorkDir` and resolved `<app.WorkDir>/<git.path>` as the effective deploy directory for both Compose and Quadlet modes, with path-traversal validation.

## [0.3.1] - 2026-11-20

### Security
- **Git Credential & SSH Hardening**: Passed HTTPS Git credentials via scoped `GIT_CONFIG_*` `http.extraHeader` env vars instead of CLI URL arguments, shell-quoted `ssh_key_path`, and added `--` separators / `-`-prefix validation against git flag injection.
- **Webhook Server Hardening**: Enforced 1 MiB request body limits, HTTP server timeouts, constant-time token comparisons (`subtle.ConstantTimeCompare`), removed `?secret=` query-string auth, and added startup warnings for unauthenticated webhooks.
- **Path Traversal & Installer Hardening**: Restricted `apps[].name` and `quadlet.files` against path traversal, restricted `state.json` to `0600`, warned on overly permissive `config.yaml` permissions, and added SHA256 `checksums.txt` verification to `scripts/install.sh`.

## [0.3.0] - 2026-11-19

### Added
- **Podman Runtime Support**: Added `defaults.runtime` (`docker` or `podman`) in `config.yaml`, automatically defaulting `compose_command` to `"podman compose"` and image pruning to `podman image prune -f`.
- **Podman Quadlet Support**: Added `apps[].mode: "quadlet"` and `apps[].quadlet` configuration to sync `.container`, `.pod`, `.kube`, `.network`, `.volume`, `.image`, and `.build` unit files into systemd's Quadlet directory, run `systemctl [--user] daemon-reload` and `restart`, and monitor unit health with automated rollback.
- **Podman Systemd Units**: Added `packaging/systemd/docker-works-podman.service` (rootful system unit) and `packaging/systemd/docker-works-podman-user.service` (rootless user unit), included in release archives and `.deb`/`.rpm` packages.
- **Podman Registry Auth Fallback**: Added `auth.json` credential resolution (`$REGISTRY_AUTH_FILE`, `$XDG_RUNTIME_DIR/containers/auth.json`, `~/.config/containers/auth.json`) alongside `~/.docker/config.json`.
- **Doctor Runtime Inspection**: Updated `docker-works doctor` to detect `defaults.runtime` from config or `--runtime docker|podman` and verify Podman CLI, `podman compose`, and `podman info`.
- **Installer Runtime Support**: Updated `scripts/install.sh` with `--runtime <docker|podman>` (and auto-detection when Docker is absent) to validate Podman prerequisites and install `docker-works-podman.service`.
- **MkDocs Material Documentation**: Added `mkdocs.yml`, structured documentation under `docs/`, and `.github/workflows/docs.yml` for GitHub Pages deployment.

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

