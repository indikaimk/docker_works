package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
		// Fallback to host Docker (~/.docker/config.json) and Podman (auth.json) credentials
		opts = append(opts, remote.WithAuthFromKeychain(
			authn.NewMultiKeychain(authn.DefaultKeychain, &podmanKeychain{}),
		))
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

// podmanKeychain resolves registry credentials from Podman auth.json files.
type podmanKeychain struct{}

type authFile struct {
	Auths map[string]struct {
		Auth string `json:"auth"`
	} `json:"auths"`
}

func (k *podmanKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	registry := res.RegistryStr()
	for _, path := range podmanAuthPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var af authFile
		if err := json.Unmarshal(data, &af); err != nil {
			continue
		}
		for key, entry := range af.Auths {
			normalized := strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
			normalized = strings.SplitN(normalized, "/", 2)[0]
			if !strings.EqualFold(normalized, registry) || entry.Auth == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				continue
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return &authn.Basic{Username: parts[0], Password: parts[1]}, nil
			}
		}
	}
	return authn.Anonymous, nil
}

func podmanAuthPaths() []string {
	var paths []string
	if p := os.Getenv("REGISTRY_AUTH_FILE"); p != "" {
		paths = append(paths, p)
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "containers", "auth.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "containers", "auth.json"))
	}
	return paths
}
