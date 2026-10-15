#!/bin/sh
set -e

# Create directories
mkdir -p /var/lib/docker-works/apps
mkdir -p /etc/docker-works

# Place example configuration if not present
if [ ! -f /etc/docker-works/config.yaml ] && [ -f /etc/docker-works/config.example.yaml ]; then
    cp /etc/docker-works/config.example.yaml /etc/docker-works/config.yaml
    chmod 0600 /etc/docker-works/config.yaml || true
fi

# Reload systemd
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi
