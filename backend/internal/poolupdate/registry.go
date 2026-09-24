package poolupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Repository is the only image repository the pool updater can resolve.
const Repository = "ghcr.io/dongyaoa/sub2api-pool"

const (
	registryHost    = "ghcr.io"
	registryPath    = "/v2/dongyaoa/sub2api-pool/"
	ociIndex        = "application/vnd.oci.image.index.v1+json"
	ociManifest     = "application/vnd.oci.image.manifest.v1+json"
	dockerIndex     = "application/vnd.docker.distribution.manifest.list.v2+json"
	dockerManifest  = "application/vnd.docker.distribution.manifest.v2+json"
	ociConfig       = "application/vnd.oci.image.config.v1+json"
	dockerConfig    = "application/vnd.docker.container.image.v1+json"
	maxRegistryBody = 4 << 20
)

var (
	poolVersionPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-pool\.(0|[1-9][0-9]*)$`)
	digestPattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	revisionPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ghcrBlobPathPattern = regexp.MustCompile(`^/ghcrblobs[0-9]+/blobs/sha256:[a-f0-9]{64}$`)
)

// Release contains verified metadata for a Linux amd64 image manifest.
type Release struct {
	Version  string    `json:"version"`
	Revision string    `json:"revision"`
	Digest   string    `json:"digest"`
	Image    string    `json:"image"`
	Created  time.Time `json:"created"`
}

// ValidateVersion accepts only the version format published by Pool Images.
func ValidateVersion(version string) error {
	if len(version) > 128 || !poolVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid pool version %q", version)
	}
	return nil
}

// CompareVersions compares all four numeric components without integer overflow.
func CompareVersions(a, b string) (int, error) {
	if err := ValidateVersion(a); err != nil {
		return 0, err
	}
	if err := ValidateVersion(b); err != nil {
		return 0, err
	}
	left := strings.Split(strings.Replace(a, "-pool.", ".", 1), ".")
	right := strings.Split(strings.Replace(b, "-pool.", ".", 1), ".")
	for i := range left {
		if len(left[i]) < len(right[i]) {
			return -1, nil
		}
		if len(left[i]) > len(right[i]) {
			return 1, nil
		}
		if comparison := strings.Compare(left[i], right[i]); comparison != 0 {
			return comparison, nil
		}
	}
	return 0, nil
}

// ValidateDigest accepts canonical lowercase SHA-256 image digests.
func ValidateDigest(digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("invalid image digest %q", digest)
	}
	return nil
}

// Registry resolves release metadata from the fixed public GHCR repository.
type Registry struct {
	client *http.Client
}

func NewRegistry() *Registry {
	return &Registry{client: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

type registryDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}

type registryManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Manifests     []registryDescriptor `json:"manifests"`
	Config        registryDescriptor   `json:"config"`
}

// Latest resolves latest once, then uses content-addressed requests throughout.
func (r *Registry) Latest(ctx context.Context) (*Release, error) {
	tokenBody, _, err := r.get(ctx, "https://ghcr.io/token?service=ghcr.io&scope=repository%3Adongyaoa%2Fsub2api-pool%3Apull", "", "application/json")
	if err != nil {
		return nil, fmt.Errorf("get registry token: %w", err)
	}
	var authorization struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenBody, &authorization); err != nil {
		return nil, fmt.Errorf("decode registry token: %w", err)
	}
	token := authorization.Token
	if token == "" {
		token = authorization.AccessToken
	}
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("registry returned an invalid token")
	}

	manifest, digest, _, err := r.manifest(ctx, token, "latest")
	if err != nil {
		return nil, err
	}
	if manifest.MediaType == ociIndex || manifest.MediaType == dockerIndex {
		var target *registryDescriptor
		for i := range manifest.Manifests {
			descriptor := &manifest.Manifests[i]
			if descriptor.Platform.OS != "linux" || descriptor.Platform.Architecture != "amd64" || descriptor.Platform.Variant != "" {
				continue
			}
			if target != nil {
				return nil, fmt.Errorf("registry index has multiple linux/amd64 images")
			}
			target = descriptor
		}
		if target == nil {
			return nil, fmt.Errorf("registry index has no linux/amd64 image")
		}
		if err := validateRegistryDescriptor(*target, ociManifest, dockerManifest); err != nil {
			return nil, fmt.Errorf("invalid platform manifest: %w", err)
		}
		var size int64
		manifest, digest, size, err = r.manifest(ctx, token, target.Digest)
		if err != nil {
			return nil, err
		}
		if manifest.MediaType != target.MediaType || size != target.Size {
			return nil, fmt.Errorf("platform manifest does not match its descriptor")
		}
	}
	if manifest.MediaType != ociManifest && manifest.MediaType != dockerManifest {
		return nil, fmt.Errorf("registry did not return an image manifest")
	}
	if err := validateRegistryDescriptor(manifest.Config, ociConfig, dockerConfig); err != nil {
		return nil, fmt.Errorf("invalid image config: %w", err)
	}
	configBody, configHeader, err := r.get(ctx, "https://"+registryHost+registryPath+"blobs/"+manifest.Config.Digest, token, manifest.Config.MediaType)
	if err != nil {
		return nil, fmt.Errorf("get image config: %w", err)
	}
	if _, err := verifyRegistryDigest(configBody, configHeader, manifest.Config.Digest); err != nil {
		return nil, fmt.Errorf("verify image config: %w", err)
	}
	if int64(len(configBody)) != manifest.Config.Size {
		return nil, fmt.Errorf("image config size does not match its descriptor")
	}
	var config struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		Created      string `json:"created"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configBody, &config); err != nil {
		return nil, fmt.Errorf("decode image config: %w", err)
	}
	if config.OS != "linux" || config.Architecture != "amd64" {
		return nil, fmt.Errorf("image config platform is not linux/amd64")
	}
	version := config.Config.Labels["org.opencontainers.image.version"]
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	revision := config.Config.Labels["org.opencontainers.image.revision"]
	if !revisionPattern.MatchString(revision) {
		return nil, fmt.Errorf("image revision must be a full lowercase commit SHA")
	}
	created := config.Config.Labels["org.opencontainers.image.created"]
	if created == "" {
		created = config.Created
	}
	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil || createdAt.IsZero() {
		return nil, fmt.Errorf("image has an invalid creation time")
	}
	return &Release{Version: version, Revision: revision, Digest: digest, Image: Repository + "@" + digest, Created: createdAt.UTC()}, nil
}

