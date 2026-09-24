package poolupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryLiveGHCR(t *testing.T) {
	if os.Getenv("POOL_UPDATER_LIVE_REGISTRY") != "1" {
		t.Skip("set POOL_UPDATER_LIVE_REGISTRY=1 for the public GHCR read-only check")
	}
	registry := NewRegistry()
	registry.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err == nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
			if location, parseErr := url.Parse(resp.Header.Get("Location")); parseErr == nil {
				t.Logf("redirect metadata only: scheme=%s host=%s path=%s", location.Scheme, location.Host, location.Path)
			}
		}
		return resp, err
	})
	release, err := registry.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verified release: version=%s revision=%s digest=%s", release.Version, release.Revision, release.Digest)
}

func TestRegistryBlobRedirectOnlyAllowsPinnedGHCRStorage(t *testing.T) {
	for _, test := range []string{"valid", "wrong host", "wrong digest path", "port", "userinfo", "fragment", "second redirect", "wrong content"} {
		t.Run(test, func(t *testing.T) {
			fixture := newRegistryFixture(t, true, false, nil)
			config := fixture.responses[fixture.config]
			digest := strings.TrimPrefix(fixture.config, registryPath+"blobs/")
			location := "https://pkg-containers.githubusercontent.com/ghcrblobs13/blobs/" + digest + "?signature=fixture"
			switch test {
			case "wrong host":
				location = strings.Replace(location, "pkg-containers.githubusercontent.com", "evil.example", 1)
			case "wrong digest path":
				location = strings.Replace(location, digest, "sha256:"+strings.Repeat("d", 64), 1)
			case "port":
				location = strings.Replace(location, ".com/", ".com:444/", 1)
			case "userinfo":
				location = strings.Replace(location, "https://", "https://attacker@", 1)
			case "fragment":
				location += "#fragment"
			}
			storageRequests := 0
			registry := NewRegistry()
			registry.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) {
				response := registryResponse{}
				if req.URL.Host == registryHost {
					response = fixture.responses[req.URL.Path]
					if req.URL.Path == fixture.config {
						response = registryResponse{status: http.StatusTemporaryRedirect, location: location}
					}
				} else {
					storageRequests++
					if req.Header.Get("Authorization") != "" {
						t.Fatal("registry bearer leaked to storage")
					}
					response = config
					response.digest = ""
					if test == "second redirect" {
						response.status = http.StatusTemporaryRedirect
						response.location = "https://evil.example/"
					}
					if test == "wrong content" {
						response.body = []byte("corrupted")
					}
				}
				status := response.status
				if status == 0 {
					status = http.StatusOK
				}
				header := http.Header{}
				header.Set("Content-Type", response.mediaType)
				header.Set("Docker-Content-Digest", response.digest)
				header.Set("Location", response.location)
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(string(response.body))), Request: req}, nil
			})
			_, err := registry.Latest(t.Context())
			if test == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if storageRequests != 1 {
					t.Fatal("expected one storage hop")
				}
			} else if err == nil {
				t.Fatal("unsafe storage response accepted")
			}
			if test != "valid" && test != "second redirect" && test != "wrong content" && storageRequests != 0 {
				t.Fatal("unapproved storage origin was contacted")
			}
		})
	}
}

type registryResponse struct {
	body      []byte
	mediaType string
	digest    string
	status    int
	location  string
}

type registryFixture struct {
	responses map[string]registryResponse
	digest    string
	config    string
	child     string
}

type registryTransport func(*http.Request) (*http.Response, error)

