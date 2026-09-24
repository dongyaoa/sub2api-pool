package poolupdate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixtureRegistry struct{ release Release }

func (r fixtureRegistry) Latest(context.Context) (*Release, error) {
	copy := r.release
	return &copy, nil
}

type fakeDocker struct {
	mu              sync.Mutex
	cfg             Config
	release         Release
	currentImage    string
	currentVersion  string
	currentRevision string
	currentID       string
	upCount         int
	failFirstUp     bool
	failRollback    bool
	badImage        bool
	badLabels       bool
	missingHealth   bool
	pullGate        <-chan struct{}
	calls           [][]string
}

func (f *fakeDocker) Run(ctx context.Context, _ string, _ string, args []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
	encoded := func(v any) ([]byte, error) { return json.Marshal(v) }
	if len(args) >= 2 && args[0] == "pull" {
		if f.pullGate != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.pullGate:
			}
		}
		return nil, nil
	}
	if len(args) >= 2 && args[0] == "image" && args[1] == "inspect" {
		arch := "amd64"
		if f.badImage {
			arch = "arm64"
		}
		version, revision := f.release.Version, f.release.Revision
		if args[2] == "sha256:"+strings.Repeat("a", 64) {
			version = f.currentVersion
			revision = f.currentRevision
		}
		return encoded([]any{map[string]any{"Os": "linux", "Architecture": arch, "RepoDigests": []string{f.release.Image}, "Config": map[string]any{"Labels": map[string]string{"org.opencontainers.image.version": version, "org.opencontainers.image.revision": revision, "org.opencontainers.image.source": "https://github.com/dongyaoa/sub2api-pool"}}}})
	}
	if len(args) >= 2 && args[0] == "container" && args[1] == "inspect" {
		labels := map[string]string{"com.docker.compose.project": f.cfg.ProjectName, "com.docker.compose.service": "sub2api", "com.docker.compose.project.working_dir": f.cfg.WorkingDir, "com.docker.compose.project.config_files": strings.Join(f.cfg.ComposeFiles, ",")}
		if f.badLabels {
			labels["com.docker.compose.project"] = "another-stack"
		}
		image := "sha256:" + strings.Repeat("a", 64)
		if f.currentImage == f.release.Image {
			image = "sha256:" + strings.Repeat("b", 64)
		}
		state := map[string]any{"Running": true}
		if !f.missingHealth {
			state["Health"] = map[string]string{"Status": "healthy"}
		}
		return encoded([]any{map[string]any{"Id": f.currentID, "Image": image, "Config": map[string]any{"Image": f.currentImage, "Labels": labels}, "State": state}})
	}
	if len(args) >= 1 && args[0] == "exec" {
		return []byte("Sub2API " + f.currentVersion + " (commit: " + f.currentRevision + ", built: fixture)"), nil
	}
	for i, arg := range args {
		if arg == "ps" {
			return []byte(f.currentID + "\n"), nil
		}
		if arg == "up" {
			if strings.Join(args[i:], " ") != "up -d --no-deps --pull never sub2api" {
				return nil, errors.New("unsafe compose command")
			}
			f.upCount++
			if (f.upCount == 1 && f.failFirstUp) || (f.upCount == 2 && f.failRollback) {
				return nil, errors.New("simulated recreate failure")
			}
			data, err := os.ReadFile(filepath.Join(f.cfg.StateDir, "compose.override.json"))
			if err != nil {
				return nil, err
			}
			var overlay struct {
				Services map[string]struct {
					Image string `json:"image"`
				} `json:"services"`
			}
			if err = json.Unmarshal(data, &overlay); err != nil {
				return nil, err
			}
			f.currentImage = overlay.Services["sub2api"].Image
			if f.currentImage == f.release.Image {
				f.currentVersion = f.release.Version
				f.currentRevision = f.release.Revision
				f.currentID = strings.Repeat("2", 64)
			} else {
				f.currentVersion = "0.2.7-pool.4"
				f.currentRevision = strings.Repeat("a", 40)
				f.currentID = strings.Repeat("3", 64)
			}
			return nil, nil
		}
	}
	return nil, errors.New("unexpected docker command")
}