func validateRegistryDescriptor(descriptor registryDescriptor, mediaTypes ...string) error {
	if err := ValidateDigest(descriptor.Digest); err != nil {
		return err
	}
	if descriptor.Size <= 0 || descriptor.Size > maxRegistryBody {
		return fmt.Errorf("invalid descriptor size")
	}
	for _, mediaType := range mediaTypes {
		if descriptor.MediaType == mediaType {
			return nil
		}
	}
	return fmt.Errorf("unsupported descriptor media type %q", descriptor.MediaType)
}

func (r *Registry) manifest(ctx context.Context, token, reference string) (*registryManifest, string, int64, error) {
	if reference != "latest" {
		if err := ValidateDigest(reference); err != nil {
			return nil, "", 0, err
		}
	}
	body, headers, err := r.get(ctx, "https://"+registryHost+registryPath+"manifests/"+reference, token, strings.Join([]string{ociIndex, dockerIndex, ociManifest, dockerManifest}, ", "))
	if err != nil {
		return nil, "", 0, fmt.Errorf("get registry manifest: %w", err)
	}
	expected := reference
	if reference == "latest" {
		expected = ""
	}
	if headers.Get("Docker-Content-Digest") == "" {
		return nil, "", 0, fmt.Errorf("registry manifest omitted content digest")
	}
	digest, err := verifyRegistryDigest(body, headers, expected)
	if err != nil {
		return nil, "", 0, fmt.Errorf("verify registry manifest: %w", err)
	}
	var manifest registryManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, "", 0, fmt.Errorf("decode registry manifest: %w", err)
	}
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil || mediaType != manifest.MediaType || manifest.SchemaVersion != 2 {
		return nil, "", 0, fmt.Errorf("invalid registry manifest media type or schema")
	}
	switch manifest.MediaType {
	case ociIndex, dockerIndex, ociManifest, dockerManifest:
		return &manifest, digest, int64(len(body)), nil
	default:
		return nil, "", 0, fmt.Errorf("unsupported registry manifest media type")
	}
}

func verifyRegistryDigest(body []byte, headers http.Header, expected string) (string, error) {
	digest := headers.Get("Docker-Content-Digest")
	// Signed GHCR storage responses do not carry Docker's digest header.
	// The descriptor from the verified manifest still pins every byte.
	if digest == "" && expected != "" {
		digest = expected
	}
	if err := ValidateDigest(digest); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) || (expected != "" && digest != expected) {
		return "", fmt.Errorf("registry content digest mismatch")
	}
	return digest, nil
}

func (r *Registry) get(ctx context.Context, endpoint, token, accept string) ([]byte, http.Header, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host != registryHost || u.User != nil || u.Fragment != "" || (u.Path != "/token" && !strings.HasPrefix(u.Path, registryPath)) {
		return nil, nil, fmt.Errorf("registry URL is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := r.client
	if client == nil {
		client = NewRegistry().client
	}
	// Copy so callers cannot enable redirects and leak the bearer token.
	requestClient := *client
	requestClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := requestClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTemporaryRedirect && strings.HasPrefix(u.Path, registryPath+"blobs/") {
		expected := strings.TrimPrefix(u.Path, registryPath+"blobs/")
		if err := ValidateDigest(expected); err != nil {
			return nil, nil, err
		}
		location, err := url.Parse(resp.Header.Get("Location"))
		if err != nil || location.Scheme != "https" || location.Host != "pkg-containers.githubusercontent.com" || location.User != nil || location.Fragment != "" || location.RawPath != "" || !ghcrBlobPathPattern.MatchString(location.Path) || !strings.HasSuffix(location.Path, "/"+expected) {
			return nil, nil, fmt.Errorf("registry blob redirect is not allowed")
		}
		blobReq, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid registry blob request")
		}
		blobReq.Header.Set("Accept", accept)
		// Deliberately do not copy the registry Authorization header. Redirects
		// stay disabled, so this signed storage URL can make only one hop.
		blobResp, err := requestClient.Do(blobReq)
		if err != nil {
			return nil, nil, fmt.Errorf("registry blob storage request failed")
		}
		defer func() { _ = blobResp.Body.Close() }()
		if blobResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("registry blob storage returned HTTP %d", blobResp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(blobResp.Body, maxRegistryBody+1))
		if err != nil {
			return nil, nil, fmt.Errorf("registry blob read failed")
		}
		if len(body) > maxRegistryBody {
			return nil, nil, fmt.Errorf("registry response exceeds size limit")
		}
		if _, err := verifyRegistryDigest(body, blobResp.Header, expected); err != nil {
			return nil, nil, err
		}
		return body, blobResp.Header, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxRegistryBody {
		return nil, nil, fmt.Errorf("registry response exceeds size limit")
	}
	return body, resp.Header, nil
}
