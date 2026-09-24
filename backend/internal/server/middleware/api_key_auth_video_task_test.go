package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsAsyncMediaTaskReadIncludesVideoRecoveryEndpoints(t *testing.T) {
	require.True(t, isAsyncMediaTaskRead(http.MethodGet, "/v1/videos/video-123"))
	require.True(t, isAsyncMediaTaskRead(http.MethodGet, "/v1/videos/video-123/content"))
	require.False(t, isAsyncMediaTaskRead(http.MethodGet, "/images/tasks/image-123"))
	require.False(t, isAsyncMediaTaskRead(http.MethodDelete, "/v1/videos/tasks"))
	require.False(t, isAsyncMediaTaskRead(http.MethodPost, "/v1/videos/generations"))
}
