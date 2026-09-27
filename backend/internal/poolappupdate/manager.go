package poolappupdate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

var (
	ErrBusy          = poolupdate.ErrBusy
	ErrUnavailable   = poolupdate.ErrUnavailable
	ErrTargetChanged = poolupdate.ErrTargetChanged
	ErrNoUpdate      = errors.New("no newer Pool application release is available")
)

type Manager struct {
	root, currentVersion, currentRevision string
	source                                Source
	restart                               func()
	verify                                func(context.Context, string, *Release) error
	replace                               func(string, string) error
	mu                                    sync.Mutex
	job                                   *poolupdate.Job
	checking, running, closed             bool
	recoveryRequired                      bool
	ctx                                   context.Context
	cancel                                context.CancelFunc
	wg                                    sync.WaitGroup
	restartDelay                          time.Duration
}

func NewManager(root, currentVersion, currentRevision string, source Source, restart func()) (*Manager, error) {
	if !filepath.IsAbs(root) || source == nil || restart == nil {
		return nil, fmt.Errorf("application updater requires an absolute runtime directory, source and restart callback")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid update runtime directory")
	}
	probe, err := os.CreateTemp(root, ".update-write-probe-")
	if err != nil {
		return nil, fmt.Errorf("update runtime directory is not writable: %w", err)
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probe.Name())
		return nil, err
	}
	if err := os.Remove(probe.Name()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{root: root, currentVersion: currentVersion, currentRevision: currentRevision, source: source, restart: restart, verify: verifyExecutable, replace: os.Rename, ctx: ctx, cancel: cancel, restartDelay: 2 * time.Second}
	data, err := readJournal(m.path("update-job.json"))
	if err == nil {
		var job poolupdate.Job
		if len(data) > 65536 || json.Unmarshal(data, &job) != nil || job.ID == "" || !knownState(job.State) {
			cancel()
			return nil, fmt.Errorf("%w: invalid persisted application update task", ErrUnavailable)
		}
		m.job = &job
	} else if !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, fmt.Errorf("%w: cannot read persisted application update task", ErrUnavailable)
	}
	m.recoveryRequired = exists(m.path("update-recovery-required"))
	if exists(m.path("update-pending")) && (m.job == nil || terminal(m.job.State)) {
		m.recoveryRequired = true
	}
	if err := validProgramFile(m.path("sub2api")); err != nil {
		cancel()
		return nil, err
	}
	return m, nil
}

func readJournal(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 65536 {
		return nil, ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, 65537))
}

func validProgramFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBinarySize {
		return fmt.Errorf("invalid current application file")
	}
	return nil
}

func (m *Manager) path(name string) string { return filepath.Join(m.root, name) }

func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "rolled_back"
}
func knownState(state string) bool {
	return terminal(state) || state == "queued" || state == "downloading" || state == "verifying" || state == "installing" || state == "restarting"
}

func (m *Manager) Status() poolupdate.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconcileLocked()
	status := poolupdate.Status{Available: !m.closed && !m.recoveryRequired, RecoveryRequired: m.recoveryRequired}
	if m.job != nil {
		copy := *m.job
		status.Job = &copy
	}
	return status
}

func (m *Manager) reconcileLocked() {
	if m.running || m.recoveryRequired || m.job == nil || terminal(m.job.State) {
		return
	}
	state, message := "", ""
	if exists(m.path("update-rolled-back")) {
		state, message = "rolled_back", "The new application did not become healthy; the previous program was restored."
	} else if !exists(m.path("update-pending")) {
		if m.currentVersion == m.job.Version && m.currentRevision == m.job.Revision {
			state, message = "succeeded", "Application update completed and passed its startup health check."
		} else {
			state, message = "failed", "The application update was interrupted before completion or the base image changed."
		}
	}
	if state != "" {
		old := *m.job
		now := time.Now().UTC()
		m.job.State, m.job.Message, m.job.FinishedAt = state, message, &now
		if m.saveLocked() != nil {
			m.job = &old
		}
	}
}

func exists(path string) bool { _, err := os.Stat(path); return !errors.Is(err, os.ErrNotExist) }

