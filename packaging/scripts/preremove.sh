#!/bin/sh
set -e

# Stop and disable systemd service before removal
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop docker-works || true
    systemctl disable docker-works || true
fi
