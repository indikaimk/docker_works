package registry

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestPinDigest(t *testing.T) {
	pinned, err := PinDigest("ghcr.io/myorg/app-config:latest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "ghcr.io/myorg/app-config@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if pinned != want {
		t.Errorf("got %q, want %q", pinned, want)
	}
}

func TestExtractImageToDirTarAndOrasLayers(t *testing.T) {
	// 1. Create a tar layer with docker-compose.yml
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	composeContent := "services:\n  web:\n    image: nginx:alpine\n"
	if err := tw.WriteHeader(&tar.Header{
		Name:     "docker-compose.yml",
		Mode:     0644,
		Size:     int64(len(composeContent)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(composeContent)); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()

	layer1, err := tarball.LayerFromReader(&tarBuf)
	if err != nil {
		t.Fatalf("failed to create tar layer: %v", err)
	}

	// 2. Create an ORAS-style raw file layer with org.opencontainers.image.title
	promContent := "global:\n  scrape_interval: 15s\n"
	layer2 := static.NewLayer([]byte(promContent), types.MediaType("application/vnd.unknown.config.v1+yaml"))

	img, err := mutate.Append(empty.Image,
		mutate.Addendum{Layer: layer1},
		mutate.Addendum{
			Layer: layer2,
			Annotations: map[string]string{
				"org.opencontainers.image.title": "configs/prometheus.yml",
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to build test OCI image: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Stale file that should be removed on clean extraction
	if err := os.WriteFile(filepath.Join(destDir, "stale.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := extractImageToDir(img, destDir); err != nil {
		t.Fatalf("extractImageToDir failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("expected stale.txt to be removed")
	}

	gotCompose, err := os.ReadFile(filepath.Join(destDir, "docker-compose.yml"))
	if err != nil || string(gotCompose) != composeContent {
		t.Errorf("unexpected docker-compose.yml: %q, err=%v", string(gotCompose), err)
	}

	gotProm, err := os.ReadFile(filepath.Join(destDir, "configs", "prometheus.yml"))
	if err != nil || string(gotProm) != promContent {
		t.Errorf("unexpected configs/prometheus.yml: %q, err=%v", string(gotProm), err)
	}
}

func TestExtractImageRejectsTraversal(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	data := "evil"
	_ = tw.WriteHeader(&tar.Header{
		Name:     "../../etc/cron.d/evil",
		Mode:     0644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write([]byte(data))
	_ = tw.Close()

	layer, err := tarball.LayerFromReader(&tarBuf)
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(t.TempDir(), "app")
	err = extractImageToDir(img, destDir)
	if err == nil || !strings.Contains(err.Error(), "unsafe path traversal") {
		t.Fatalf("expected path traversal error, got %v", err)
	}
}
