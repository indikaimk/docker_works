# Development & Releases

## Local Development

A root `Makefile` provides common developer targets:

```bash
# Run unit tests with race detection and coverage
make test

# Run go vet and check gofmt formatting
make lint

# Run doctor against the local environment
make doctor

# Build binary into bin/docker-works
make build
```

## Previewing Documentation Locally

If you have Python and `mkdocs-material` installed:

```bash
pip install mkdocs-material
mkdocs serve
```

## Releases & Packaging

Releases are automated via GitHub Actions (`.github/workflows/release.yml`) and GoReleaser (`.goreleaser.yaml`):

1. Tag and push a semantic version:

   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. GoReleaser builds:
   - Static binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`
   - `.deb` and `.rpm` packages including systemd units (`docker-works.service`, `docker-works-podman.service`, `docker-works-podman-user.service`)
   - `checksums.txt` and release notes

## License

MIT License. See `LICENSE.txt` in the repository root.
