package poolappupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

type fakeSource struct {
	release       *Release
	binary        []byte
	downloadError error
	gate          chan struct{}
	entered       chan struct{}
}

func (s *fakeSource) Latest(context.Context) (*Release, error) { copy := *s.release; return &copy, nil }
func (s *fakeSource) Download(ctx context.Context, _ *Release, path string) error {
	if s.entered != nil {
		close(s.entered)
	}
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.downloadError != nil {
		return s.downloadError
	}
	return os.WriteFile(path, s.binary, 0600)
}

func newTestManager(t *testing.T, source *fakeSource) (*Manager, chan struct{}) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sub2api"), []byte("previous application"), 0755); err != nil {
		t.Fatal(err)
	}
	restarted := make(chan struct{}, 1)
	m, err := NewManager(root, "0.2.7-pool.11", strings.Repeat("b", 40), source, func() { restarted <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	m.verify = func(context.Context, string, *Release) error { return nil }
	m.restartDelay = 0
	t.Cleanup(m.Close)
	return m, restarted
}

func waitTask(t *testing.T, m *Manager, want string) *poolupdate.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := m.Status()
		if status.Job != nil && status.Job.State == want {
			return status.Job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wanted state %s; got %+v", want, m.Status().Job)
	return nil
}

func requestFor(release *Release) poolupdate.UpdateRequest {
	return poolupdate.UpdateRequest{Version: release.Version, Digest: release.Digest}
}

func TestManagerInstallsAndConfirmsOnlyAfterLauncherHealth(t *testing.T) {
	binary := []byte("new verified application")
	source := &fakeSource{release: testRelease(binary), binary: binary}
	m, restarted := newTestManager(t, source)
	job, err := m.Start(context.Background(), requestFor(source.release))
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "queued" {
		t.Fatal(job.State)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("restart not called")
	}
	current, _ := os.ReadFile(m.path("sub2api"))
	backup, _ := os.ReadFile(m.path("sub2api.backup"))
	pending, _ := os.ReadFile(m.path("update-pending"))
	if string(current) != string(binary) || string(backup) != "previous application" || strings.TrimSpace(string(pending)) != strings.TrimPrefix(source.release.Digest, "sha256:") {
		t.Fatal("incorrect installed state")
	}
	next, err := NewManager(m.root, source.release.Version, source.release.Revision, source, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if status := next.Status(); status.Job.State != "restarting" {
		t.Fatal("prematurely declared success")
	}
	if err := os.Remove(m.path("update-pending")); err != nil {
		t.Fatal(err)
	}
	if status := next.Status(); status.Job.State != "succeeded" || status.Job.FinishedAt == nil {
		t.Fatalf("not confirmed: %+v", status.Job)
	}
	if _, err := os.Stat(m.path("update-job.json")); err != nil {
		t.Fatal("journal not retained")
	}
}

func TestManagerDownloadIndependentOfRequestAndOnlyOneTask(t *testing.T) {
	binary := []byte("new application")
	source := &fakeSource{release: testRelease(binary), binary: binary, gate: make(chan struct{}), entered: make(chan struct{})}
	m, restarted := newTestManager(t, source)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := m.Start(ctx, requestFor(source.release)); err != nil {
		t.Fatal(err)
	}
	<-source.entered
	cancel()
	if _, err := m.Start(context.Background(), requestFor(source.release)); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	close(source.gate)
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("request cancellation stopped accepted update")
	}
}

func TestManagerRejectsChangedOrOlderTarget(t *testing.T) {
	binary := []byte("new application")
	source := &fakeSource{release: testRelease(binary), binary: binary}
	m, _ := newTestManager(t, source)
	request := requestFor(source.release)
	request.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := m.Start(context.Background(), request); !errors.Is(err, ErrTargetChanged) {
		t.Fatal(err)
	}
	source.release.Version = "0.2.7-pool.10"
	source.release.Tag = "pool-v0.2.7.10"
	if _, err := m.Start(context.Background(), requestFor(source.release)); !errors.Is(err, ErrNoUpdate) {
		t.Fatal(err)
	}
	if m.Status().Job != nil {
		t.Fatal("rejected request wrote a task")
	}
}

func TestManagerFailureNeverReplacesCurrentProgram(t *testing.T) {
	for _, failure := range []string{"download", "checksum", "version"} {
		t.Run(failure, func(t *testing.T) {
			binary := []byte("new application")
			source := &fakeSource{release: testRelease(binary), binary: binary}
			m, restarted := newTestManager(t, source)
			switch failure {
			case "download":
				source.downloadError = errors.New("network unavailable")
			case "checksum":
				source.binary = []byte("bad application")
			case "version":
				m.verify = func(context.Context, string, *Release) error { return errors.New("wrong build identity") }
			}
			if _, err := m.Start(context.Background(), requestFor(source.release)); err != nil {
				t.Fatal(err)
			}
			waitTask(t, m, "failed")
			current, _ := os.ReadFile(m.path("sub2api"))
			if string(current) != "previous application" || exists(m.path("update-pending")) {
				t.Fatal("failed verification changed runtime")
			}
			select {
			case <-restarted:
				t.Fatal("restarted failed update")
			default:
			}
		})
	}
}

func TestManagerRecoversPersistedInterruptedAndRolledBackTasks(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{true: "rollback", false: "interrupted"}[rollback], func(t *testing.T) {
			source := &fakeSource{release: testRelease([]byte("binary")), binary: []byte("binary")}
			m, _ := newTestManager(t, source)
			m.mu.Lock()
			m.job = &poolupdate.Job{ID: "saved-job", State: "downloading", Version: source.release.Version, Revision: source.release.Revision, Digest: source.release.Digest}
			err := m.saveLocked()
			m.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if rollback {
				if err := os.WriteFile(m.path("update-rolled-back"), []byte("health check failed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			next, err := NewManager(m.root, m.currentVersion, m.currentRevision, source, func() {})
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			want := "failed"
			if rollback {
				want = "rolled_back"
			}
			if actual := next.Status().Job.State; actual != want {
				t.Fatalf("want %s got %s", want, actual)
			}
		})
	}
}

func TestManagerRejectsCorruptJournal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "update-job.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root, "0.2.7-pool.11", strings.Repeat("b", 40), &fakeSource{}, func() {}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("corrupt journal must require recovery: %v", err)
	}
}

