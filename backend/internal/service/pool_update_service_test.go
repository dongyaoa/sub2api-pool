//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
	"github.com/stretchr/testify/require"
)

type poolRegistryStub struct {
	release *poolupdate.Release
	err     error
	calls   int
}

func (s *poolRegistryStub) Latest(context.Context) (*poolupdate.Release, error) {
	s.calls++
	return s.release, s.err
}

type poolUpdaterStub struct {
	status     *poolupdate.Status
	err        error
	startCalls int
	request    poolupdate.UpdateRequest
}

func (s *poolUpdaterStub) Status(context.Context) (*poolupdate.Status, error) { return s.status, s.err }
func (s *poolUpdaterStub) Start(ctx context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	s.startCalls++
	s.request = request
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &poolupdate.Job{ID: "accepted", State: "queued", Version: request.Version, Digest: request.Digest}, s.err
}

func poolTestService() (*UpdateService, *poolRegistryStub, *poolUpdaterStub) {
	registry := &poolRegistryStub{release: &poolupdate.Release{
		Version: "0.2.7-pool.5", Revision: strings.Repeat("b", 40),
		Digest: "sha256:" + strings.Repeat("c", 64), Created: time.Now(),
	}}
	helper := &poolUpdaterStub{status: &poolupdate.Status{Available: true}}
	svc := NewPoolUpdateService(nil, nil, BuildInfo{Version: "0.2.7-pool.4", Commit: strings.Repeat("a", 40), BuildType: "release"}, "")
	svc.poolRegistry, svc.poolUpdater = registry, helper
	return svc, registry, helper
}

func TestPoolRegistryCheckUsesOwnCacheAndBuildRevision(t *testing.T) {
	svc, registry, _ := poolTestService()
	// A nil official client/cache must never be touched on this path.
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.True(t, info.UpdateAvailable)
	require.Equal(t, "container", info.UpdateMethod)
	require.Equal(t, strings.Repeat("a", 40), info.CurrentRevision)
	require.Contains(t, info.ReleaseInfo.HTMLURL, "dongyaoa/sub2api-pool/commit/")
	require.Equal(t, 1, registry.calls)
	info, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.Equal(t, 1, registry.calls)
	_, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, 2, registry.calls)
	version, revision := svc.GetCurrentBuild()
	require.Equal(t, "0.2.7-pool.4", version)
	require.Equal(t, strings.Repeat("a", 40), revision)
	require.Equal(t, 2, registry.calls, "version endpoint must not contact the registry")
}

func TestPoolRegistryFailureDoesNotReportStaleSuccess(t *testing.T) {
	svc, registry, _ := poolTestService()
	_, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	registry.err = errors.New("unavailable")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "pool_registry_unavailable", info.Warning)
	require.False(t, info.UpdateAvailable)
	require.False(t, info.HasUpdate)
	require.Empty(t, info.ImageDigest)
	info, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "pool_registry_unavailable", info.Warning, "a failed forced refresh must invalidate the old success")
	require.Equal(t, 3, registry.calls)
}

type blockingPoolRegistry struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	value   *poolupdate.Release
}

func (s *blockingPoolRegistry) Latest(ctx context.Context) (*poolupdate.Release, error) {
	if s.calls.Add(1) == 1 {
		close(s.entered)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return s.value, nil
	}
}

func TestPoolConcurrentForceChecksShareFetchAndRespectCancellation(t *testing.T) {
	svc, registry, _ := poolTestService()
	block := &blockingPoolRegistry{entered: make(chan struct{}), release: make(chan struct{}), value: registry.release}
	svc.poolRegistry = block
	first := make(chan error, 1)
	go func() { _, _, err := svc.latestPoolRelease(context.Background(), true); first <- err }()
	<-block.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := svc.latestPoolRelease(ctx, true)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, block.calls.Load())
	second := make(chan error, 1)
	go func() { _, _, err := svc.latestPoolRelease(context.Background(), false); second <- err }()
	close(block.release)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	require.EqualValues(t, 1, block.calls.Load())
}

