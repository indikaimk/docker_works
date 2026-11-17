package registry

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/indikaimk/docker_works/internal/config"
)

type mockDigestChecker struct {
	digests map[string]string
}

func (m *mockDigestChecker) GetRemoteDigest(ctx context.Context, imageRef string, authMap map[string]config.RegistryAuth) (string, error) {
	if d, ok := m.digests[imageRef]; ok {
		return d, nil
	}
	return "sha256:1234567890abcdef", nil
}

func TestMockDigestChecker(t *testing.T) {
	checker := &mockDigestChecker{
		digests: map[string]string{
			"nginx:latest": "sha256:custom-digest",
		},
	}

	digest, err := checker.GetRemoteDigest(context.Background(), "nginx:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if digest != "sha256:custom-digest" {
		t.Errorf("expected sha256:custom-digest, got %s", digest)
	}
}

func TestInvalidImageRef(t *testing.T) {
	checker := NewRemoteChecker(nil)
	_, err := checker.GetRemoteDigest(context.Background(), "INVALID::IMAGE--NAME", nil)
	if err == nil {
		t.Fatalf("expected error for invalid image ref, got nil")
	}
}

func TestPodmanKeychain(t *testing.T) {
	tempDir := t.TempDir()
	authPath := filepath.Join(tempDir, "auth.json")
	encoded := base64.StdEncoding.EncodeToString([]byte("myuser:mypass"))
	content := `{"auths":{"ghcr.io":{"auth":"` + encoded + `"}}}`
	if err := os.WriteFile(authPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write auth.json: %v", err)
	}

	t.Setenv("REGISTRY_AUTH_FILE", authPath)

	ref, err := name.ParseReference("ghcr.io/myorg/app:latest")
	if err != nil {
		t.Fatalf("failed to parse ref: %v", err)
	}

	kc := &podmanKeychain{}
	auth, err := kc.Resolve(ref.Context())
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	cfg, err := auth.Authorization()
	if err != nil {
		t.Fatalf("authorization failed: %v", err)
	}
	if cfg.Username != "myuser" || cfg.Password != "mypass" {
		t.Errorf("expected myuser/mypass, got %s/%s", cfg.Username, cfg.Password)
	}
}