func TestManagerJournalFailureRequiresRecovery(t *testing.T) {
	binary := []byte("new program")
	source := &fakeSource{release: testRelease(binary), binary: binary}
	m, _ := newTestManager(t, source)
	if err := os.Mkdir(m.path("update-job.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), requestFor(source.release)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	status := m.Status()
	if status.Available || !status.RecoveryRequired || !exists(m.path("update-recovery-required")) {
		t.Fatal("unsafe task persistence failure did not disable updates")
	}
	if _, err := m.Start(context.Background(), requestFor(source.release)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("retry accepted after persistence failure: %v", err)
	}
}

func TestManagerSwapFailureKeepsOldProgram(t *testing.T) {
	binary := []byte("new program")
	source := &fakeSource{release: testRelease(binary), binary: binary}
	m, restarted := newTestManager(t, source)
	m.replace = func(string, string) error { return errors.New("simulated atomic swap failure") }
	if _, err := m.Start(context.Background(), requestFor(source.release)); err != nil {
		t.Fatal(err)
	}
	waitTask(t, m, "failed")
	current, _ := os.ReadFile(m.path("sub2api"))
	if string(current) != "previous application" || exists(m.path("update-pending")) {
		t.Fatal("atomic swap failure changed current program")
	}
	select {
	case <-restarted:
		t.Fatal("restarted failed swap")
	default:
	}
}

func TestManagerPostSwapJournalFailureRestoresOrRequiresRecovery(t *testing.T) {
	for _, restoreFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore succeeds", true: "restore fails"}[restoreFails], func(t *testing.T) {
			binary := []byte("new program")
			source := &fakeSource{release: testRelease(binary), binary: binary}
			m, restarted := newTestManager(t, source)
			m.replace = func(from, to string) error {
				if err := os.Rename(from, to); err != nil {
					return err
				}
				if err := os.Remove(m.path("update-job.json")); err != nil {
					return err
				}
				if err := os.Mkdir(m.path("update-job.json"), 0700); err != nil {
					return err
				}
				if restoreFails {
					if err := os.Remove(m.path("sub2api.backup")); err != nil {
						return err
					}
				}
				return nil
			}
			if _, err := m.Start(context.Background(), requestFor(source.release)); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				m.mu.Lock()
				running := m.running
				m.mu.Unlock()
				if !running {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			status := m.Status()
			if status.Available || !status.RecoveryRequired {
				t.Fatal("uncertain installed state remained available")
			}
			current, _ := os.ReadFile(m.path("sub2api"))
			if !restoreFails && (string(current) != "previous application" || exists(m.path("update-pending"))) {
				t.Fatal("backup not restored after journal failure")
			}
			if restoreFails && !exists(m.path("update-pending")) {
				t.Fatal("lost pending marker after failed restoration")
			}
			select {
			case <-restarted:
				t.Fatal("restarted uncertain update")
			default:
			}
		})
	}
}

func TestManagerRejectsSymlinkProgram(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("unrelated program"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "sub2api")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if _, err := NewManager(root, "0.2.7-pool.11", strings.Repeat("b", 40), &fakeSource{}, func() {}); err == nil {
		t.Fatal("symlink program accepted")
	}
}

func TestManagerRejectsDirectoryProgram(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub2api"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root, "0.2.7-pool.11", strings.Repeat("b", 40), &fakeSource{}, func() {}); err == nil {
		t.Fatal("directory program accepted")
	}
}

func TestManagerOrphanedPendingRequiresRecovery(t *testing.T) {
	binary := []byte("new program")
	source := &fakeSource{release: testRelease(binary), binary: binary}
	m, _ := newTestManager(t, source)
	if err := os.WriteFile(m.path("update-pending"), []byte(strings.TrimPrefix(source.release.Digest, "sha256:")), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := NewManager(m.root, m.currentVersion, m.currentRevision, source, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if status := next.Status(); status.Available || !status.RecoveryRequired {
		t.Fatal("orphaned installation marker did not require recovery")
	}
	if _, err := next.Start(context.Background(), requestFor(source.release)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyExecutableRejectsOtherFileFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub2api")
	if err := os.WriteFile(path, []byte("not an ELF binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := verifyExecutable(context.Background(), path, testRelease([]byte("not an ELF binary"))); err == nil {
		t.Fatal("non-ELF application accepted")
	}
}
