# DockerWorks

A lightweight, zero-dependency GitOps and auto-update daemon for **Docker Compose**, **Podman Compose**, and **Podman Quadlets**, written in Go and designed to run as a native `systemd` service on Linux hosts.

📚 **Full Documentation:** [https://indikaimk.github.io/docker_works/](https://indikaimk.github.io/docker_works/)

---

## ✨ Features

- **Docker & Podman Support**: Invokes `docker compose` or `podman compose` directly (`defaults.runtime`), plus native **Podman Quadlet** (`mode: quadlet`) support.
- **GitOps & Gitless OCI Mode**: Polls Git repositories (`git ls-remote`) or pulls Compose/Quadlet bundles directly from OCI registries (`apps[].oci`) with digest-pinned rollbacks.
- **Registry Watcher**: Checks remote image manifest digests (`HEAD`/`GET`) without pulling layers and redeploys on digest changes.
- **Automated Rollback & Health Checks**: Monitors container / systemd unit health post-deploy and rolls back automatically on failure.
- **Webhooks, Metrics & Hooks**: Built-in `/healthz`, `/metrics`, and `/webhooks/{app}` endpoints plus `pre_deploy`, `post_deploy`, `on_rollback` hooks and **SOPS** encrypted secrets decryption.
- **Auto Prune**: Cleans up unused images (`docker image prune -f` / `podman image prune -f`) after healthy deployments.

---

## 🚀 Quick Start

```bash
# Install for Docker (Ubuntu, Debian, Fedora)
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash

# Or install for Podman
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --runtime podman
```

Verify and start the service:

```bash
docker-works doctor --config /etc/docker-works/config.yaml
sudo systemctl enable --now docker-works
```

---

## 📖 Documentation

- [Getting Started](https://indikaimk.github.io/docker_works/getting-started/) — Installer options, `.deb`/`.rpm` packages, and binary downloads
- [Configuration Reference](https://indikaimk.github.io/docker_works/configuration/) — Full `config.yaml` schema and examples
- [Gitless Mode (OCI)](https://indikaimk.github.io/docker_works/gitless/) — Deploy Compose/Quadlet bundles from OCI registries without Git
- [Secrets Management (SOPS)](https://indikaimk.github.io/docker_works/secrets/) — Encrypted `.env` and config files with `age` or AWS KMS
- [Templates (Go)](https://indikaimk.github.io/docker_works/templating/) — Render config files from YAML/JSON vars with `text/template`
- [Podman Guide](https://indikaimk.github.io/docker_works/podman/) — Rootful, rootless, and Podman Quadlet setup
- [Tutorial: Nginx on Docker Compose](https://indikaimk.github.io/docker_works/tutorials/nginx-docker/) — Step-by-step Docker Compose walkthrough
- [Tutorial: Nginx on Podman Quadlets](https://indikaimk.github.io/docker_works/tutorials/nginx-podman-quadlet/) — Step-by-step rootless Quadlet walkthrough
- [CLI Reference](https://indikaimk.github.io/docker_works/cli/) — `daemon`, `sync`, `doctor`, `validate`, and `version`
- [Webhooks & Health](https://indikaimk.github.io/docker_works/webhooks/) — Webhook authentication and endpoints
- [Development & Releases](https://indikaimk.github.io/docker_works/development/) — `Makefile` targets and GoReleaser workflow

---

## 📄 License

MIT License. See [LICENSE.txt](LICENSE.txt) for details.

> **Disclaimer:** DockerWorks is an independent, non-commercial open-source project and is not affiliated with, sponsored by, or endorsed by Docker, Inc. "Docker" is a registered trademark of Docker, Inc.