func (f registryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func registryTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func registryTestDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newRegistryFixture(t *testing.T, indexed, docker bool, changeConfig func(map[string]any)) registryFixture {
	t.Helper()
	config := map[string]any{
		"architecture": "amd64", "os": "linux", "created": "2026-09-24T01:02:03Z",
		"config": map[string]any{"Labels": map[string]string{
			"org.opencontainers.image.version":  "0.2.7-pool.10",
			"org.opencontainers.image.revision": strings.Repeat("a", 40),
			"org.opencontainers.image.created":  "2026-09-24T09:02:03+08:00",
		}},
	}
	if changeConfig != nil {
		changeConfig(config)
	}
	configBody := registryTestJSON(t, config)
	configDigest := registryTestDigest(configBody)
	manifestType, indexType, configType := ociManifest, ociIndex, ociConfig
	if docker {
		manifestType, indexType, configType = dockerManifest, dockerIndex, dockerConfig
	}
	manifest := registryManifest{
		SchemaVersion: 2, MediaType: manifestType,
		Config: registryDescriptor{MediaType: configType, Digest: configDigest, Size: int64(len(configBody))},
	}
	manifestBody := registryTestJSON(t, manifest)
	manifestDigest := registryTestDigest(manifestBody)
	fixture := registryFixture{
		responses: map[string]registryResponse{
			"/token":                               {body: []byte(`{"token":"test-public-token"}`), mediaType: "application/json"},
			registryPath + "blobs/" + configDigest: {body: configBody, mediaType: configType, digest: configDigest},
		},
		digest: manifestDigest,
		config: registryPath + "blobs/" + configDigest,
		child:  registryPath + "manifests/" + manifestDigest,
	}
	fixture.responses[registryPath+"manifests/latest"] = registryResponse{body: manifestBody, mediaType: manifestType, digest: manifestDigest}
	if indexed {
		target := registryDescriptor{MediaType: manifestType, Digest: manifestDigest, Size: int64(len(manifestBody))}
		target.Platform.OS, target.Platform.Architecture = "linux", "amd64"
		other := target
		other.Platform.Architecture = "arm64"
		indexBody := registryTestJSON(t, registryManifest{SchemaVersion: 2, MediaType: indexType, Manifests: []registryDescriptor{other, target}})
		fixture.responses[fixture.child] = fixture.responses[registryPath+"manifests/latest"]
		fixture.responses[registryPath+"manifests/latest"] = registryResponse{body: indexBody, mediaType: indexType, digest: registryTestDigest(indexBody)}
	}
	return fixture
}

func (f registryFixture) registry(t *testing.T) (*Registry, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		if req.Method != http.MethodGet {
			t.Errorf("unexpected request method %s", req.Method)
		}
		if req.URL.Path == "/token" {
			if req.URL.Query().Get("service") != registryHost || req.URL.Query().Get("scope") != "repository:dongyaoa/sub2api-pool:pull" || req.Header.Get("Authorization") != "" {
				t.Error("unexpected anonymous token request")
			}
		} else if req.Header.Get("Authorization") != "Bearer test-public-token" {
			t.Error("image request omitted the expected token")
		}
		response, ok := f.responses[req.URL.Path]
		if !ok {
			t.Errorf("unexpected registry request %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", response.mediaType)
		if response.digest != "" {
			w.Header().Set("Docker-Content-Digest", response.digest)
		}
		if response.location != "" {
			w.Header().Set("Location", response.location)
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		_, _ = w.Write(response.body)
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != registryHost {
			return nil, fmt.Errorf("request escaped fixed registry origin")
		}
		clone := req.Clone(req.Context())
		clone.URL.Host, clone.URL.Scheme, clone.Host = endpoint.Host, endpoint.Scheme, endpoint.Host
		return server.Client().Transport.RoundTrip(clone)
	})
	return registry, requests
}

func TestRegistryLatest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		indexed bool
		docker  bool
	}{
		{name: "OCI index", indexed: true},
		{name: "OCI manifest"},
		{name: "Docker index", indexed: true, docker: true},
		{name: "Docker manifest", docker: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRegistryFixture(t, tc.indexed, tc.docker, nil)
			registry, requests := fixture.registry(t)
			release, err := registry.Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if release.Version != "0.2.7-pool.10" || release.Revision != strings.Repeat("a", 40) || release.Digest != fixture.digest || release.Image != Repository+"@"+fixture.digest {
				t.Fatalf("unexpected release: %+v", release)
			}
			if release.Created.Format(time.RFC3339) != "2026-09-24T01:02:03Z" {
				t.Fatalf("unexpected creation time: %s", release.Created)
			}
			wantRequests := int32(3)
			if tc.indexed {
				wantRequests++
			}
			if requests.Load() != wantRequests {
				t.Fatalf("request count = %d, want %d", requests.Load(), wantRequests)
			}
		})
	}
}

func TestRegistryLatestRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"wrong architecture", "architecture", "arm64"},
		{"wrong OS", "os", "windows"},
		{"upstream version", "version", "0.2.7"},
		{"version prefix", "version", "v0.2.7-pool.4"},
		{"leading zero version", "version", "0.2.7-pool.04"},
		{"invalid revision", "revision", "main"},
		{"short revision", "revision", "abcdef1234"},
		{"invalid created", "created", "yesterday"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRegistryFixture(t, true, false, func(config map[string]any) {
				if tc.key == "architecture" || tc.key == "os" {
					config[tc.key] = tc.value
					return
				}
				config["config"] = map[string]any{"Labels": map[string]string{
					"org.opencontainers.image.version": "0.2.7-pool.10", "org.opencontainers.image.revision": strings.Repeat("a", 40), "org.opencontainers.image.created": "2026-09-24T01:02:03Z",
					"org.opencontainers.image." + tc.key: tc.value,
				}}
			})
			registry, _ := fixture.registry(t)
			if _, err := registry.Latest(context.Background()); err == nil {
				t.Fatal("expected invalid metadata to be rejected")
			}
		})
	}
}

