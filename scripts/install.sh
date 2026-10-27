#!/usr/bin/env bash
set -euo pipefail

# DockerWorks Installer Script
# Supports direct execution via curl:
#   curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- --version 0.1.0

VERSION="${VERSION:-}"
SKIP_DOCKER="${SKIP_DOCKER:-0}"
BUILD_FROM_SOURCE="${BUILD_FROM_SOURCE:-0}"

# Parse optional arguments first so --help works without sudo
while [ $# -gt 0 ]; do
  case "$1" in
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    --skip-docker)
      SKIP_DOCKER=1
      shift
      ;;
    --build)
      BUILD_FROM_SOURCE=1
      shift
      ;;
    -h|--help)
      echo "DockerWorks Automated Installer"
      echo ""
      echo "Usage:"
      echo "  curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash"
      echo "  curl -fsSL https://raw.githubusercontent.com/indikaimk/docker_works/main/scripts/install.sh | sudo bash -s -- [options]"
      echo ""
      echo "Options:"
      echo "  -v, --version <ver>   Install a specific release version (default: latest)"
      echo "      --skip-docker     Skip automatic Docker & Compose installation check"
      echo "      --build           Force building from local source (requires Go)"
      echo "  -h, --help            Show this help message"
      exit 0
      ;;
    *)
      echo "Warning: Unknown option '$1' ignored"
      shift
      ;;
  esac
done

echo "=== Installing DockerWorks ==="

if [ "$EUID" -ne 0 ]; then
  echo "Error: Please run as root (e.g. using sudo)."
  exit 1
fi

GITHUB_REPO="indikaimk/docker_works"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/docker-works"
STATE_DIR="/var/lib/docker-works"
SYSTEMD_DIR="/etc/systemd/system"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || pwd)"

# Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) TARGET_ARCH="amd64" ;;
  aarch64|arm64) TARGET_ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH (DockerWorks provides binaries for Intel/AMD amd64 and ARM arm64)."
    exit 1
    ;;
esac

echo "Detected platform: ${OS}/${TARGET_ARCH}"

# Discover latest version if not explicitly supplied
get_latest_version() {
  local tag=""
  if command -v curl >/dev/null 2>&1; then
    tag=$(curl -sSL -o /dev/null -w "%{url_effective}" "https://github.com/${GITHUB_REPO}/releases/latest" 2>/dev/null | awk -F'/' '{print $NF}' || true)
  fi
  if [ -n "$tag" ] && [ "$tag" != "latest" ] && [ "$tag" != "releases" ]; then
    echo "${tag#v}"
    return 0
  fi
  echo "0.1.0"
}

if [ -z "$VERSION" ]; then
  VERSION="$(get_latest_version)"
fi
VERSION="${VERSION#v}"

# 1. Check Docker & Compose prerequisites
if [ "${SKIP_DOCKER}" != "1" ]; then
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
else
  echo "1. Skipping Docker check (--skip-docker specified)."
fi

# 2. Installing binary
echo "2. Installing DockerWorks binary..."
TMP_DIR=""
cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

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

if [ "$BUILD_FROM_SOURCE" = "1" ] && command -v go >/dev/null 2>&1 && [ -f "./cmd/docker-works/main.go" ]; then
  echo "   Building binary from local source with Go..."
  go build -ldflags="-s -w" -o /tmp/docker-works ./cmd/docker-works
  mv /tmp/docker-works "${BIN_DIR}/docker-works"
elif [ -n "$PREBUILT_BIN" ]; then
  echo "   Using local pre-compiled binary: ${PREBUILT_BIN}"
  cp "${PREBUILT_BIN}" "${BIN_DIR}/docker-works"
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

# 3. Setting up directories & configuration
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

# 4. Installing systemd unit
echo "4. Installing systemd unit..."
SERVICE_SRC=""
for p in "./packaging/systemd/docker-works.service" "${SCRIPT_DIR}/../packaging/systemd/docker-works.service" "${SCRIPT_DIR}/docker-works.service" "${TMP_DIR:-}/packaging/systemd/docker-works.service"; do
  if [ -f "$p" ]; then
    SERVICE_SRC="$p"
    break
  fi
done

if command -v systemctl >/dev/null 2>&1; then
  if [ -n "$SERVICE_SRC" ]; then
    cp "$SERVICE_SRC" "${SYSTEMD_DIR}/docker-works.service"
  elif command -v curl >/dev/null 2>&1; then
    echo "   Fetching systemd service definition..."
    curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/packaging/systemd/docker-works.service" -o "${SYSTEMD_DIR}/docker-works.service"
  fi
  systemctl daemon-reload
  echo "   Installed ${SYSTEMD_DIR}/docker-works.service"
else
  echo "   Notice: systemctl not found; skipping systemd unit installation."
fi

echo "=== Installation Complete! ==="
echo "Version:             $("${BIN_DIR}/docker-works" version 2>/dev/null || echo "v${VERSION}")"
echo "Binary:              ${BIN_DIR}/docker-works"
echo "Configuration:       ${CONFIG_DIR}/config.yaml"
echo "Enable & start:      systemctl enable --now docker-works"
echo "Check status & logs: systemctl status docker-works"
echo "                     journalctl -u docker-works -f"

