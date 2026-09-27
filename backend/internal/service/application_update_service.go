package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sysutil"
	"github.com/Wei-Shaw/sub2api/internal/poolappupdate"
	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

type applicationUpdater interface {
	Status() poolupdate.Status
	Start(context.Context, poolupdate.UpdateRequest) (*poolupdate.Job, error)
}

type applicationReleaseCheck struct {
	done    chan struct{}
	release *poolappupdate.Release
	err     error
}

// NewPoolApplicationUpdateService keeps the official download-and-restart
// experience, using only this fork's releases and its persistent runtime.
func NewPoolApplicationUpdateService(cache UpdateCache, github GitHubReleaseClient, build BuildInfo, root string) *UpdateService {
	s := NewUpdateService(cache, github, build.Version, build.BuildType)
	s.appUpdates = true
	s.currentRevision = build.Commit
	s.appSource, _ = poolappupdate.NewSourceFromEnvironment()
	switch {
	case build.BuildType != "release":
		s.appUnavailableReason = "source_build"
	case runtime.GOOS != "linux" || runtime.GOARCH != "amd64":
		s.appUnavailableReason = "unsupported_platform"
	case root == "" || !filepath.IsAbs(root) || os.Getenv("POOL_APP_UPDATE_SUPERVISED") != "1":
		s.appUnavailableReason = "runtime_not_configured"
	default:
		// A writable directory alone does not mean it is the program that the
		// supervisor will start. Never install into an unrelated data directory.
		executable, err := os.Executable()
		if err != nil || filepath.Clean(executable) != filepath.Join(filepath.Clean(root), "sub2api") {
			s.appUnavailableReason = "runtime_not_configured"
			break
		}
		if s.appSource == nil {
			break
		}
		manager, err := poolappupdate.NewManager(root, build.Version, build.Commit, s.appSource, sysutil.RestartServiceAsync)
		if err != nil {
			s.appUnavailableReason = "runtime_unwritable"
			if errors.Is(err, poolupdate.ErrUnavailable) {
				s.appUnavailableReason = "recovery_required"
			}
			break
		}
		s.appUpdater = manager
	}
	return s
}

// Cache only verified release metadata. Forced checks join an existing request,
// and a failed refresh cannot leave a stale update button enabled.
func (s *UpdateService) latestApplicationRelease(ctx context.Context, force bool) (*poolappupdate.Release, bool, error) {
	s.poolCheckMu.Lock()
	if pending := s.appChecking; pending != nil {
		s.poolCheckMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-pending.done:
			return pending.release, true, pending.err
		}
	}
	if !force && s.appRelease != nil && time.Since(s.appCheckedAt) < 5*time.Minute {
		release := s.appRelease
		s.poolCheckMu.Unlock()
		return release, true, nil
	}
	pending := &applicationReleaseCheck{done: make(chan struct{})}
	s.appChecking = pending
	s.poolCheckMu.Unlock()
	var release *poolappupdate.Release
	var err error
	if s.appSource == nil {
		err = fmt.Errorf("release source is unavailable")
	} else {
		fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		release, err = s.appSource.Latest(fetchCtx)
		cancel()
		if release == nil && err == nil {
			err = fmt.Errorf("release source returned no release")
		}
	}
	s.poolCheckMu.Lock()
	if err != nil {
		s.appRelease, s.appCheckedAt = nil, time.Time{}
	} else {
		s.appRelease, s.appCheckedAt = release, time.Now()
	}
	pending.release, pending.err = release, err
	s.appChecking = nil
	close(pending.done)
	s.poolCheckMu.Unlock()
	return release, false, err
}

func (s *UpdateService) checkApplicationUpdate(ctx context.Context, force bool) *UpdateInfo {
	info := &UpdateInfo{CurrentVersion: s.currentVersion, CurrentRevision: s.currentRevision,
		LatestVersion: s.currentVersion, BuildType: s.buildType, UpdateMethod: "binary",
		UpdateUnavailableReason: s.appUnavailableReason}
	if s.appUpdater != nil {
		status := s.appUpdater.Status()
		info.UpdateAvailable = status.Available && !status.RecoveryRequired
		if status.RecoveryRequired {
			info.UpdateUnavailableReason = "recovery_required"
		}
	}
	release, cached, err := s.latestApplicationRelease(ctx, force)
	if err != nil {
		info.Warning = "pool_release_unavailable"
		info.UpdateAvailable = false
		return info
	}
	info.Cached = cached
	info.LatestVersion, info.LatestRevision, info.UpdateDigest = release.Version, release.Revision, release.Digest
	order, err := poolupdate.CompareVersions(release.Version, s.currentVersion)
	if err != nil {
		info.Warning = "unrecognized_current_version"
		info.UpdateAvailable = false
	} else {
		// Published program releases are immutable. Rebuilds receive a new
		// numeric Pool version; a replaced tag must never silently install.
		info.HasUpdate = order > 0
	}
	info.ReleaseInfo = &ReleaseInfo{Name: "Sub2API Pool " + release.Version, Body: release.Body,
		PublishedAt: release.PublishedAt.UTC().Format(time.RFC3339), HTMLURL: release.URL}
	return info
}

func (s *UpdateService) startApplicationUpdate(ctx context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	if s.buildType != "release" || s.appUpdater == nil || s.appUnavailableReason != "" {
		return nil, infraerrors.Conflict("POOL_UPDATE_UNAVAILABLE", "Online program updates are unavailable for this deployment")
	}
	job, err := s.appUpdater.Start(ctx, request)
	switch {
	case errors.Is(err, poolupdate.ErrTargetChanged):
		return nil, infraerrors.Conflict("POOL_UPDATE_CHANGED", "A new release was published; refresh and confirm the version again")
	case errors.Is(err, poolupdate.ErrBusy):
		return nil, infraerrors.Conflict("POOL_UPDATE_BUSY", "An update is already running; check its progress")
	case errors.Is(err, poolupdate.ErrUnavailable):
		return nil, infraerrors.Conflict("POOL_UPDATE_UNAVAILABLE", "The program updater is unavailable; refresh its status before retrying")
	case errors.Is(err, poolappupdate.ErrNoUpdate):
		return nil, ErrNoUpdateAvailable
	case err != nil:
		return nil, infraerrors.Conflict("POOL_UPDATE_UNAVAILABLE", "The update could not be started; check the release connection and try again")
	default:
		return job, nil
	}
}