func TestPoolVersionComparisonAndRebuild(t *testing.T) {
	for _, tt := range []struct {
		name, current, commit, latest string
		want                          bool
		warning                       string
	}{
		{"same", "0.2.7-pool.5", strings.Repeat("b", 40), "0.2.7-pool.5", false, ""},
		{"rebuild", "0.2.7-pool.5", strings.Repeat("a", 40), "0.2.7-pool.5", true, ""},
		{"unknown revision", "0.2.7-pool.5", "unknown", "0.2.7-pool.5", false, ""},
		{"older image", "0.2.7-pool.6", strings.Repeat("a", 40), "0.2.7-pool.5", false, ""},
		{"numeric pool versions", "0.2.7-pool.9", strings.Repeat("a", 40), "0.2.7-pool.10", true, ""},
		{"invalid current", "dev", strings.Repeat("a", 40), "0.2.7-pool.5", false, "unrecognized_current_version"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, registry, _ := poolTestService()
			svc.currentVersion, svc.currentRevision, registry.release.Version = tt.current, tt.commit, tt.latest
			info, err := svc.CheckUpdate(context.Background(), false)
			require.NoError(t, err)
			require.Equal(t, tt.want, info.HasUpdate)
			require.Equal(t, tt.warning, info.Warning)
		})
	}
}

func TestPoolUpdateCapabilityFailures(t *testing.T) {
	for _, reason := range []string{"source_build", "helper_not_configured", "helper_unavailable", "recovery_required"} {
		t.Run(reason, func(t *testing.T) {
			svc, registry, helper := poolTestService()
			switch reason {
			case "source_build":
				svc.buildType = "source"
			case "helper_not_configured":
				svc.poolUpdater = nil
			case "helper_unavailable":
				helper.err = errors.New("offline")
			case "recovery_required":
				helper.status.RecoveryRequired = true
			}
			info, err := svc.CheckUpdate(context.Background(), false)
			require.NoError(t, err)
			require.Equal(t, reason, info.UpdateUnavailableReason)
			require.False(t, info.UpdateAvailable)
			require.True(t, info.HasUpdate, "the new image is still discoverable before onboarding")
			_, err = svc.StartPoolUpdate(context.Background(), poolupdate.UpdateRequest{Version: registry.release.Version, Digest: registry.release.Digest})
			require.Error(t, err)
			require.Zero(t, helper.startCalls)
		})
	}
}

func TestPoolUpdateRevalidatesConfirmedTarget(t *testing.T) {
	svc, registry, helper := poolTestService()
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	request := poolupdate.UpdateRequest{Version: info.LatestVersion, Digest: info.ImageDigest}
	registry.release = &poolupdate.Release{Version: "0.2.7-pool.6", Revision: strings.Repeat("d", 40), Digest: "sha256:" + strings.Repeat("e", 64)}
	_, err = svc.StartPoolUpdate(context.Background(), request)
	require.ErrorContains(t, err, "changed")
	require.Zero(t, helper.startCalls)
	request = poolupdate.UpdateRequest{Version: registry.release.Version, Digest: registry.release.Digest}
	job, err := svc.StartPoolUpdate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "queued", job.State)
	require.Equal(t, 1, helper.startCalls)
	require.Equal(t, request, helper.request)
	require.Equal(t, 3, registry.calls)
}

func TestPoolUpdateRejectsNoNewImageAndBinaryRollback(t *testing.T) {
	svc, registry, helper := poolTestService()
	svc.currentVersion, svc.currentRevision = registry.release.Version, registry.release.Revision
	_, err := svc.StartPoolUpdate(context.Background(), poolupdate.UpdateRequest{Version: registry.release.Version, Digest: registry.release.Digest})
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
	require.Zero(t, helper.startCalls)
	require.Error(t, svc.Rollback())
	require.Error(t, svc.PerformUpdate(context.Background()))
}
