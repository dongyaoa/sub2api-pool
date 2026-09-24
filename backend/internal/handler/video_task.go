package handler

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) serveStoredGrokVideoContent(
	c *gin.Context,
	owner service.VideoTaskOwner,
	record *service.VideoTaskRecord,
) bool {
	if h == nil || h.videoTasks == nil || record == nil || strings.TrimSpace(record.VideoURL) == "" {
		return false
	}
	rangeHeader := c.GetHeader("Range")
	needsPlaybackUpgrade := record.NeedsBrowserPlaybackUpgrade()
	if needsPlaybackUpgrade {
		// The first full read of a legacy object upgrades HEVC/AV1/WebM content
		// to H.264 MP4 before it reaches the browser.
		rangeHeader = ""
	}
	resp, err := h.videoTasks.OpenStoredContent(c.Request.Context(), record, rangeHeader)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if needsPlaybackUpgrade {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, (512<<20)+1))
		if readErr != nil || len(data) > 512<<20 {
			h.errorResponse(c, http.StatusBadGateway, "video_conversion_error", "Failed to read the stored video for browser conversion")
			return true
		}
		prepared, contentType, upgradeErr := h.videoTasks.UpgradeStoredContent(
			c.Request.Context(), owner, record.ID, resp.Header.Get("Content-Type"), data,
		)
		if len(prepared) == 0 {
			h.errorResponse(c, http.StatusBadGateway, "video_conversion_error", "Failed to convert the video to a browser-supported format")
			return true
		}
		_ = upgradeErr // Playback can proceed even if refreshing the R2 object failed.
		c.Header("Cache-Control", "private, max-age=3600")
		c.Header("Accept-Ranges", "bytes")
		service.MarkResponseCommitted(c)
		c.Data(http.StatusOK, contentType, prepared)
		return true
	}

	for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition", "ETag", "Last-Modified"} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			c.Header(name, value)
		}
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(strings.ToLower(contentType), "video/") {
		contentType = strings.TrimSpace(record.ContentType)
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "video/") {
		contentType = "video/mp4"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "private, max-age=3600")
	c.Status(resp.StatusCode)
	service.MarkResponseCommitted(c)
	_, _ = io.Copy(c.Writer, resp.Body)
	return true
}

func writeStoredGrokVideoStatus(c *gin.Context, record *service.VideoTaskRecord) {
	payload := gin.H{
		"id":         record.ID,
		"request_id": record.ID,
		"object":     "video.generation.task",
		"status":     record.Status,
		"created_at": record.CreatedAt,
	}
	if record.Error != nil {
		payload["error"] = record.Error
	}
	if record.CompletedAt != nil {
		payload["completed_at"] = record.CompletedAt
	}
	if strings.TrimSpace(record.VideoURL) != "" {
		prefix := ""
		if c.Request != nil && c.Request.URL != nil && strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			prefix = "/v1"
		}
		payload["video"] = gin.H{"url": prefix + "/videos/" + url.PathEscape(record.ID) + "/content"}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, payload)
}
