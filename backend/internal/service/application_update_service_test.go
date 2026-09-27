//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolappupdate"
	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
	"github.com/stretchr/testify/require"
)

type applicationSourceStub struct {
	release *poolappupdate.Release
	err     error
	calls   atomic.Int32
	entered chan struct{}
	resume  chan struct{}
}

func (s *applicationSourceStub) Latest(ctx context.Context) (*poolappupdate.Release, error) {
	if s.calls.Add(1) == 1 && s.entered != nil {
		close(s.entered)
	}
	if s.resume != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.resume:
		}
	}
	return s.release, s.err
}
func (*applicationSourceStub) Download(context.Context, *poolappupdate.Release, string) error {
	panic("metadata checks must not download a program")
}

type applicationUpdaterStub struct {
	status  poolupdate.Status
	err     error
	request poolupdate.UpdateRequest
}

func (s *applicationUpdaterStub) Status() poolupdate.Status { return s.status }
func (s *applicationUpdaterStub) Start(_ context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	s.request = request
	return &poolupdate.Job{ID: "accepted", State: "queued", Version: request.Version, Digest: request.Digest}, s.err
}

func applicationTestService() (*UpdateService, *applicationSourceStub, *applicationUpdaterStub) {
	source := &applicationSourceStub{release: &poolappupdate.Release{
		Version: "0.2.7-pool.12", Revision: strings.Repeat("b", 40), Digest: "sha256:" + strings.Repeat("c", 64),
		PublishedAt: time.Now(), URL: "https://github.com/dongyaoa/sub2api-pool/releases/tag/pool-v0.2.7.12", Body: "Program update",
	}}
	updater := &applicationUpdaterStub{status: poolupdate.Status{Available: true}}
	svc := NewUpdateService(nil, nil, "0.2.7-pool.11", "release")
	svc.currentRevision = strings.Repeat("a", 40)
	svc.appUpdates, svc.appSource, svc.appUpdater = true, source, updater
	return svc, source, updater
}

func TestApplicationUpdateChecksOwnReleaseWithoutHostHelper(t *testing.T) {
	svc, source, _ := applicationTestService()
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "binary", info.UpdateMethod)
	require.True(t, info.HasUpdate)
	require.True(t, info.UpdateAvailable)
	require.Empty(t, info.ImageDigest)
	require.Equal(t, source.release.Digest, info.UpdateDigest)
	require.Equal(t, source.release.URL, info.ReleaseInfo.HTMLURL)
	require.Equal(t, source.release.Body, info.ReleaseInfo.Body)
	require.True(t, svc.PoolUpdatesEnabled())
	status, err := svc.PoolUpdateStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Available)
	_, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.EqualValues(t, 1, source.calls.Load())
	version, revision := svc.GetCurrentBuild()
	require.Equal(t, "0.2.7-pool.11", version)
	require.Equal(t, strings.Repeat("a", 40), revision)
	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrOfficialUpdatesDisabled)
}

func TestApplicationUpdateFailedRefreshInvalidatesOldRelease(t *testing.T) {
	svc, source, _ := applicationTestService()
	_, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	source.err = errors.New("offline")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "pool_release_unavailable", info.Warning)
	require.False(t, info.HasUpdate)
	require.False(t, info.UpdateAvailable)
	require.Empty(t, info.UpdateDigest)
	_, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.EqualValues(t, 3, source.calls.Load())
}

func TestApplicationUpdateVersionAndDeploymentBoundaries(t *testing.T) {
	for _, tt := range []struct {
		current string
		update  bool
		warning string
	}{
		{"0.2.7-pool.9", true, ""}, {"0.2.7-pool.12", false, ""}, {"0.2.7-pool.13", false, ""}, {"dev", false, "unrecognized_current_version"},
	} {
		t.Run(tt.current, func(t *testing.T) {
			svc, _, _ := applicationTestService()
			svc.currentVersion = tt.current
			info, err := svc.CheckUpdate(context.Background(), false)
			require.NoError(t, err)
			require.Equal(t, tt.update, info.HasUpdate)
			require.Equal(t, tt.warning, info.Warning)
		})
	}
	svc, _, updater := applicationTestService()
	updater.status.RecoveryRequired = true
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.False(t, info.UpdateAvailable)
	require.Equal(t, "recovery_required", info.UpdateUnavailableReason)
	svc.appUpdater, svc.appUnavailableReason = nil, "runtime_not_configured"
	info, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.False(t, info.UpdateAvailable)
	require.Equal(t, "runtime_not_configured", info.UpdateUnavailableReason)
	_, err = svc.StartPoolUpdate(context.Background(), poolupdate.UpdateRequest{})
	require.ErrorContains(t, err, "unavailable")
	svc.appUnavailableReason = "recovery_required"
	status, err := svc.PoolUpdateStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Available)
	require.True(t, status.RecoveryRequired, "persisted browser tasks must see constructor recovery failures immediately")
}

func TestApplicationUpdateSubmissionUsesPinnedTargetAndMapsFailures(t *testing.T) {
	svc, source, updater := applicationTestService()
	request := poolupdate.UpdateRequest{Version: source.release.Version, Digest: source.release.Digest}
	job, err := svc.StartPoolUpdate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, request, updater.request)
	require.Equal(t, "queued", job.State)
	for _, tt := range []struct {
		cause   error
		message string
	}{
		{poolupdate.ErrBusy, "already running"}, {poolupdate.ErrTargetChanged, "new release"}, {poolupdate.ErrUnavailable, "unavailable"}, {errors.New("private filesystem detail"), "could not be started"},
	} {
		updater.err = tt.cause
		_, err = svc.StartPoolUpdate(context.Background(), request)
		require.ErrorContains(t, err, tt.message)
		require.NotContains(t, err.Error(), "private filesystem")
	}
}

func TestApplicationConcurrentChecksShareRequest(t *testing.T) {
	svc, source, _ := applicationTestService()
	source.entered, source.resume = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { _, _, err := svc.latestApplicationRelease(context.Background(), true); done <- err }()
	<-source.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := svc.latestApplicationRelease(ctx, true)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, source.calls.Load())
	close(source.resume)
	require.NoError(t, <-done)
}
