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
2. Rewrites portable `./` relative paths (see below) to absolute paths under `work_dir` when writing synced Quadlet files.
3. Pulls any `Image=` references declared in the Quadlet files using `podman pull`.
4. Runs `systemctl [--user] daemon-reload` so Podman's Quadlet generator updates the systemd units.
5. Restarts long-running and standalone `Type=oneshot` services in dependency order:
   `.network` → `.volume` → `.image` → `.build` → `.pod` → `.kube` → `.container`.
   Any `.timer` units are enabled/started (`systemctl [--user] enable --now` + `restart`) without immediately firing their target batch `.service`.
6. Monitors `systemctl [--user] show <unit>` until units are `active` (or `inactive` with `Result=success` for `Type=oneshot` batch tasks), triggering rollback if any unit enters `failed`.

### Portable Relative Paths (`./`)

Because Quadlet unit files are copied into `<quadlet.dir>/<app.name>/`, relative paths inside `.container` or `.kube` files would normally fail to resolve against your Git repository. DockerWorks automatically rewrites `./` paths to absolute paths inside `work_dir` for the following directives:

- `Volume=./<path>:<container_path>[:<options>]`
- `EnvironmentFile=./<path>` and `EnvironmentFile=-./<path>`
- `Yaml=./<path>` (`.kube` units)
- `ConfigMap=./<path>` (`.kube` units)
- `SeccompProfile=./<path>` (`.container` units)

Absolute paths (e.g. `/data/kafka`), systemd specifiers (e.g. `%h/...`), and named volumes (e.g. `mydata.volume:/data`) are left untouched:

```ini
[Container]
Image=prom/prometheus:latest
# Versioned config file in Git -> rewritten to <work_dir>/prometheus.yml
Volume=./prometheus.yml:/etc/prometheus/prometheus.yml:Z,U
# Persistent host data directory -> left untouched
Volume=/data/kafka:/prometheus:Z,U
# Optional .env file in Git -> rewritten to -<work_dir>/app.env
EnvironmentFile=-./app.env
```

### Batch Tasks (`Type=oneshot` & `.timer` Units)

- **Deploy-time batch jobs (`Type=oneshot`):** Add `[Service]\nType=oneshot` in your `.container` unit. DockerWorks restarts the unit on deploy and treats `ActiveState=inactive` with `Result=success` as a successful run.
- **Scheduled batch jobs (`.timer`):** Place a matching `<name>.timer` (or specify `Unit=<target>.service` in `[Timer]`) alongside `<name>.container`. DockerWorks syncs the `.timer` into `quadlet.systemd_dir`, enables and restarts the timer, and skips running the target `.service` immediately on deploy.

### Unit Name Mapping & Overrides

By default, DockerWorks derives the systemd unit name from the Quadlet filename:

| Extension | Generated Unit |
| :--- | :--- |
| `.container`, `.kube` | `<name>.service` |
| `.pod` | `<name>-pod.service` |
| `.network` | `<name>-network.service` |
| `.volume` | `<name>-volume.service` |
| `.image` | `<name>-image.service` |
| `.build` | `<name>-build.service` |

If you set `ServiceName=` inside a Quadlet file or specify `quadlet.services` in `config.yaml`, DockerWorks uses those unit names instead.
