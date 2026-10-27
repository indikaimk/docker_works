#!/usr/bin/env bash
set -euo pipefail

# DockerWorks Installer Script
echo "=== Installing DockerWorks ==="

if [ "$EUID" -ne 0 ]; then
  echo "Error: Please run as root (e.g. using sudo)."
  exit 1
fi

VERSION="${VERSION:-0.1.0}"
GITHUB_REPO="indikaimk/docker_works"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/docker-works"
STATE_DIR="/var/lib/docker-works"
SYSTEMD_DIR="/etc/systemd/system"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || pwd)"

# Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) TARGET_ARCH="amd64" ;;
  aarch64|arm64) TARGET_ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH (only Linux Intel amd64 and ARM arm64 are supported)."
    exit 1
    ;;
esac

echo "Detected platform: ${OS}/${TARGET_ARCH}"

echo "1. Checking Docker & Compose prerequisites..."
if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  echo "   Docker or Docker Compose plugin not found on this host."
  if [ -f "${SCRIPT_DIR}/setup-docker.sh" ]; then
    echo "   Running automated Docker setup for host..."
    bash "${SCRIPT_DIR}/setup-docker.sh"
  elif command -v curl >/dev/null 2>&1; then
    echo "   Fetching and running host Docker setup script..."
    curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/scripts/setup-docker.sh" | bash
  else
    echo "   Error: Docker is required. Please install Docker and docker compose plugin."
    exit 1
  fi
else
  echo "   Docker and Docker Compose are already present:"
  echo "   - $(docker --version)"
  echo "   - $(docker compose version)"
fi

echo "2. Installing DockerWorks binary..."
TMP_DIR=""
cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

# Check for existing binary in local directory (e.g. inside release tarball or local build)
PREBUILT_BIN=""
if [ -f "./docker-works" ]; then
  PREBUILT_BIN="./docker-works"
elif [ -f "${SCRIPT_DIR}/../docker-works" ]; then
  PREBUILT_BIN="${SCRIPT_DIR}/../docker-works"
elif [ -f "${SCRIPT_DIR}/docker-works" ]; then
  PREBUILT_BIN="${SCRIPT_DIR}/docker-works"
elif [ -f "./bin/docker-works" ]; then
  PREBUILT_BIN="./bin/docker-works"
fi

if [ -n "$PREBUILT_BIN" ]; then
  echo "   Using local pre-compiled binary: ${PREBUILT_BIN}"
  cp "${PREBUILT_BIN}" "${BIN_DIR}/docker-works"
elif [ "${BUILD_FROM_SOURCE:-0}" = "1" ] && command -v go >/dev/null 2>&1 && [ -f "./cmd/docker-works/main.go" ]; then
  echo "   Building binary from local source with Go..."
  go build -ldflags="-s -w" -o /tmp/docker-works ./cmd/docker-works
  mv /tmp/docker-works "${BIN_DIR}/docker-works"
else
  echo "   Downloading pre-compiled binary for ${OS}/${TARGET_ARCH} (v${VERSION})..."
  TMP_DIR="$(mktemp -d)"
  TAR_URL="https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/docker-works_${VERSION}_${OS}_${TARGET_ARCH}.tar.gz"
  
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$TAR_URL" -o "${TMP_DIR}/archive.tar.gz"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${TMP_DIR}/archive.tar.gz" "$TAR_URL"
  else
    echo "   Error: curl or wget is required to download binary."
    exit 1
  fi

  tar -xzf "${TMP_DIR}/archive.tar.gz" -C "$TMP_DIR"
  cp "${TMP_DIR}/docker-works" "${BIN_DIR}/docker-works"
fi

chmod +x "${BIN_DIR}/docker-works"
echo "   Installed ${BIN_DIR}/docker-works"

echo "3. Setting up directories..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${STATE_DIR}/apps"

if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
  CONFIG_SRC=""
  for p in "./examples/config.example.yaml" "${SCRIPT_DIR}/../examples/config.example.yaml" "${SCRIPT_DIR}/config.example.yaml" "${TMP_DIR:-}/examples/config.example.yaml"; do
    if [ -f "$p" ]; then
      CONFIG_SRC="$p"
      break
    fi
  done

  if [ -n "$CONFIG_SRC" ]; then
    echo "   Copying example configuration to ${CONFIG_DIR}/config.yaml"
    cp "$CONFIG_SRC" "${CONFIG_DIR}/config.yaml"
  elif command -v curl >/dev/null 2>&1; then
    echo "   Fetching example configuration from repository..."
    curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/examples/config.example.yaml" -o "${CONFIG_DIR}/config.yaml"
  fi
  chmod 0600 "${CONFIG_DIR}/config.yaml" 2>/dev/null || true
fi

echo "4. Installing systemd unit..."
SERVICE_SRC=""
for p in "./packaging/systemd/docker-works.service" "${SCRIPT_DIR}/../packaging/systemd/docker-works.service" "${SCRIPT_DIR}/docker-works.service" "${TMP_DIR:-}/packaging/systemd/docker-works.service"; do
  if [ -f "$p" ]; then
    SERVICE_SRC="$p"
    break
  fi
done

if [ -n "$SERVICE_SRC" ]; then
  cp "$SERVICE_SRC" "${SYSTEMD_DIR}/docker-works.service"
elif command -v curl >/dev/null 2>&1; then
  echo "   Fetching systemd service definition..."
  curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/packaging/systemd/docker-works.service" -o "${SYSTEMD_DIR}/docker-works.service"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

echo "=== Installation Complete! ==="
echo "Version:             $("${BIN_DIR}/docker-works" version 2>/dev/null || echo "v${VERSION}")"
echo "Edit configuration:  nano ${CONFIG_DIR}/config.yaml"
echo "Enable & start:      systemctl enable --now docker-works"
echo "Check status & logs: systemctl status docker-works"
echo "                     journalctl -u docker-works -f"