func TestRegistryLatestConfigCreationTimeFallback(t *testing.T) {
	fixture := newRegistryFixture(t, false, false, func(config map[string]any) {
		config["config"] = map[string]any{"Labels": map[string]string{
			"org.opencontainers.image.version": "0.2.7-pool.1", "org.opencontainers.image.revision": strings.Repeat("a", 40),
		}}
	})
	registry, _ := fixture.registry(t)
	release, err := registry.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Created.Format(time.RFC3339) != "2026-09-24T01:02:03Z" {
		t.Fatalf("unexpected creation time: %s", release.Created)
	}
}

func TestRegistryLatestRejectsInvalidResponses(t *testing.T) {
	for _, name := range []string{"missing digest", "wrong digest", "manifest invalid JSON", "schema", "media type", "no platform", "duplicate platform", "child digest mismatch", "config digest mismatch", "config size mismatch", "unsafe descriptor", "HTTP error", "redirect", "missing token", "oversized body"} {
		t.Run(name, func(t *testing.T) {
			fixture := newRegistryFixture(t, true, false, nil)
			path := registryPath + "manifests/latest"
			response := fixture.responses[path]
			switch name {
			case "missing digest":
				response.digest = ""
			case "wrong digest":
				response.digest = "sha256:" + strings.Repeat("0", 64)
			case "manifest invalid JSON":
				response.body = []byte(`{"schemaVersion":`)
				response.digest = registryTestDigest(response.body)
			case "schema", "media type", "no platform", "duplicate platform", "unsafe descriptor":
				var index registryManifest
				if err := json.Unmarshal(response.body, &index); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "schema":
					index.SchemaVersion = 1
				case "media type":
					index.MediaType = "application/json"
				case "no platform":
					index.Manifests = index.Manifests[:1]
				case "duplicate platform":
					index.Manifests = append(index.Manifests, index.Manifests[1])
				case "unsafe descriptor":
					index.Manifests[1].Digest = "../../outside"
				}
				response.body = registryTestJSON(t, index)
				response.digest = registryTestDigest(response.body)
			case "child digest mismatch":
				path = fixture.child
				response = fixture.responses[path]
				response.body = append(response.body, ' ')
				response.digest = registryTestDigest(response.body)
			case "config digest mismatch":
				path = fixture.config
				response = fixture.responses[path]
				response.body = append(response.body, ' ')
				response.digest = registryTestDigest(response.body)
			case "config size mismatch":
				fixture = newRegistryFixture(t, false, false, nil)
				response = fixture.responses[path]
				var manifest registryManifest
				if err := json.Unmarshal(response.body, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Config.Size++
				response.body = registryTestJSON(t, manifest)
				response.digest = registryTestDigest(response.body)
			case "HTTP error":
				response.status = http.StatusUnauthorized
			case "redirect":
				response.status = http.StatusTemporaryRedirect
				response.location = "https://ghcr.io/outside"
			case "missing token":
				path = "/token"
				response = registryResponse{body: []byte(`{}`), mediaType: "application/json"}
			case "oversized body":
				response.body = []byte(strings.Repeat(" ", maxRegistryBody+1))
			}
			fixture.responses[path] = response
			registry, requests := fixture.registry(t)
			if _, err := registry.Latest(context.Background()); err == nil {
				t.Fatal("expected invalid registry response to be rejected")
			}
			if name == "redirect" && requests.Load() != 2 {
				t.Fatal("redirect was followed")
			}
		})
	}
}

func TestRegistryRejectsUnapprovedOrigins(t *testing.T) {
	registry := NewRegistry()
	registry.client.Transport = registryTransport(func(_ *http.Request) (*http.Response, error) {
		t.Error("disallowed URL reached transport")
		return nil, fmt.Errorf("unexpected network request")
	})
	for _, endpoint := range []string{"http://ghcr.io/token", "https://ghcr.io.evil.test/token", "https://ghcr.io:443/token", "https://user@ghcr.io/token", "https://ghcr.io/v2/another/image/manifests/latest"} {
		if _, _, err := registry.get(context.Background(), endpoint, "", "application/json"); err == nil {
			t.Errorf("accepted unapproved URL %s", endpoint)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.2.7-pool.10", "0.2.7-pool.9", 1},
		{"0.2.7-pool.9", "0.2.7-pool.10", -1},
		{"0.2.8-pool.1", "0.2.7-pool.999", 1},
		{"1.0.0-pool.0", "0.99.99-pool.999", 1},
		{"0.2.7-pool.10", "0.2.7-pool.10", 0},
		{"0.2.7-pool.9999999999999999999999", "0.2.7-pool.999999999999999999999", 1},
	} {
		got, err := CompareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, %v, want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, version := range []string{"latest", "v0.2.7-pool.1", "0.2.7", "0.02.7-pool.1", "0.2.7-pool.01", "0.2.7-pool.1+local", "0.2.7-pool.1\n"} {
		if _, err := CompareVersions(version, "0.2.7-pool.1"); err == nil {
			t.Errorf("accepted invalid version %q", version)
		}
	}
}
