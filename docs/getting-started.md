# Getting Started

DockerWorks releases standalone static binaries and native Linux packages (`.deb` and `.rpm`) for **Linux `amd64`** and **Linux `arm64`**.

## Installation

### Option A: One-Line Installer (Recommended)

Detects your CPU architecture, installs the binary, creates `/etc/docker-works/config.yaml`, and installs the `systemd` unit:

=== "Docker"

    ```bash
    curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash
    ```

=== "Podman"

    ```bash
    curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --runtime podman
    ```

Available installer flags (passed after `bash -s --`):

- `--version <ver>` (`-v`): Install a specific release version (default: latest).
- `--runtime <docker|podman>` (`-r`): Select container runtime (auto-detected if `docker` is missing and `podman` is present).
- `--skip-docker`: Skip host container runtime & Compose prerequisite checks.
- `--build`: Force building from local source (requires Go).

### Option B: Native Linux Packages (`.deb` / `.rpm`)

Download the package for your platform from the [GitHub Releases](https://github.com/indikaimk/docker_works/releases/latest) page:

| Platform | Architecture | Command |
| :--- | :--- | :--- |
| Ubuntu / Debian | `amd64` | `sudo dpkg -i docker-works_<version>_linux_amd64.deb` |
| Ubuntu / Debian | `arm64` | `sudo dpkg -i docker-works_<version>_linux_arm64.deb` |
| Fedora / RHEL | `amd64` | `sudo rpm -ivh docker-works_<version>_linux_amd64.rpm` |
| Fedora / RHEL | `arm64` | `sudo rpm -ivh docker-works_<version>_linux_arm64.rpm` |

### Option C: Direct Binary Download

```bash
# Linux Intel (amd64)
curl -fsSL https://github.com/indikaimk/docker_works/releases/latest/download/docker-works_0.1.0_linux_amd64.tar.gz | sudo tar -xz -C /usr/local/bin docker-works

# Linux ARM (arm64)
curl -fsSL https://github.com/indikaimk/docker_works/releases/latest/download/docker-works_0.1.0_linux_arm64.tar.gz | sudo tar -xz -C /usr/local/bin docker-works
```

## Verify & Start

```bash
# 1. Check host environment and runtime prerequisites
docker-works doctor --config /etc/docker-works/config.yaml

# 2. Validate configuration file
docker-works validate --config /etc/docker-works/config.yaml

# 3. Enable and start the systemd service
sudo systemctl enable --now docker-works

# 4. Stream logs
journalctl -u docker-works -f
```

!!! tip "Running with Podman?"
    See the [Podman Guide](podman.md) for both rootful (`docker-works-podman.service`) and rootless (`docker-works-podman-user.service`) systemd unit instructions.
