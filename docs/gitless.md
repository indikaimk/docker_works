# Gitless Mode (OCI Artifacts)

DockerWorks can run **100% gitless** by pulling Compose files, Podman Quadlets, and encrypted SOPS configs directly from any OCI-compatible container registry (GitHub Packages GHCR, `zot`, `registry:2`, AWS ECR, etc.).

## How It Works

1. Your CI/CD pipeline packages deployment files (`docker-compose.yml`, `*.container`, `prometheus.yml`, `secrets.enc.env`) and pushes them as an **OCI artifact** using [`oras`](https://oras.land/) or as a minimal OCI config image.
2. DockerWorks polls the artifact's manifest digest using lightweight `HEAD`/`GET` requests.
3. When the digest changes, DockerWorks pulls and safely extracts the files into `work_dir` (supporting both ORAS raw-file layers and tar/tar+gzip layers, with path-traversal protection).
4. DockerWorks runs `deploy` (and `sops --decrypt` if configured) and monitors container/unit health.
5. On healthcheck failure, DockerWorks **pulls the previous immutable `@sha256:...` digest** from the registry and rolls back.

## Example: CI Push + DockerWorks Config

### 1. Push config bundle from CI (`oras`)

```bash
oras push ghcr.io/myorg/my-app-config:latest \
  docker-compose.yml \
  configs/prometheus.yml \
  secrets.enc.env
```

### 2. Configure `apps[].oci` in `config.yaml`

```yaml
apps:
  - name: "my-app"
    env_files:
      - "secrets.env"
    oci:
      artifact: "ghcr.io/myorg/my-app-config:latest"
      poll_interval: "2m"
      auth:
        ghcr.io:
          token: "${GHCR_TOKEN}"
    sops:
      age_key: "${SOPS_AGE_KEY}"
      files:
        - source: "secrets.enc.env"
          target: "secrets.env"
    auto_rollback: true
    auto_prune: true
```

*(If `oci.auth` is omitted, DockerWorks falls back to `~/.docker/config.json` and Podman's `auth.json`.)*

## Combining with Watch Images or Webhooks

- You can pair `oci:` (for Compose/Quadlet config updates) with `registry:` (for app container image updates) in the same app, or use `oci:` alone.
- Trigger an immediate OCI pull + deploy anytime via `POST /webhooks/{app}` or `docker-works sync --app my-app`.
