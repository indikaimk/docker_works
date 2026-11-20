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

## Podman Quadlets (`mode: "quadlet"`)

In addition to `podman compose`, DockerWorks supports **Podman Quadlets** (Podman 4.4+), syncing `.container`, `.pod`, `.kube`, `.network`, `.volume`, `.image`, and `.build` unit files into systemd's Quadlet directory, running `systemctl daemon-reload`, restarting the generated services, and monitoring unit health for automated rollback.

### Example Quadlet App Configuration

```yaml
defaults:
  runtime: "podman"

apps:
  - name: "my-quadlet-app"
    mode: "quadlet" # Optional if quadlet: block is present
    quadlet:
      # dir defaults to /etc/containers/systemd (root) or ~/.config/containers/systemd (rootless)
      # files: ["app.network", "web.container"] # Auto-discovers quadlet files in work_dir if omitted
      # services: ["app-network.service", "web.service"] # Auto-derived if omitted
      # user_mode: true # Auto-detected from UID if omitted
    git:
      repo: "https://github.com/myorg/quadlet-infra.git"
      branch: "main"
      path: "apps/web"
    auto_rollback: true
    auto_prune: true
```

### How Quadlet Sync Works

1. Syncs Quadlet unit files from `work_dir` into `<quadlet.dir>/<app.name>/` (removing stale unit files from previous commits), and syncs any `.timer` files into `quadlet.systemd_dir` (`/etc/systemd/system` or `~/.config/systemd/user`).
2. Pulls any `Image=` references declared in the Quadlet files using `podman pull`.
3. Runs `systemctl [--user] daemon-reload` so Podman's Quadlet generator updates the systemd units.
4. Restarts long-running and standalone `Type=oneshot` services in dependency order (`.network`, `.volume`, `.image`, `.build`, `.pod`, `.kube`, `.container`), and enables/restarts `.timer` units without immediately firing their target batch `.service`.
5. Monitors `systemctl [--user] show <unit>` until units are `active` (or `inactive` with `Result=success` for `Type=oneshot` batch tasks), triggering rollback if any unit enters `failed`.
