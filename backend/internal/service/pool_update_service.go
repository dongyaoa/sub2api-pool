package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
)

type poolReleaseRegistry interface {
	Latest(context.Context) (*poolupdate.Release, error)
}

type poolUpdaterClient interface {
	Status(context.Context) (*poolupdate.Status, error)
	Start(context.Context, poolupdate.UpdateRequest) (*poolupdate.Job, error)
}

type poolReleaseCheck struct {
	done    chan struct{}
	release *poolupdate.Release
	err     error
}

// Concurrent force checks share a single registry read. Waiting callers retain
// their own cancellation deadline and never queue another complete network read.
func (s *UpdateService) latestPoolRelease(ctx context.Context, force bool) (*poolupdate.Release, bool, error) {
	s.poolCheckMu.Lock()
	if pending := s.poolChecking; pending != nil {
		s.poolCheckMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-pending.done:
			return pending.release, true, pending.err
		}
	}
	if !force && s.poolRelease != nil && time.Since(s.poolCheckedAt) < 5*time.Minute {
		release := s.poolRelease
		s.poolCheckMu.Unlock()
		return release, true, nil
	}
	pending := &poolReleaseCheck{done: make(chan struct{})}
	s.poolChecking = pending
	s.poolCheckMu.Unlock()
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	release, err := s.poolRegistry.Latest(fetchCtx)
	cancel()
	if release == nil && err == nil {
		err = fmt.Errorf("registry returned no release")
	}
	s.poolCheckMu.Lock()
	if err != nil {
		s.poolRelease, s.poolCheckedAt = nil, time.Time{}
	} else {
		s.poolRelease, s.poolCheckedAt = release, time.Now()
	}
	pending.release, pending.err = release, err
	s.poolChecking = nil
	close(pending.done)
	s.poolCheckMu.Unlock()
	return release, false, err
}

// NewPoolUpdateService checks only the published Pool registry image. Official
// binary replacement and remote rollback remain disabled in this fork.
func NewPoolUpdateService(cache UpdateCache, github GitHubReleaseClient, build BuildInfo, socket string) *UpdateService {
	s := NewUpdateService(cache, github, build.Version, build.BuildType)
	s.currentRevision = build.Commit
	s.poolRegistry = poolupdate.NewRegistry()
	if socket = strings.TrimSpace(socket); socket != "" && filepath.IsAbs(socket) {
		s.poolUpdater = poolupdate.NewClient(socket)
	}
	return s
}

func (s *UpdateService) PoolUpdatesEnabled() bool { return s.poolRegistry != nil }

func (s *UpdateService) GetCurrentBuild() (string, string) {
	return s.currentVersion, s.currentRevision
}

func (s *UpdateService) checkPoolUpdate(ctx context.Context, force bool) *UpdateInfo {
	info := &UpdateInfo{CurrentVersion: s.currentVersion, CurrentRevision: s.currentRevision,
		LatestVersion: s.currentVersion, BuildType: s.buildType, UpdateMethod: "container"}
	status, statusErr := s.PoolUpdateStatus(ctx)
	switch {
	case s.buildType != "release":
		info.UpdateUnavailableReason = "source_build"
	case s.poolUpdater == nil:
		info.UpdateUnavailableReason = "helper_not_configured"
	case statusErr != nil || status == nil:
		info.UpdateUnavailableReason = "helper_unavailable"
	case status.RecoveryRequired:
		info.UpdateUnavailableReason = "recovery_required"
	case !status.Available:
		info.UpdateUnavailableReason = "helper_unavailable"
	default:
		info.UpdateAvailable = true
	}
	// A short process-local cache never consumes the old official release cache.
	release, cached, err := s.latestPoolRelease(ctx, force)
	if err != nil {
		info.Warning = "pool_registry_unavailable"
		info.UpdateAvailable = false
		return info
	}
	info.Cached = cached
	info.LatestVersion, info.LatestRevision, info.ImageDigest = release.Version, release.Revision, release.Digest
	order, err := poolupdate.CompareVersions(release.Version, s.currentVersion)
	if err != nil {
		info.Warning = "unrecognized_current_version"
		info.UpdateAvailable = false
	} else {
		// Same-version rebuilds are visible only when both revisions are known;
		// published tags may be rebuilt, but older semantic versions never win.
		info.HasUpdate = order > 0 || (order == 0 && s.currentRevision != "" && s.currentRevision != "unknown" && s.currentRevision != "docker" && release.Revision != s.currentRevision)
	}
	info.ReleaseInfo = &ReleaseInfo{Name: "Sub2API Pool " + release.Version,
		PublishedAt: release.Created.UTC().Format(time.RFC3339),
		HTMLURL:     "https://github.com/dongyaoa/sub2api-pool/commit/" + release.Revision}
	return info
}

func (s *UpdateService) PoolUpdateStatus(ctx context.Context) (*poolupdate.Status, error) {
	if s.poolUpdater == nil {
		return &poolupdate.Status{Available: false}, nil
	}
	statusCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.poolUpdater.Status(statusCtx)
}

// StartPoolUpdate revalidates the exact image confirmed in the UI. Only the
// host helper can run Docker; the web process never receives its socket.
func (s *UpdateService) StartPoolUpdate(ctx context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	if s.buildType != "release" || s.poolRegistry == nil || s.poolUpdater == nil {
		return nil, infraerrors.Conflict("POOL_UPDATE_UNAVAILABLE", "Online image updater is not configured for this deployment")
	}
	info := s.checkPoolUpdate(ctx, true)
	if info.Warning != "" || !info.UpdateAvailable {
		return nil, infraerrors.Conflict("POOL_UPDATE_UNAVAILABLE", "The image registry or update service is unavailable; check again before updating")
	}
	if !info.HasUpdate {
		return nil, ErrNoUpdateAvailable
	}
	if request.Version != info.LatestVersion || request.Digest != info.ImageDigest {
		return nil, infraerrors.Conflict("POOL_UPDATE_CHANGED", "The published image changed; refresh and confirm the new version")
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	job, err := s.poolUpdater.Start(startCtx, request)
	if err != nil {
		return nil, infraerrors.Conflict("POOL_UPDATE_REJECTED", "The update service did not accept the request; check the update status before retrying")
	}
	if job == nil || job.ID == "" {
		return nil, fmt.Errorf("update service returned an invalid task")
	}
	return job, nil
}
