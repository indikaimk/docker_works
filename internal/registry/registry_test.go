package registry

import (
	"context"
	"testing"

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
