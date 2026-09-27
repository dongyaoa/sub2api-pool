// Package poolappupdate implements verified, in-place updates from this fork's releases.
package poolappupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

const (
	Repository      = "dongyaoa/sub2api-pool"
	BinaryAsset     = "sub2api-linux-amd64"
	ManifestAsset   = "pool-update.json"
	maxMetadataSize = 4 << 20
	maxManifestSize = 16 << 10
	maxBinarySize   = 500 << 20
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var tagPattern = regexp.MustCompile(`^pool-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Release struct {
	Version     string    `json:"version"`
	Revision    string    `json:"revision"`
	Digest      string    `json:"digest"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"url"`
	Body        string    `json:"body"`
	Size        int64     `json:"size"`
	Tag         string    `json:"tag"`
}

type Source interface {
	Latest(context.Context) (*Release, error)
	Download(context.Context, *Release, string) error
}

type GitHubSource struct {
	client *http.Client
	token  string
}

// NewSource clones the client, preserving its proxy transport and enforcing a
// bounded download timeout and a strict GitHub redirect policy.
func NewSource(client *http.Client) *GitHubSource {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	cloned.Timeout = 12 * time.Minute
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Authorization")
		if len(via) >= 5 || !allowedAssetRedirect(req.URL) {
			return fmt.Errorf("release download redirect is not allowed")
		}
		if len(via) == 0 || via[0].URL.Host != "github.com" {
			return fmt.Errorf("release API redirects are not allowed")
		}
		if req.URL.Host == "github.com" && req.URL.Path != via[0].URL.Path {
			return fmt.Errorf("release asset path changed")
		}
		return nil
	}
	return &GitHubSource{client: &cloned, token: os.Getenv("UPDATE_GITHUB_TOKEN")}
}

func NewSourceFromEnvironment() (Source, error) {
	client, err := httpclient.GetClient(httpclient.Options{Timeout: 12 * time.Minute, ProxyURL: os.Getenv("UPDATE_PROXY_URL")})
	if err != nil {
		return nil, fmt.Errorf("initialize update proxy: %w", err)
	}
	return NewSource(client), nil
}

func allowedAssetRedirect(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	return u.Host == "github.com" || u.Host == "release-assets.githubusercontent.com" || u.Host == "objects.githubusercontent.com"
}

func versionFromTag(tag string) string {
	parts := tagPattern.FindStringSubmatch(tag)
	if parts == nil {
		return ""
	}
	return strings.Join(parts[1:4], ".") + "-pool." + parts[4]
}

func assetURL(tag, name string) string {
	return "https://github.com/" + Repository + "/releases/download/" + tag + "/" + name
}

func (s *GitHubSource) request(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Sub2API-Pool-Updater")
	if req.URL.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		if s.token != "" {
			req.Header.Set("Authorization", "Bearer "+s.token)
		}
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("release download request failed")
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("release server returned HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (s *GitHubSource) metadata(ctx context.Context, address string, limit int64) ([]byte, error) {
	response, err := s.request(ctx, address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength > limit {
		return nil, fmt.Errorf("release metadata exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read release metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("release metadata exceeds size limit")
	}
	return data, nil
}

type githubRelease struct {
	Tag         string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

type manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Revision      string `json:"revision"`
	Platform      string `json:"platform"`
	Asset         string `json:"asset"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
}

func (s *GitHubSource) Latest(ctx context.Context) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := s.metadata(ctx, "https://api.github.com/repos/"+Repository+"/releases?per_page=20", maxMetadataSize)
	if err != nil {
		return nil, err
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("invalid release list")
	}
	var latest *githubRelease
	var version string
	for i := range releases {
		item := &releases[i]
		candidate := versionFromTag(item.Tag)
		if item.Draft || candidate == "" || poolupdate.ValidateVersion(candidate) != nil {
			continue
		}
		order := 1
		if latest != nil {
			order, _ = poolupdate.CompareVersions(candidate, version)
		}
		if order > 0 {
			latest, version = item, candidate
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no published Pool release found")
	}
	// Pick the newest version before validating its assets. An incomplete release
	// must never make the updater quietly offer an older version.
	var binarySize int64
	seen := map[string]bool{}
	for _, asset := range latest.Assets {
		if asset.Name != BinaryAsset && asset.Name != ManifestAsset {
			continue
		}
		if seen[asset.Name] || asset.URL != assetURL(latest.Tag, asset.Name) {
			return nil, fmt.Errorf("invalid Pool release asset")
		}
		seen[asset.Name] = true
		if asset.Name == BinaryAsset {
			binarySize = asset.Size
		}
		if asset.Name == ManifestAsset && (asset.Size <= 0 || asset.Size > maxManifestSize) {
			return nil, fmt.Errorf("invalid Pool update manifest size")
		}
	}
	if !seen[BinaryAsset] || !seen[ManifestAsset] {
		return nil, fmt.Errorf("latest Pool release is missing online update assets")
	}
	data, err = s.metadata(ctx, assetURL(latest.Tag, ManifestAsset), maxManifestSize)
	if err != nil {
		return nil, err
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || m.SchemaVersion != 1 || m.Version != version || !revisionPattern.MatchString(m.Revision) || m.Platform != "linux/amd64" || m.Asset != BinaryAsset || !hashPattern.MatchString(m.SHA256) || m.Size <= 0 || m.Size > maxBinarySize || m.Size != binarySize || latest.PublishedAt.IsZero() {
		return nil, fmt.Errorf("invalid Pool update manifest")
	}
	return &Release{Version: version, Revision: m.Revision, Digest: "sha256:" + m.SHA256, PublishedAt: latest.PublishedAt, URL: "https://github.com/" + Repository + "/releases/tag/" + latest.Tag, Body: latest.Body, Size: m.Size, Tag: latest.Tag}, nil
}

func validateRelease(release *Release) error {
	if release == nil || poolupdate.ValidateVersion(release.Version) != nil || versionFromTag(release.Tag) != release.Version || !revisionPattern.MatchString(release.Revision) || poolupdate.ValidateDigest(release.Digest) != nil || release.Size <= 0 || release.Size > maxBinarySize {
		return fmt.Errorf("invalid update target")
	}
	return nil
}

func (s *GitHubSource) Download(ctx context.Context, release *Release, dest string) (resultErr error) {
	if err := validateRelease(release); err != nil {
		return err
	}
	response, err := s.request(ctx, assetURL(release.Tag, BinaryAsset))
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength > 0 && response.ContentLength != release.Size {
		return fmt.Errorf("binary size differs from release manifest")
	}
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if resultErr != nil {
			_ = os.Remove(dest)
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, release.Size+1))
	if err != nil {
		return fmt.Errorf("download update binary: %w", err)
	}
	if size != release.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != release.Digest {
		return fmt.Errorf("update binary checksum or size mismatch")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}
