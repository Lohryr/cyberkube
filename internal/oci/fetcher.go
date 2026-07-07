// Package oci fetches static-challenge attachments published as OCI
// artifacts, with an in-memory cache keyed by content digest.
package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Fetcher pulls attachment blobs from an OCI registry and caches them by
// their sha256 digest. The registry must already be reachable (it serves all
// challenge images), so this adds no new hard dependency.
type Fetcher struct {
	cache *cache
	creds auth.CredentialFunc
}

// NewFetcher builds a Fetcher. creds may be nil for anonymous pulls.
func NewFetcher(creds auth.CredentialFunc) *Fetcher {
	return &Fetcher{cache: newCache(), creds: creds}
}

// Fetch returns the single-file artifact content at ref, verifying it against
// the expected sha256. Results are cached by digest.
func (f *Fetcher) Fetch(ctx context.Context, ref, sha256hex string) ([]byte, error) {
	if cached, ok := f.cache.get(sha256hex); ok {
		return cached, nil
	}

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("parse OCI ref %q: %w", ref, err)
	}
	if f.creds != nil {
		repo.Client = &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache(), Credential: f.creds}
	}

	store := memory.New()
	manifestDesc, err := oras.Copy(ctx, repo, repo.Reference.Reference, store, "", oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("pull %q: %w", ref, err)
	}

	blob, err := firstLayerBlob(ctx, store, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("read attachment blob from %q: %w", ref, err)
	}

	sum := sha256.Sum256(blob)
	if got := hex.EncodeToString(sum[:]); got != sha256hex {
		return nil, fmt.Errorf("attachment digest mismatch for %q: got %s want %s", ref, got, sha256hex)
	}

	f.cache.put(sha256hex, blob)
	return blob, nil
}

// firstLayerBlob returns the content of a manifest's first layer — the
// convention for single-file OCI artifacts produced by `oras push`.
func firstLayerBlob(ctx context.Context, store content.Fetcher, manifestDesc ocispec.Descriptor) ([]byte, error) {
	manifestBytes, err := content.FetchAll(ctx, store, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("artifact has no layers")
	}
	blob, err := content.FetchAll(ctx, store, manifest.Layers[0])
	if err != nil {
		return nil, fmt.Errorf("fetch layer: %w", err)
	}
	return blob, nil
}
