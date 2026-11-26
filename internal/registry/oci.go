package registry

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/indikaimk/docker_works/internal/config"
)

const maxArtifactExtractBytes = 64 << 20 // 64 MiB

// ArtifactPuller extends DigestChecker with the ability to download and extract OCI artifacts.
type ArtifactPuller interface {
	DigestChecker
	PullArtifact(ctx context.Context, artifactRef string, authMap map[string]config.RegistryAuth, destDir string) (string, error)
}

// PinDigest returns an immutable digest-pinned reference for rollback (e.g. repo@sha256:...).
func PinDigest(artifactRef, digest string) (string, error) {
	ref, err := name.ParseReference(artifactRef)
	if err != nil {
		return "", fmt.Errorf("invalid artifact reference %q: %w", artifactRef, err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("invalid digest %q", digest)
	}
	return ref.Context().Digest(digest).Name(), nil
}

// PullArtifact fetches an OCI artifact or config image from a registry and extracts its files into destDir.
func (c *RemoteChecker) PullArtifact(ctx context.Context, artifactRef string, authMap map[string]config.RegistryAuth, destDir string) (string, error) {
	ref, err := name.ParseReference(artifactRef)
	if err != nil {
		return "", fmt.Errorf("invalid artifact reference '%s': %w", artifactRef, err)
	}

	opts := buildRemoteOptions(ctx, ref, authMap)
	img, err := remote.Image(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to pull OCI artifact %s: %w", artifactRef, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to compute digest for %s: %w", artifactRef, err)
	}

	if err := extractImageToDir(img, destDir); err != nil {
		return "", fmt.Errorf("failed to extract OCI artifact %s: %w", artifactRef, err)
	}

	return digest.String(), nil
}

func cleanArtifactPath(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("invalid empty or null path")
	}
	cleaned := filepath.Clean(strings.TrimPrefix(raw, "/"))
	if cleaned == "." {
		return ".", nil
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path traversal in artifact entry: %q", raw)
	}
	return cleaned, nil
}

func isTarMediaType(mt string) bool {
	lower := strings.ToLower(mt)
	return strings.Contains(lower, ".tar") || strings.HasSuffix(lower, "+tar")
}

func extractImageToDir(img v1.Image, destDir string) error {
	parentDir := filepath.Dir(destDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent dir %s: %w", parentDir, err)
	}

	stageDir, err := os.MkdirTemp(parentDir, ".oci-stage-*")
	if err != nil {
		return fmt.Errorf("failed to create staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to read artifact layers: %w", err)
	}
	manifest, err := img.Manifest()
	if err != nil {
		return fmt.Errorf("failed to read artifact manifest: %w", err)
	}

	var totalBytes int64
	for i, layer := range layers {
		var title string
		var mediaType string
		if i < len(manifest.Layers) {
			mediaType = string(manifest.Layers[i].MediaType)
			title = manifest.Layers[i].Annotations["org.opencontainers.image.title"]
		}

		if title != "" && !isTarMediaType(mediaType) {
			rc, err := layer.Compressed()
			if err != nil {
				return fmt.Errorf("failed to read blob layer %q: %w", title, err)
			}
			err = writeStageFile(stageDir, title, rc, 0644, &totalBytes)
			_ = rc.Close()
			if err != nil {
				return err
			}
			continue
		}

		rc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("failed to decompress tar layer: %w", err)
		}
		err = extractTarStream(stageDir, rc, &totalBytes)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}

	// Replace destDir contents cleanly with staged files.
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("failed to clean destination dir %s: %w", destDir, err)
	}
	if err := os.Rename(stageDir, destDir); err != nil {
		return fmt.Errorf("failed to activate staged artifact in %s: %w", destDir, err)
	}
	return nil
}

func writeStageFile(stageDir, rawPath string, r io.Reader, mode os.FileMode, totalBytes *int64) error {
	rel, err := cleanArtifactPath(rawPath)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	target := filepath.Join(stageDir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	remaining := maxArtifactExtractBytes - *totalBytes
	if remaining <= 0 {
		return fmt.Errorf("artifact exceeds maximum allowed size (%d bytes)", maxArtifactExtractBytes)
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := io.Copy(f, io.LimitReader(r, remaining+1))
	if err != nil {
		return err
	}
	*totalBytes += n
	if *totalBytes > maxArtifactExtractBytes {
		return fmt.Errorf("artifact exceeds maximum allowed size (%d bytes)", maxArtifactExtractBytes)
	}
	return nil
}

func extractTarStream(stageDir string, r io.Reader, totalBytes *int64) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		rel, err := cleanArtifactPath(hdr.Name)
		if err != nil {
			return err
		}
		if rel == "." {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filepath.Join(stageDir, rel), 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			mode := os.FileMode(hdr.Mode) & 0777
			if mode == 0 {
				mode = 0644
			}
			if err := writeStageFile(stageDir, rel, tr, mode, totalBytes); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("unsupported link entry %q in OCI artifact", hdr.Name)
		}
	}
}
