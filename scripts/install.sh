#!/usr/bin/env bash
set -euo pipefail

# DockerWorks Installer Script
echo "=== Installing DockerWorks ==="

if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (e.g. using sudo)."
  exit 1
fi

BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/docker-works"
STATE_DIR="/var/lib/docker-works"
SYSTEMD_DIR="/etc/systemd/system"

echo "1. Building binary..."
go build -ldflags="-s -w" -o /tmp/docker-works ./cmd/docker-works
mv /tmp/docker-works "${BIN_DIR}/docker-works"
chmod +x "${BIN_DIR}/docker-works"
echo "   Installed ${BIN_DIR}/docker-works"

echo "2. Setting up directories..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${STATE_DIR}/apps"

if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
  echo "   Copying example config to ${CONFIG_DIR}/config.yaml"
  cp examples/config.example.yaml "${CONFIG_DIR}/config.yaml"
fi

echo "3. Installing systemd unit..."
cp packaging/systemd/docker-works.service "${SYSTEMD_DIR}/docker-works.service"
systemctl daemon-reload

echo "=== Installation Complete! ==="
echo "Edit configuration:  nano ${CONFIG_DIR}/config.yaml"
echo "Enable & start:      systemctl enable --now docker-works"
echo "Check status & logs: systemctl status docker-works"
echo "                     journalctl -u docker-works -f"
