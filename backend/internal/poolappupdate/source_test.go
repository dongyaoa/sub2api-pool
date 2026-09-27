package poolappupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), ContentLength: int64(len(body))}
}
func jsonBody(value any) string { data, _ := json.Marshal(value); return string(data) }

func testRelease(binary []byte) *Release {
	hash := sha256.Sum256(binary)
	return &Release{Version: "0.2.7-pool.12", Revision: strings.Repeat("a", 40), Digest: "sha256:" + hex.EncodeToString(hash[:]), Size: int64(len(binary)), Tag: "pool-v0.2.7.12", PublishedAt: time.Now().UTC()}
}

func releaseMetadata(release *Release) map[string]any {
	return map[string]any{"tag_name": release.Tag, "prerelease": true, "published_at": release.PublishedAt, "assets": []map[string]any{
		{"name": BinaryAsset, "size": release.Size, "browser_download_url": assetURL(release.Tag, BinaryAsset)},
		{"name": ManifestAsset, "size": 300, "browser_download_url": assetURL(release.Tag, ManifestAsset)},
	}}
}

func releaseManifest(release *Release) manifest {
	return manifest{SchemaVersion: 1, Version: release.Version, Revision: release.Revision, Platform: "linux/amd64", Asset: BinaryAsset, SHA256: strings.TrimPrefix(release.Digest, "sha256:"), Size: release.Size}
}

func TestLatestSelectsPoolPrereleaseNumerically(t *testing.T) {
	release := testRelease([]byte("verified application"))
	older := *release
	older.Version = "0.2.7-pool.9"
	older.Tag = "pool-v0.2.7.9"
	calls := 0
	source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.URL.String() {
		case "https://api.github.com/repos/" + Repository + "/releases?per_page=20":
			return response(jsonBody([]any{releaseMetadata(&older), map[string]any{"tag_name": "v99.0.0"}, releaseMetadata(release)})), nil
		case assetURL(release.Tag, ManifestAsset):
			return response(jsonBody(releaseManifest(release))), nil
		default:
			t.Fatalf("unexpected URL %s", req.URL)
			return nil, nil
		}
	})})
	actual, err := source.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if actual.Version != release.Version || actual.Digest != release.Digest || actual.Revision != release.Revision || calls != 2 {
		t.Fatalf("unexpected release: %+v, calls=%d", actual, calls)
	}
	if actual.URL != "https://github.com/"+Repository+"/releases/tag/"+release.Tag {
		t.Fatal(actual.URL)
	}
}

func TestLatestIncompleteNewestDoesNotDowngrade(t *testing.T) {
	release := testRelease([]byte("binary"))
	newest := map[string]any{"tag_name": "pool-v0.2.7.13", "published_at": release.PublishedAt, "assets": []any{}}
	source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" {
			t.Fatal("must not download an older manifest")
		}
		return response(jsonBody([]any{releaseMetadata(release), newest})), nil
	})})
	if _, err := source.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("got %v", err)
	}
}

func TestLatestRejectsInvalidManifestAndAssetLocation(t *testing.T) {
	for _, kind := range []string{"wrong version", "wrong revision", "wrong platform", "wrong hash", "wrong size", "untrusted asset", "duplicate asset"} {
		t.Run(kind, func(t *testing.T) {
			release := testRelease([]byte("binary"))
			metadata := releaseMetadata(release)
			m := releaseManifest(release)
			switch kind {
			case "wrong version":
				m.Version = "0.2.7-pool.13"
			case "wrong revision":
				m.Revision = "main"
			case "wrong platform":
				m.Platform = "linux/arm64"
			case "wrong hash":
				m.SHA256 = "bad"
			case "wrong size":
				m.Size++
			case "untrusted asset":
				assets, ok := metadata["assets"].([]map[string]any)
				if !ok {
					t.Fatal("invalid fixture assets")
				}
				assets[0]["browser_download_url"] = "https://github.com/other/repo/releases/download/x/binary"
			case "duplicate asset":
				assets, ok := metadata["assets"].([]map[string]any)
				if !ok {
					t.Fatal("invalid fixture assets")
				}
				metadata["assets"] = append(assets, assets[0])
			}
			source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host == "api.github.com" {
					return response(jsonBody([]any{metadata})), nil
				}
				return response(jsonBody(m)), nil
			})})
			if _, err := source.Latest(context.Background()); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestDownloadVerifiesSizeAndChecksum(t *testing.T) {
	binary := []byte("a verified application")
	for _, body := range []string{string(binary), "a tampered application", string(binary) + "x", string(binary[:len(binary)-1])} {
		t.Run(body, func(t *testing.T) {
			release := testRelease(binary)
			source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != assetURL(release.Tag, BinaryAsset) {
					t.Fatal(req.URL)
				}
				resp := response(body)
				resp.ContentLength = -1
				return resp, nil
			})})
			path := filepath.Join(t.TempDir(), "sub2api")
			err := source.Download(context.Background(), release, path)
			if body == string(binary) {
				if err != nil {
					t.Fatal(err)
				}
				actual, _ := os.ReadFile(path)
				if string(actual) != body {
					t.Fatal("wrong file")
				}
			} else {
				if err == nil {
					t.Fatal("unverified data accepted")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("partial file left behind")
				}
			}
		})
	}
}

func TestRedirectPolicy(t *testing.T) {
	for _, target := range []string{"https://release-assets.githubusercontent.com/asset", "https://objects.githubusercontent.com/asset", "http://github.com/asset", "https://example.com/asset", "https://github.com/other/repo/asset", "https://github.com:443/asset", "https://user@release-assets.githubusercontent.com/asset"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{target}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
				}
				if req.Header.Get("Authorization") != "" {
					t.Fatal("credential leaked on redirect")
				}
				return response("ok"), nil
			})})
			resp, err := source.request(context.Background(), assetURL("pool-v0.2.7.12", ManifestAsset))
			allowed := strings.HasPrefix(target, "https://release-assets.githubusercontent.com/") || strings.HasPrefix(target, "https://objects.githubusercontent.com/")
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
			} else if err == nil {
				t.Fatal("unsafe redirect accepted")
			}
		})
	}
}

func TestReleaseAPIRejectsRedirect(t *testing.T) {
	source := NewSource(&http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com/asset"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})})
	if _, err := source.Latest(context.Background()); err == nil {
		t.Fatal("API redirect accepted")
	}
}
