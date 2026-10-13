package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/indikaimk/docker_works/internal/config"
)

// DigestChecker retrieves the remote digest for a given image reference.
type DigestChecker interface {
	GetRemoteDigest(ctx context.Context, imageRef string, authMap map[string]config.RegistryAuth) (string, error)
}

type RemoteChecker struct {
	logger *slog.Logger
}

func NewRemoteChecker(logger *slog.Logger) *RemoteChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &RemoteChecker{logger: logger}
}

// GetRemoteDigest inspects the remote registry and returns the manifest digest (e.g. sha256:...)
// without pulling the entire image layers.
func (c *RemoteChecker) GetRemoteDigest(ctx context.Context, imageRef string, authMap map[string]config.RegistryAuth) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference '%s': %w", imageRef, err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
	}

	// Check if auth credentials are provided for the registry domain
	registryDomain := ref.Context().RegistryStr()
	var auth authn.Authenticator

	if len(authMap) > 0 {
		for domain, creds := range authMap {
			if strings.EqualFold(domain, registryDomain) {
				if creds.Token != "" {
					auth = &authn.Bearer{Token: creds.Token}
				} else if creds.Username != "" {
					auth = &authn.Basic{
						Username: creds.Username,
						Password: creds.Password,
					}
				}
				break
			}
		}
	}

	if auth != nil {
		opts = append(opts, remote.WithAuth(auth))
	} else {
		// Fallback to host Docker config (~/.docker/config.json)
		opts = append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}

	// Try remote.Head first (fastest HEAD HTTP request)
	desc, err := remote.Head(ref, opts...)
	if err == nil && desc != nil && desc.Digest.String() != "" {
		return desc.Digest.String(), nil
	}

	// Some registries don't support HEAD for manifests, fallback to remote.Get (GET manifest)
	descriptor, err := remote.Get(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest for image %s: %w", imageRef, err)
	}

	return descriptor.Digest.String(), nil
}