func workerFixture(t *testing.T) (*Worker, *fakeDocker, Release) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{ComposeFiles: []string{filepath.Join(dir, "compose.yml"), filepath.Join(dir, "custom.yml")}, ProjectName: "pool-production", WorkingDir: dir, StateDir: filepath.Join(dir, "state"), SocketPath: filepath.Join(dir, "run", "updater.sock"), SocketGID: 1000, DockerPath: filepath.Join(dir, "docker"), ComposeCommand: []string{filepath.Join(dir, "docker"), "compose"}, HealthTimeoutSeconds: 10, EnvFiles: []string{filepath.Join(dir, "pool.env")}}
	release := Release{Version: "0.2.7-pool.5", Revision: strings.Repeat("b", 40), Digest: "sha256:" + strings.Repeat("c", 64)}
	release.Image = Repository + "@" + release.Digest
	fake := &fakeDocker{cfg: cfg, release: release, currentImage: Repository + ":latest", currentVersion: "0.2.7-pool.4", currentRevision: strings.Repeat("a", 40), currentID: strings.Repeat("1", 64)}
	worker, err := NewWorker(cfg, fixtureRegistry{release}, fake)
	require.NoError(t, err)
	worker.pollInterval = time.Millisecond
	t.Cleanup(worker.Close)
	return worker, fake, release
}

func awaitJob(t *testing.T, worker *Worker) *Job {
	t.Helper()
	require.Eventually(t, func() bool { s := worker.Status(); return s.Job != nil && terminal(s.Job.State) }, 3*time.Second, time.Millisecond)
	return worker.Status().Job
}

func TestWorkerUpdatesOnlyVerifiedComposeService(t *testing.T) {
	w, f, r := workerFixture(t)
	job, err := w.Start(t.Context(), UpdateRequest{Version: r.Version, Digest: r.Digest})
	require.NoError(t, err)
	require.Equal(t, "queued", job.State)
	finished := awaitJob(t, w)
	require.Equal(t, "succeeded", finished.State)
	require.NotNil(t, finished.FinishedAt)
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, 1, f.upCount)
	for _, args := range f.calls {
		require.NotContains(t, args, "down")
		require.NotContains(t, args, "rm")
		require.NotContains(t, args, "--volumes")
	}
	data, err := os.ReadFile(w.overlayPath())
	require.NoError(t, err)
	require.Contains(t, string(data), r.Image)
	require.Contains(t, string(data), "POOL_UPDATER_SOCKET")
	require.Contains(t, string(data), `"read_only":true`)
	data, err = os.ReadFile(w.statePath())
	require.NoError(t, err)
	require.Contains(t, string(data), `"state":"succeeded"`)
	require.Contains(t, string(data), `"previous_image":"sha256:`)
}

func TestWorkerRollsBackOnlyOnceAndReportsDatabaseBoundary(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "failed"}[rollbackFails], func(t *testing.T) {
			w, f, r := workerFixture(t)
			f.failFirstUp = true
			f.failRollback = rollbackFails
			_, err := w.Start(t.Context(), UpdateRequest{r.Version, r.Digest})
			require.NoError(t, err)
			job := awaitJob(t, w)
			want := "rolled_back"
			if rollbackFails {
				want = "failed"
			}
			require.Equal(t, want, job.State)
			require.Contains(t, job.Message, "Database changes were not rolled back")
			f.mu.Lock()
			require.Equal(t, 2, f.upCount)
			f.mu.Unlock()
			require.Equal(t, rollbackFails, w.Status().RecoveryRequired)
		})
	}
}

