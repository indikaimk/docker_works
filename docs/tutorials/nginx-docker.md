# Tutorial: Nginx on Docker Compose

This step-by-step tutorial walks through deploying an **Nginx** web server using DockerWorks and **Docker Compose** with GitOps auto-updates and automated rollback.

## Prerequisites

- A Linux host (Ubuntu, Debian, or Fedora) with `sudo` access.
- A Git repository (e.g. on GitHub) that you can push to.

---

## Step 1: Install DockerWorks

Run the one-line installer (this also installs Docker CE and Docker Compose if they are not already present):

```bash
curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash
```

Verify the host environment:

```bash
sudo docker-works doctor
```

---

## Step 2: Create the Nginx Compose Repository

Create a new Git repository (e.g. `my-nginx-app`) with `index.html` and `docker-compose.yml`:

```html
<!-- html/index.html -->
<!DOCTYPE html>
<html>
  <body><h1>Hello from DockerWorks (Docker Compose) v1!</h1></body>
</html>
```

```yaml
# docker-compose.yml
services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "8088:80"
    volumes:
      - ./html:/usr/share/nginx/html:ro
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/"]
      interval: 10s
      timeout: 3s
      retries: 3
```

Commit and push these files to your `main` branch.

---

## Step 3: Configure DockerWorks

Edit `/etc/docker-works/config.yaml`:

```yaml
defaults:
  runtime: "docker"
  git_poll_interval: "1m"
  state_dir: "/var/lib/docker-works"
  health_check_timeout: "60s"
  auto_rollback: true

apps:
  - name: "nginx-demo"
    compose_files:
      - "docker-compose.yml"
    git:
      repo: "https://github.com/YOUR_USER/my-nginx-app.git"
      branch: "main"
      # auth:
      #   token: "${GITHUB_TOKEN}" # Required only for private repos
    auto_prune: true
```

Validate the configuration:

```bash
sudo docker-works validate --config /etc/docker-works/config.yaml
```

---

## Step 4: Start the Daemon & Verify

Enable and start the `docker-works` systemd service:

```bash
sudo systemctl enable --now docker-works
journalctl -u docker-works -f
```

Within a few seconds, DockerWorks clones the repo, runs `docker compose up -d`, and waits for the healthcheck. Verify Nginx is serving traffic:

```bash
curl http://localhost:8088
```

---

## Step 5: Test Auto-Update

Update `html/index.html` to `v2`, commit, and push to `main`:

```bash
git add html/index.html
git commit -m "chore: update page to v2"
git push origin main
```

Within `git_poll_interval` (`1m`), DockerWorks detects the new commit, updates the repo, redeploys Nginx, and `curl http://localhost:8088` returns `v2`.
