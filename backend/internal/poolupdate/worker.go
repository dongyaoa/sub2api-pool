package poolupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type ReleaseSource interface {
	Latest(context.Context) (*Release, error)
}

type journal struct {
	Job              *Job   `json:"job,omitempty"`
	RecoveryRequired bool   `json:"recovery_required,omitempty"`
	PreviousImage    string `json:"previous_image,omitempty"`
}

type Worker struct {
	cfg          Config
	registry     ReleaseSource
	runner       Runner
	mu           sync.Mutex
	state        journal
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	pollInterval time.Duration
}

func NewWorker(cfg Config, registry ReleaseSource, runner Runner) (*Worker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if registry == nil || runner == nil {
		return nil, errors.New("registry and runner are required")
	}
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{cfg: cfg, registry: registry, runner: runner, ctx: ctx, cancel: cancel, pollInterval: 2 * time.Second}
	data, err := os.ReadFile(w.statePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	if err == nil {
		if len(data) > 65536 || json.Unmarshal(data, &w.state) != nil {
			cancel()
			return nil, errors.New("invalid updater journal; operator recovery required")
		}
		if w.state.Job != nil && !terminal(w.state.Job.State) {
			now := time.Now().UTC()
			w.state.Job.State = "failed"
			w.state.Job.FinishedAt = &now
			w.state.Job.Message = "Updater was interrupted; operator recovery is required. No automatic recreation or database rollback was attempted."
			w.state.RecoveryRequired = true
			if err := w.saveLocked(); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	return w, nil
}

func (w *Worker) Close()              { w.cancel(); w.wg.Wait() }
func (w *Worker) statePath() string   { return filepath.Join(w.cfg.StateDir, "job.json") }
func (w *Worker) overlayPath() string { return filepath.Join(w.cfg.StateDir, "compose.override.json") }

func (w *Worker) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := Status{Available: !w.state.RecoveryRequired, RecoveryRequired: w.state.RecoveryRequired}
	if w.state.Job != nil {
		copy := *w.state.Job
		s.Job = &copy
	}
	return s
}

func (w *Worker) Start(ctx context.Context, request UpdateRequest) (*Job, error) {
	if err := ValidateVersion(request.Version); err != nil {
		return nil, err
	}
	if err := ValidateDigest(request.Digest); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.RecoveryRequired || w.ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	if w.state.Job != nil && !terminal(w.state.Job.State) {
		return nil, ErrBusy
	}
	// Keep the lock through target verification so concurrent POSTs cannot
	// both pass the latest check and enqueue destructive work.
	verifyCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	release, err := w.registry.Latest(verifyCtx)
	if err != nil {
		return nil, errors.New("could not verify latest pool release")
	}
	if release == nil || release.Version != request.Version || release.Digest != request.Digest || release.Image != Repository+"@"+request.Digest || !revisionPattern.MatchString(release.Revision) {
		return nil, ErrTargetChanged
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	w.state = journal{Job: &Job{ID: hex.EncodeToString(id[:]), State: "queued", Message: "Verified image update queued.", Version: release.Version, Revision: release.Revision, Digest: release.Digest, StartedAt: time.Now().UTC()}}
	if err := w.saveLocked(); err != nil {
		w.state.RecoveryRequired = true
		return nil, errors.New("cannot persist update job")
	}
	job := *w.state.Job
	w.wg.Add(1)
	go func() { defer w.wg.Done(); w.run(*release) }()
	return &job, nil
}

func (w *Worker) saveLocked() error {
	data, err := json.Marshal(w.state)
	if err != nil {
		return err
	}
	return atomicFile(w.statePath(), data, 0600)
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pool-update-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() { _ = os.Remove(temp) }()
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (w *Worker) transition(state, message string, finished bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.Job.State = state
	w.state.Job.Message = message
	if finished {
		now := time.Now().UTC()
		w.state.Job.FinishedAt = &now
	}
	if err := w.saveLocked(); err != nil {
		w.state.RecoveryRequired = true
		return err
	}
	return nil
}

func (w *Worker) fail(message string, recovery bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.RecoveryRequired = w.state.RecoveryRequired || recovery
	w.state.Job.State = "failed"
	w.state.Job.Message = message
	now := time.Now().UTC()
	w.state.Job.FinishedAt = &now
	if w.saveLocked() != nil {
		w.state.RecoveryRequired = true
	}
}

func (w *Worker) run(release Release) {
	jobCtx, jobCancel := context.WithTimeout(w.ctx, 18*time.Minute)
	defer jobCancel()
	ctx, cancel := context.WithTimeout(jobCtx, 14*time.Minute)
	defer cancel()
	old, err := w.current(ctx)
	if err != nil {
		w.fail("Current Compose service could not be verified; no container was changed.", false)
		return
	}
	if !old.State.Running || old.State.Health == nil || old.State.Health.Status != "healthy" {
		w.fail("Current service must have a passing health check before an update; no container was changed.", false)
		return
	}
	oldVersion, oldRevision, err := w.binaryVersion(ctx, old.ID)
	if err != nil {
		w.fail("Current application version could not be verified; no container was changed.", false)
		return
	}
	if err = w.verifyCurrentImage(ctx, old.Image, oldVersion, oldRevision); err != nil {
		w.fail("Current image provenance could not be verified; no container was changed.", false)
		return
	}
	comparison, err := CompareVersions(release.Version, oldVersion)
	if err != nil || comparison < 0 || (comparison == 0 && revisionMatches(oldRevision, release.Revision)) {
		w.fail("Target must be a newer pool release; no container was changed.", false)
		return
	}
	w.mu.Lock()
	w.state.PreviousImage = old.Image
	err = w.saveLocked()
	w.mu.Unlock()
	if err != nil {
		w.fail("Previous image could not be persisted; no container was changed.", true)
		return
	}
	if err = w.transition("pulling", "Pulling the verified immutable image.", false); err != nil {
		w.fail("Cannot persist pulling state; no container was changed.", true)
		return
	}
	pullCtx, pullCancel := context.WithTimeout(ctx, 10*time.Minute)
	_, err = w.docker(pullCtx, "pull", release.Image)
	pullCancel()
	if err != nil {
		w.fail("Image pull failed; the current container is unchanged.", false)
		return
	}
	if err = w.verifyImage(ctx, release); err != nil {
		w.fail("Pulled image identity or platform verification failed; the current container is unchanged.", false)
		return
	}
	// A deployment edited outside this updater must never be overwritten.
	before, err := w.current(ctx)
	if err != nil || before.ID != old.ID || before.Image != old.Image {
		w.fail("Compose service changed during the pull; no recreation was attempted.", false)
		return
	}
	if err = w.transition("recreating", "Recreating only the sub2api service; database volumes remain in place.", false); err != nil {
		w.fail("Cannot persist recreation state; no container was changed.", true)
		return
	}
	if err = w.writeOverlay(release.Image); err != nil {
		w.fail("Cannot write the managed image overlay; operator recovery is required.", true)
		return
	}
	_, err = w.compose(ctx, true, "up", "-d", "--no-deps", "--pull", "never", "sub2api")
	if err == nil {
		err = w.transition("checking", "Waiting for container health and application version verification.", false)
	}
	if err == nil {
		err = w.waitHealthy(ctx, release.Image, release.Version, release.Revision)
	}
	if err == nil {
		if err = w.transition("succeeded", "Image update completed. Application health, version and revision were verified.", true); err != nil {
			w.fail("Application updated, but final state could not be persisted; operator recovery is required.", true)
		}
		return
	}
	// Shutdown deliberately leaves a recovery gate instead of initiating more
	// destructive actions while systemd is stopping this process.
	if w.ctx.Err() != nil {
		w.fail("Update interrupted; inspect the container and managed overlay before clearing recovery. Database changes were not rolled back.", true)
		return
	}
	_ = w.transition("checking", "Update failed; restoring the preceding image once. Database changes are not rolled back.", false)
	rollbackCtx, rollbackCancel := context.WithTimeout(jobCtx, 4*time.Minute)
	defer rollbackCancel()
	if err = w.writeOverlay(old.Image); err == nil {
		_, err = w.compose(rollbackCtx, true, "up", "-d", "--no-deps", "--pull", "never", "sub2api")
	}
	if err == nil {
		err = w.waitHealthy(rollbackCtx, old.Image, oldVersion, oldRevision)
	}
	if err != nil {
		w.fail("Image update and image rollback failed; operator recovery is required. Database changes were not rolled back.", true)
		return
	}
	if err = w.transition("rolled_back", "Update failed; the preceding application image was restored and verified. Database changes were not rolled back.", true); err != nil {
		w.fail("Image rollback completed, but final state could not be persisted; operator recovery is required.", true)
	}
}

func (w *Worker) docker(ctx context.Context, args ...string) ([]byte, error) {
	return w.runner.Run(ctx, w.cfg.WorkingDir, w.cfg.DockerPath, args)
}
func (w *Worker) compose(ctx context.Context, overlay bool, args ...string) ([]byte, error) {
	if _, realHost := w.runner.(ExecRunner); realHost {
		if err := w.cfg.validateDeploymentPaths(); err != nil {
			return nil, errors.New("trusted deployment paths changed")
		}
	}
	command := w.cfg.ComposeCommand
	full := append([]string{}, command[1:]...)
	full = append(full, "--project-directory", w.cfg.WorkingDir, "--project-name", w.cfg.ProjectName)
	for _, p := range w.cfg.EnvFiles {
		full = append(full, "--env-file", p)
	}
	for _, p := range w.cfg.ComposeFiles {
		full = append(full, "-f", p)
	}
	if overlay {
		full = append(full, "-f", w.overlayPath())
	}
	full = append(full, args...)
	return w.runner.Run(ctx, w.cfg.WorkingDir, command[0], full)
}

type containerInfo struct {
	ID     string `json:"Id"`
	Image  string `json:"Image"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

var dockerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
var imageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (w *Worker) current(ctx context.Context) (*containerInfo, error) {
	_, err := os.Stat(w.overlayPath())
	overlay := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	data, err := w.compose(ctx, overlay, "ps", "-q", "sub2api")
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(data))
	if !dockerID.MatchString(id) {
		return nil, errors.New("expected exactly one Compose service container")
	}
	data, err = w.docker(ctx, "container", "inspect", id)
	if err != nil {
		return nil, err
	}
	var containers []containerInfo
	if json.Unmarshal(data, &containers) != nil || len(containers) != 1 {
		return nil, errors.New("invalid container inspection")
	}
	c := &containers[0]
	if !dockerID.MatchString(c.ID) || !strings.HasPrefix(c.ID, id) || !imageID.MatchString(c.Image) {
		return nil, errors.New("invalid container identity")
	}
	labels := c.Config.Labels
	if labels["com.docker.compose.project"] != w.cfg.ProjectName || labels["com.docker.compose.service"] != "sub2api" || labels["com.docker.compose.project.working_dir"] != w.cfg.WorkingDir || labels["com.docker.compose.oneoff"] == "True" {
		return nil, errors.New("container does not belong to the configured Compose service")
	}
	files := strings.Split(labels["com.docker.compose.project.config_files"], ",")
	base := make([]string, 0, len(files))
	for _, p := range files {
		if p != w.overlayPath() {
			base = append(base, p)
		}
	}
	if len(base) != len(w.cfg.ComposeFiles) {
		return nil, errors.New("original Compose file list changed")
	}
	for i, p := range base {
		if p != w.cfg.ComposeFiles[i] {
			return nil, errors.New("original Compose file order changed")
		}
	}
	return c, nil
}

func (w *Worker) verifyImage(ctx context.Context, release Release) error {
	data, err := w.docker(ctx, "image", "inspect", release.Image)
	if err != nil {
		return err
	}
	var values []struct {
		OS           string   `json:"Os"`
		Architecture string   `json:"Architecture"`
		RepoDigests  []string `json:"RepoDigests"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(data, &values) != nil || len(values) != 1 {
		return errors.New("invalid image inspection")
	}
	value := values[0]
	found := false
	for _, digest := range value.RepoDigests {
		if digest == release.Image {
			found = true
		}
	}
	if !found || value.OS != "linux" || value.Architecture != "amd64" || value.Config.Labels["org.opencontainers.image.version"] != release.Version || value.Config.Labels["org.opencontainers.image.revision"] != release.Revision {
		return errors.New("pulled image does not match the verified release")
	}
	return nil
}

func (w *Worker) verifyCurrentImage(ctx context.Context, image, version, revision string) error {
	data, err := w.docker(ctx, "image", "inspect", image)
	if err != nil {
		return err
	}
	var values []struct {
		OS           string `json:"Os"`
		Architecture string `json:"Architecture"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(data, &values) != nil || len(values) != 1 {
		return errors.New("invalid current image inspection")
	}
	v := values[0]
	labels := v.Config.Labels
	if v.OS != "linux" || v.Architecture != "amd64" || labels["org.opencontainers.image.source"] != "https://github.com/dongyaoa/sub2api-pool" || labels["org.opencontainers.image.version"] != version || !revisionPattern.MatchString(labels["org.opencontainers.image.revision"]) || !revisionMatches(revision, labels["org.opencontainers.image.revision"]) {
		return errors.New("current image is not a verified pool release")
	}
	return nil
}

var binaryVersionPattern = regexp.MustCompile(`Sub2API ([0-9]+\.[0-9]+\.[0-9]+-pool\.[0-9]+) \(commit: ([a-f0-9]{7,64}),`)

func (w *Worker) binaryVersion(ctx context.Context, id string) (string, string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := w.docker(probeCtx, "exec", id, "/app/sub2api", "-version")
	if err != nil {
		return "", "", err
	}
	match := binaryVersionPattern.FindSubmatch(data)
	if len(match) != 3 {
		return "", "", errors.New("running binary did not report a pool version and revision")
	}
	return string(match[1]), string(match[2]), nil
}
func revisionMatches(actual, want string) bool {
	return len(actual) >= 7 && len(want) >= 7 && (actual == want || strings.HasPrefix(want, actual) || strings.HasPrefix(actual, want))
}
func (w *Worker) waitHealthy(ctx context.Context, expectedImage, version, revision string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(w.cfg.HealthTimeoutSeconds)*time.Second)
	defer cancel()
	for {
		c, err := w.current(ctx)
		if err == nil && c.State.Running && c.State.Health != nil && c.State.Health.Status == "healthy" && (c.Config.Image == expectedImage || c.Image == expectedImage) {
			actual, commit, err := w.binaryVersion(ctx, c.ID)
			if err == nil && actual == version && revisionMatches(commit, revision) {
				return nil
			}
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("application health or version verification timed out")
		case <-timer.C:
		}
	}
}

func (w *Worker) writeOverlay(image string) error {
	if strings.HasPrefix(image, Repository+"@") {
		if err := ValidateDigest(strings.TrimPrefix(image, Repository+"@")); err != nil {
			return err
		}
	} else if !imageID.MatchString(image) {
		return errors.New("invalid rollback image ID")
	}
	service := map[string]any{"image": image, "environment": map[string]string{"POOL_UPDATER_SOCKET": ContainerSocketDirectory + "/" + filepath.Base(w.cfg.SocketPath)}, "volumes": []any{map[string]any{"type": "bind", "source": filepath.Dir(w.cfg.SocketPath), "target": ContainerSocketDirectory, "read_only": true}}}
	data, err := json.Marshal(map[string]any{"services": map[string]any{"sub2api": service}})
	if err != nil {
		return err
	}
	return atomicFile(w.overlayPath(), data, 0600)
}

// Bootstrap installs socket access on the already-running immutable image.
// It neither pulls nor selects a different release.
func (w *Worker) Bootstrap(ctx context.Context) error {
	jobCtx, jobCancel := context.WithTimeout(ctx, 18*time.Minute)
	defer jobCancel()
	ctx, cancel := context.WithTimeout(jobCtx, 14*time.Minute)
	defer cancel()
	w.mu.Lock()
	if w.state.RecoveryRequired {
		w.mu.Unlock()
		return ErrUnavailable
	}
	if w.state.Job != nil && !terminal(w.state.Job.State) {
		w.mu.Unlock()
		return ErrBusy
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		w.mu.Unlock()
		return err
	}
	w.state = journal{Job: &Job{ID: hex.EncodeToString(id[:]), State: "queued", Message: "Installing updater socket access on the current image.", StartedAt: time.Now().UTC()}}
	err := w.saveLocked()
	w.mu.Unlock()
	if err != nil {
		return err
	}
	old, err := w.current(ctx)
	if err != nil {
		w.fail("Bootstrap could not verify the current Compose container.", false)
		return err
	}
	if !old.State.Running || old.State.Health == nil || old.State.Health.Status != "healthy" {
		w.fail("Bootstrap requires a healthy existing service with a configured health check.", false)
		return errors.New("current service is not healthy")
	}
	version, revision, err := w.binaryVersion(ctx, old.ID)
	if err != nil {
		w.fail("Bootstrap could not verify the running pool binary.", false)
		return err
	}
	if err = w.verifyCurrentImage(ctx, old.Image, version, revision); err != nil {
		w.fail("Bootstrap requires an existing pool release image with matching provenance and binary version.", false)
		return err
	}
	w.mu.Lock()
	w.state.Job.Version = version
	w.state.Job.Revision = revision
	w.state.PreviousImage = old.Image
	err = w.saveLocked()
	w.mu.Unlock()
	if err != nil {
		w.fail("Cannot persist bootstrap image identity.", true)
		return err
	}
	previous, readErr := os.ReadFile(w.overlayPath())
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		w.fail("Cannot read the existing overlay.", false)
		return readErr
	}
	if err = w.transition("recreating", "Installing updater socket access without changing the image.", false); err != nil {
		w.fail("Cannot persist bootstrap state.", true)
		return err
	}
	if err = w.writeOverlay(old.Image); err == nil {
		_, err = w.compose(ctx, true, "up", "-d", "--no-deps", "--pull", "never", "sub2api")
	}
	if err == nil {
		err = w.transition("checking", "Verifying health and unchanged application version.", false)
	}
	if err == nil {
		err = w.waitHealthy(ctx, old.Image, version, revision)
	}
	if err == nil {
		return w.transition("succeeded", "Updater socket installed. The existing image and application health were verified.", true)
	}
	if jobCtx.Err() != nil {
		w.fail("Bootstrap interrupted; operator recovery is required. Database changes were not rolled back.", true)
		return err
	}
	// Restore the old overlay settings, but always pin its image ID. Reusing
	// the base floating tag could accidentally start a newly pulled image.
	rollbackOverlay := map[string]any{"services": map[string]any{"sub2api": map[string]any{"image": old.Image}}}
	rollbackCtx, rollbackCancel := context.WithTimeout(jobCtx, 4*time.Minute)
	defer rollbackCancel()
	var restoreErr error
	if existed {
		var decoded struct {
			Services map[string]map[string]any `json:"services"`
		}
		if restoreErr = json.Unmarshal(previous, &decoded); restoreErr == nil {
			if decoded.Services["sub2api"] == nil {
				restoreErr = errors.New("invalid previous overlay")
			} else {
				decoded.Services["sub2api"]["image"] = old.Image
				rollbackOverlay = map[string]any{"services": decoded.Services}
			}
		}
	}
	if restoreErr == nil {
		var data []byte
		data, restoreErr = json.Marshal(rollbackOverlay)
		if restoreErr == nil {
			restoreErr = atomicFile(w.overlayPath(), data, 0600)
		}
	}
	if restoreErr == nil {
		_, restoreErr = w.compose(rollbackCtx, true, "up", "-d", "--no-deps", "--pull", "never", "sub2api")
	}
	if restoreErr == nil {
		restoreErr = w.waitHealthy(rollbackCtx, old.Image, version, revision)
	}
	if restoreErr != nil {
		w.fail("Bootstrap and restoration failed; operator recovery is required. Database changes were not rolled back.", true)
		return errors.New("bootstrap restoration failed")
	}
	if finishErr := w.transition("rolled_back", "Bootstrap failed; the original Compose configuration and image were restored. Database changes were not rolled back.", true); finishErr != nil {
		return finishErr
	}
	return errors.New("bootstrap failed and was rolled back")
}

// ClearRecovery is a host-only operation. It verifies the surviving container
// before acknowledging interruption; it does not recreate or roll back it.
func (w *Worker) ClearRecovery(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.Job != nil && !terminal(w.state.Job.State) {
		return ErrBusy
	}
	c, err := w.current(ctx)
	if err != nil {
		return err
	}
	if !c.State.Running || c.State.Health == nil || c.State.Health.Status != "healthy" {
		return errors.New("current service is not healthy")
	}
	version, revision, err := w.binaryVersion(ctx, c.ID)
	if err != nil {
		return err
	}
	if err = w.verifyCurrentImage(ctx, c.Image, version, revision); err != nil {
		return err
	}
	w.state.RecoveryRequired = false
	return w.saveLocked()
}

func (w *Worker) String() string {
	return fmt.Sprintf("pool updater for Compose project %s", w.cfg.ProjectName)
}