func TestWorkerRefusesChangedTargetConcurrentJobAndUnverifiedDeployment(t *testing.T) {
	t.Run("changed latest", func(t *testing.T) {
		w, _, r := workerFixture(t)
		_, err := w.Start(t.Context(), UpdateRequest{r.Version, "sha256:" + strings.Repeat("d", 64)})
		require.ErrorIs(t, err, ErrTargetChanged)
	})
	t.Run("busy", func(t *testing.T) {
		w, f, r := workerFixture(t)
		gate := make(chan struct{})
		f.pullGate = gate
		_, err := w.Start(t.Context(), UpdateRequest{r.Version, r.Digest})
		require.NoError(t, err)
		_, err = w.Start(t.Context(), UpdateRequest{r.Version, r.Digest})
		require.ErrorIs(t, err, ErrBusy)
		close(gate)
		require.Equal(t, "succeeded", awaitJob(t, w).State)
	})
	for _, field := range []string{"labels", "image"} {
		t.Run(field, func(t *testing.T) {
			w, f, r := workerFixture(t)
			f.badLabels = field == "labels"
			f.badImage = field == "image"
			_, err := w.Start(t.Context(), UpdateRequest{r.Version, r.Digest})
			require.NoError(t, err)
			require.Equal(t, "failed", awaitJob(t, w).State)
			f.mu.Lock()
			require.Zero(t, f.upCount)
			f.mu.Unlock()
		})
	}
}

func TestWorkerSameVersionRebuiltRevision(t *testing.T) {
	w, f, r := workerFixture(t)
	f.currentVersion = r.Version
	_, err := w.Start(t.Context(), UpdateRequest{r.Version, r.Digest})
	require.NoError(t, err)
	require.Equal(t, "succeeded", awaitJob(t, w).State)
}

func TestWorkerRestartRequiresRecoveryWithoutDockerMutation(t *testing.T) {
	w, f, _ := workerFixture(t)
	data, err := json.Marshal(journal{Job: &Job{ID: "interrupted", State: "recreating"}, PreviousImage: "sha256:" + strings.Repeat("a", 64)})
	require.NoError(t, err)
	require.NoError(t, atomicFile(w.statePath(), data, 0600))
	restarted, err := NewWorker(w.cfg, w.registry, f)
	require.NoError(t, err)
	defer restarted.Close()
	status := restarted.Status()
	require.False(t, status.Available)
	require.True(t, status.RecoveryRequired)
	require.Equal(t, "failed", status.Job.State)
	f.mu.Lock()
	require.Empty(t, f.calls)
	f.mu.Unlock()
}

func TestUpdaterHTTPRejectsCommandsUnknownFieldsAndOversizeBody(t *testing.T) {
	w, _, r := workerFixture(t)
	handler := Handler(w)
	for _, body := range []string{`{"version":"` + r.Version + `","digest":"` + r.Digest + `","command":"docker rm"}`, `{} {}`, strings.Repeat("a", 5000)} {
		req := httptest.NewRequest(http.MethodPost, "/v1/update", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"available":true`)
}

func TestBootstrapKeepsImageAndRequiresExistingHealthCheck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		w, f, _ := workerFixture(t)
		require.NoError(t, w.Bootstrap(t.Context()))
		require.Equal(t, "succeeded", w.Status().Job.State)
		f.mu.Lock()
		require.Equal(t, 1, f.upCount)
		require.Equal(t, "sha256:"+strings.Repeat("a", 64), f.currentImage)
		f.mu.Unlock()
	})
	t.Run("missing health", func(t *testing.T) {
		w, f, _ := workerFixture(t)
		f.missingHealth = true
		require.Error(t, w.Bootstrap(t.Context()))
		require.Equal(t, "failed", w.Status().Job.State)
		f.mu.Lock()
		require.Zero(t, f.upCount)
		f.mu.Unlock()
	})
	t.Run("rollback pins original image despite floating tag", func(t *testing.T) {
		w, f, _ := workerFixture(t)
		f.failFirstUp = true
		require.Error(t, w.Bootstrap(t.Context()))
		require.Equal(t, "rolled_back", w.Status().Job.State)
		data, err := os.ReadFile(w.overlayPath())
		require.NoError(t, err)
		require.Contains(t, string(data), "sha256:"+strings.Repeat("a", 64))
		require.NotContains(t, string(data), ":latest")
		f.mu.Lock()
		require.Equal(t, 2, f.upCount)
		require.Equal(t, "sha256:"+strings.Repeat("a", 64), f.currentImage)
		f.mu.Unlock()
	})
}
