# Tutorial: Nginx on Podman Quadlets (Rootless)

This step-by-step tutorial walks through deploying an **Nginx** web server using **Podman Quadlets** in rootless user mode, with portable `./` volume paths and automated rollback.

## Prerequisites

- A Linux host with **Podman 4.4+** and `systemd` user sessions enabled.
- A Git repository (e.g. on GitHub) that you can push to.

---

## Step 1: Enable Rootless Podman Socket & Install DockerWorks

1. Enable the user-level Podman socket and lingering (so containers stay running after you log out):

   ```bash
   systemctl --user enable --now podman.socket
   loginctl enable-linger "$USER"
   ```

2. Download and install the `docker-works` binary (replace `<version>` and `amd64`/`arm64` as needed):

   ```bash
   curl -fsSL https://github.com/indikaimk/docker_works/releases/latest/download/docker-works_0.5.0_linux_amd64.tar.gz \
     | sudo tar -xz -C /usr/local/bin docker-works
   ```

3. Install the rootless Podman user systemd unit:

   ```bash
   mkdir -p ~/.config/systemd/user ~/.config/docker-works
   curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/packaging/systemd/docker-works-podman-user.service \
     -o ~/.config/systemd/user/docker-works.service
   systemctl --user daemon-reload
   ```

---

## Step 2: Create the Nginx Quadlet Repository

Create a Git repository (e.g. `my-nginx-quadlet`) with `html/index.html` and `nginx.container`:

```html
<!-- html/index.html -->
<!DOCTYPE html>
<html>
  <body><h1>Hello from DockerWorks (Podman Quadlet) v1!</h1></body>
</html>
```

```ini
# nginx.container
[Unit]
Description=Nginx Demo Container

[Container]
Image=docker.io/library/nginx:alpine
PublishPort=8088:80
# Portable ./ path -> DockerWorks rewrites this to <work_dir>/html
Volume=./html:/usr/share/nginx/html:Z,ro

[Service]
Restart=always

[Install]
WantedBy=default.target
```

Commit and push these files to your `main` branch.

---

## Step 3: Configure DockerWorks

Create `~/.config/docker-works/config.yaml` (replace `YOUR_USER` with your Linux username and GitHub username):

```yaml
defaults:
  runtime: "podman"
  git_poll_interval: "1m"
  state_dir: "/home/YOUR_USER/.local/share/docker-works"
  health_check_timeout: "60s"
  auto_rollback: true

apps:
  - name: "nginx-quadlet"
    mode: "quadlet"
    git:
      repo: "https://github.com/YOUR_GITHUB_USER/my-nginx-quadlet.git"
      branch: "main"
    auto_prune: true
```

Validate the host and configuration:

```bash
docker-works doctor --config ~/.config/docker-works/config.yaml
docker-works validate --config ~/.config/docker-works/config.yaml
```

---

## Step 4: Start the User Daemon & Verify

Enable and start the user service:

```bash
systemctl --user enable --now docker-works
journalctl --user -u docker-works -f
```

DockerWorks will:
1. Clone the repo into `~/.local/share/docker-works/apps/nginx-quadlet`.
2. Pull `docker.io/library/nginx:alpine`.
3. Copy `nginx.container` to `~/.config/containers/systemd/nginx-quadlet/nginx.container` with `./html` resolved to `~/.local/share/docker-works/apps/nginx-quadlet/html`.
4. Run `systemctl --user daemon-reload` and `systemctl --user restart nginx.service`.

Verify Nginx is running:

```bash
curl http://localhost:8088
systemctl --user status nginx.service
```

---

## Step 5: Test Auto-Update

Update `html/index.html` to `v2`, commit, and push to `main`. Within `1m`, DockerWorks updates the repository, restarts `nginx.service`, and `curl http://localhost:8088` serves `v2`.
