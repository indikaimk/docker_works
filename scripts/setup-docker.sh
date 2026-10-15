#!/usr/bin/env bash
set -euo pipefail

# DockerWorks - Automated Docker & Docker Compose Installer
# Supported distributions: Ubuntu, Debian, Fedora

echo "=========================================================="
echo " DockerWorks: Host Docker & Compose Setup"
echo "=========================================================="

if [ "$EUID" -ne 0 ]; then
  echo "Error: This script must be run as root (e.g. using sudo)."
  exit 1
fi

# 1. Check if Docker and Compose plugin are already installed
check_existing() {
  local has_docker=0
  local has_compose=0

  if command -v docker >/dev/null 2>&1; then
    has_docker=1
  fi

  if docker compose version >/dev/null 2>&1; then
    has_compose=1
  fi

  if [ "$has_docker" -eq 1 ] && [ "$has_compose" -eq 1 ]; then
    echo "Docker and Docker Compose plugin are already installed:"
    echo "  - $(docker --version)"
    echo "  - $(docker compose version)"

    # Ensure systemd service is active
    if command -v systemctl >/dev/null 2>&1; then
      if ! systemctl is-active --quiet docker; then
        echo "Starting Docker service via systemd..."
        systemctl enable --now docker
      else
        echo "Docker service is active and running."
      fi
    fi
    exit 0
  fi

  if [ "$has_docker" -eq 1 ] && [ "$has_compose" -eq 0 ]; then
    echo "Notice: Docker is installed, but 'docker compose' (v2 plugin) is missing."
    echo "Proceeding to install Docker Compose plugin..."
  fi
}

# 2. Detect Linux distribution
detect_distribution() {
  if [ ! -f /etc/os-release ]; then
    echo "Error: Cannot detect Linux distribution (/etc/os-release not found)."
    exit 1
  fi

  . /etc/os-release
  DISTRO_ID="${ID:-unknown}"
  DISTRO_ID_LIKE="${ID_LIKE:-}"
  DISTRO_CODENAME="${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}"
}

# 3. Installation for Ubuntu & Debian
install_ubuntu_debian() {
  local target_os="$1" # "ubuntu" or "debian"
  echo "--> Installing Docker CE and Compose plugin on ${target_os^}..."

  export DEBIAN_FRONTEND=noninteractive

  # Remove old or conflicting packages
  echo "1. Removing any conflicting legacy packages..."
  apt-get remove -y docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc >/dev/null 2>&1 || true

  # Install prerequisite utilities
  echo "2. Installing repository prerequisites..."
  apt-get update -y
  apt-get install -y ca-certificates curl gnupg lsb-release

  # Setup Docker GPG keyring
  echo "3. Adding Docker's official GPG key..."
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL "https://download.docker.com/linux/${target_os}/gpg" -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc

  # Add official repository
  echo "4. Adding Docker APT repository..."
  ARCH=$(dpkg --print-architecture)
  echo "deb [arch=${ARCH} signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${target_os} ${DISTRO_CODENAME} stable" > /etc/apt/sources.list.d/docker.list

  # Install packages
  echo "5. Installing Docker Engine, CLI, and Docker Compose plugin..."
  apt-get update -y
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

  # Enable and start service
  echo "6. Enabling and starting Docker systemd service..."
  systemctl enable --now docker
}

# 4. Installation for Fedora
install_fedora() {
  echo "--> Installing Docker CE and Compose plugin on Fedora..."

  # Remove old or conflicting packages
  echo "1. Removing any conflicting legacy packages..."
  dnf remove -y docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-selinux docker-engine-selinux docker-engine >/dev/null 2>&1 || true

  # Install dnf-plugins-core
  echo "2. Installing dnf-plugins-core..."
  dnf -y install dnf-plugins-core

  # Add Docker CE repository
  echo "3. Adding Docker CE repository..."
  dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo

  # Install packages
  echo "4. Installing Docker Engine, CLI, and Docker Compose plugin..."
  dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

  # Enable and start service
  echo "5. Enabling and starting Docker systemd service..."
  systemctl enable --now docker
}

# Run prerequisite check
check_existing

# Run detection
detect_distribution

case "$DISTRO_ID" in
  ubuntu|pop|mint)
    install_ubuntu_debian "ubuntu"
    ;;
  debian|raspbian)
    install_ubuntu_debian "debian"
    ;;
  fedora)
    install_fedora
    ;;
  *)
    # Check ID_LIKE fallback
    if [[ "$DISTRO_ID_LIKE" == *"ubuntu"* ]]; then
      install_ubuntu_debian "ubuntu"
    elif [[ "$DISTRO_ID_LIKE" == *"debian"* ]]; then
      install_ubuntu_debian "debian"
    elif [[ "$DISTRO_ID_LIKE" == *"fedora"* ]] || [[ "$DISTRO_ID_LIKE" == *"rhel"* ]]; then
      install_fedora
    else
      echo "Error: Unsupported distribution: ${DISTRO_ID}."
      echo "DockerWorks currently provides automated setup for Ubuntu, Debian, and Fedora."
      echo "Please install Docker manually: https://docs.docker.com/engine/install/"
      exit 1
    fi
    ;;
esac

# Post-installation verification and group setup
echo ""
echo "=== Verifying Installation ==="
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  echo "✓ Docker:         $(docker --version)"
  echo "✓ Docker Compose: $(docker compose version)"

  # Configure non-root sudo user if applicable
  if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    usermod -aG docker "$SUDO_USER" || true
    echo "✓ Added user '${SUDO_USER}' to 'docker' group."
  fi

  echo "✓ Docker daemon service is active."
  echo "Setup completed successfully!"
else
  echo "Error: Docker installation completed but docker or docker compose was not found."
  exit 1
fi