func (m *Manager) Start(ctx context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	if poolupdate.ValidateVersion(request.Version) != nil || poolupdate.ValidateDigest(request.Digest) != nil {
		return nil, ErrTargetChanged
	}
	m.mu.Lock()
	m.reconcileLocked()
	if m.closed || m.recoveryRequired {
		m.mu.Unlock()
		return nil, ErrUnavailable
	}
	if m.checking || m.running || (m.job != nil && !terminal(m.job.State)) || exists(m.path("update-pending")) {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.checking = true
	m.mu.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	release, err := m.source.Latest(checkCtx)
	cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checking = false
	if m.closed || m.recoveryRequired {
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("check application update: %w", err)
	}
	if err := validateRelease(release); err != nil {
		return nil, err
	}
	if release.Version != request.Version || release.Digest != request.Digest {
		return nil, ErrTargetChanged
	}
	order, err := poolupdate.CompareVersions(release.Version, m.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("current build is not a supported Pool version")
	}
	if order <= 0 {
		return nil, ErrNoUpdate
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validProgramFile(m.path("sub2api")); err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	previous := m.job
	m.job = &poolupdate.Job{ID: hex.EncodeToString(id), State: "queued", Message: "Application update accepted.", Version: release.Version, Revision: release.Revision, Digest: release.Digest, StartedAt: time.Now().UTC()}
	if err := m.saveLocked(); err != nil {
		m.job = previous
		return nil, err
	}
	if err := os.Remove(m.path("update-rolled-back")); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.job = previous
		_ = m.saveLocked()
		return nil, err
	}
	copy := *m.job
	m.running = true
	m.wg.Add(1)
	go m.run(release)
	return &copy, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) saveLocked() error {
	data, err := json.Marshal(m.job)
	if err != nil {
		return err
	}
	if err := atomicWrite(m.path("update-job.json"), data, 0600); err != nil {
		m.recoveryRequired = true
		_ = atomicWrite(m.path("update-recovery-required"), []byte("Update task could not be saved. Inspect runtime state before retrying.\n"), 0600)
		return fmt.Errorf("%w: persist application update task: %v", ErrUnavailable, err)
	}
	return nil
}

func (m *Manager) transition(state, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := *m.job
	m.job.State, m.job.Message = state, message
	if terminal(state) {
		now := time.Now().UTC()
		m.job.FinishedAt = &now
	}
	if err := m.saveLocked(); err != nil {
		m.job = &old
		return err
	}
	return nil
}

func (m *Manager) run(release *Release) {
	defer m.wg.Done()
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	defer cancel()
	fail := func(err error) { _ = m.transition("failed", err.Error()) }
	dir, err := os.MkdirTemp(m.root, ".pool-update-")
	if err != nil {
		fail(fmt.Errorf("prepare update: %w", err))
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	binary := filepath.Join(dir, "sub2api")
	if err := m.transition("downloading", "Downloading the verified Pool application release."); err != nil {
		fail(err)
		return
	}
	if err := m.source.Download(ctx, release, binary); err != nil {
		fail(err)
		return
	}
	if err := m.transition("verifying", "Verifying application checksum, platform and build identity."); err != nil {
		fail(err)
		return
	}
	if err := verifyFile(binary, release); err != nil {
		fail(err)
		return
	}
	if err := os.Chmod(binary, 0755); err != nil {
		fail(err)
		return
	}
	if err := m.verify(ctx, binary, release); err != nil {
		fail(err)
		return
	}
	if err := ctx.Err(); err != nil {
		fail(err)
		return
	}
	if err := m.transition("installing", "Saving the previous program and installing the verified update."); err != nil {
		fail(err)
		return
	}
	current, backup := m.path("sub2api"), m.path("sub2api.backup")
	if err := copyAtomic(current, backup); err != nil {
		fail(fmt.Errorf("backup current application: %w", err))
		return
	}
	// The launcher can distinguish a pre-swap interruption from a replaced
	// application by comparing this hash with its active executable.
	if err := atomicWrite(m.path("update-pending"), []byte(strings.TrimPrefix(release.Digest, "sha256:")+"\n"), 0600); err != nil {
		fail(err)
		return
	}
	if err := m.replace(binary, current); err != nil {
		_ = os.Remove(m.path("update-pending"))
		fail(fmt.Errorf("install application: %w", err))
		return
	}
	syncDir(m.root)
	if err := m.transition("restarting", "Restarting the application and waiting for its health check."); err != nil {
		if restoreErr := copyAtomic(backup, current); restoreErr == nil {
			_ = os.Remove(m.path("update-pending"))
		} else {
			m.mu.Lock()
			m.recoveryRequired = true
			_ = atomicWrite(m.path("update-recovery-required"), []byte("Restoring the application backup failed.\n"), 0600)
			m.mu.Unlock()
		}
		fail(fmt.Errorf("persist installed update: %w", err))
		return
	}
	// An accepted request owns its task: caller disconnection cannot cancel it.
	// Once the binary is installed, restart even if application shutdown races us.
	timer := time.NewTimer(m.restartDelay)
	defer timer.Stop()
	<-timer.C
	m.restart()
}

func verifyFile(path string, release *Release) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != release.Size {
		return fmt.Errorf("downloaded application size is invalid")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxBinarySize+1)); err != nil {
		return err
	}
	if "sha256:"+hex.EncodeToString(hash.Sum(nil)) != release.Digest {
		return fmt.Errorf("downloaded application checksum does not match")
	}
	return nil
}

func verifyExecutable(ctx context.Context, path string, release *Release) error {
	binary, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("update is not a Linux executable")
	}
	valid := binary.Class == elf.ELFCLASS64 && binary.Machine == elf.EM_X86_64 && (binary.Type == elf.ET_EXEC || binary.Type == elf.ET_DYN)
	_ = binary.Close()
	if !valid {
		return fmt.Errorf("update is not a Linux amd64 executable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--version")
	output := &boundedOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return fmt.Errorf("verify downloaded application build: %w", err)
	}
	expected := "Sub2API " + release.Version + " (commit: " + release.Revision + ","
	if !strings.Contains(string(output.data), expected) {
		return fmt.Errorf("downloaded application version or revision differs from its release manifest")
	}
	return nil
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(data []byte) (int, error) {
	count := len(data)
	if len(b.data)+count > 16384 {
		return 0, fmt.Errorf("application version output exceeds limit")
	}
	b.data = append(b.data, data...)
	return count, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".update-state-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

func copyAtomic(from, to string) error {
	if err := validProgramFile(from); err != nil {
		return err
	}
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBinarySize {
		return fmt.Errorf("invalid current application file")
	}
	output, err := os.CreateTemp(filepath.Dir(to), ".update-backup-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(output.Name()) }()
	if _, err := io.Copy(output, io.LimitReader(input, maxBinarySize+1)); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Chmod(0755); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Rename(output.Name(), to); err != nil {
		return err
	}
	syncDir(filepath.Dir(to))
	return nil
}

func syncDir(path string) {
	if directory, err := os.Open(path); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
}
