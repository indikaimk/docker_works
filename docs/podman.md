# Podman Guide

DockerWorks supports **Podman** (Podman 4.1+ via `podman compose`) for both **rootful** (system-wide) and **rootless** (per-user) deployments.

## Prerequisites

- **Podman 4.1+**
- A Compose provider supported by `podman compose` (such as `docker-compose`)
- `podman.socket` enabled via systemd

## Configuration

Set `defaults.runtime` to `"podman"` in your `config.yaml`:

```yaml
defaults:
  runtime: "podman"
  state_dir: "/var/lib/docker-works"
```

When `defaults.runtime: "podman"` is set:

- `defaults.compose_command` automatically defaults to `"podman compose"` (unless explicitly overridden).
- `auto_prune: true` executes `podman image prune -f`.
- `docker-works doctor` checks `podman --version`, `podman compose version`, and `podman info`.
- Registry digest polling checks Podman's `auth.json` (`$REGISTRY_AUTH_FILE`, `$XDG_RUNTIME_DIR/containers/auth.json`, `~/.config/containers/auth.json`) in addition to `~/.docker/config.json`.

## Rootful Podman (System Service)

1. Enable the system Podman socket:

   ```bash
   sudo systemctl enable --now podman.socket
   ```

2. Install using the automated installer with `--runtime podman`, or copy `packaging/systemd/docker-works-podman.service`:

   ```bash
   sudo cp packaging/systemd/docker-works-podman.service /etc/systemd/system/docker-works.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now docker-works
   ```

3. Verify the host environment:

   ```bash
   sudo docker-works doctor --config /etc/docker-works/config.yaml
   ```

## Rootless Podman (User Service)

1. Enable the user-level Podman socket and lingering so services stay active after logout:

   ```bash
   systemctl --user enable --now podman.socket
   loginctl enable-linger "$USER"
   ```

2. Create `~/.config/docker-works/config.yaml` with `runtime: "podman"` and a user-writable `state_dir`:

   ```yaml
   defaults:
     runtime: "podman"
     state_dir: "/home/youruser/.local/share/docker-works"

   apps:
     - name: "my-app"
       git:
         repo: "https://github.com/myorg/infra.git"
   ```

3. Install `packaging/systemd/docker-works-podman-user.service` (which sets `DOCKER_HOST=unix://%t/podman/podman.sock` and depends on `podman.socket`):

   ```bash
   mkdir -p ~/.config/systemd/user
   cp packaging/systemd/docker-works-podman-user.service ~/.config/systemd/user/docker-works.service
   systemctl --user daemon-reload
   systemctl --user enable --now docker-works
   ```

4. Verify and stream logs:

   ```bash
   docker-works doctor --config ~/.config/docker-works/config.yaml
   journalctl --user -u docker-works -f
   ```
