package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Keep the public OAuth constructor compatible with existing callers and tests.
func ProvideOpenAIOAuthHandler(oauth *service.OpenAIOAuthService, admin service.AdminService,
	quota *service.OpenAIQuotaService, limits *service.RateLimitService, reauth *service.OpenAIReauthService) *OpenAIOAuthHandler {
	h := NewOpenAIOAuthHandler(oauth, admin, quota, limits)
	h.reauth = reauth
	return h
}

func (h *OpenAIOAuthHandler) ListAutoReauth(c *gin.Context) {
	if h.reauth == nil {
		response.Error(c, http.StatusServiceUnavailable, "auto_reauth_unavailable")
		return
	}
	statuses, err := h.reauth.List(c.Request.Context())
	if err != nil {
		var sqlErr interface{ SQLState() string }
		state := ""
		if errors.As(err, &sqlErr) {
			state = sqlErr.SQLState()
		}
		slog.Error("openai_auto_reauth_status_load_failed", "sql_state", state)
		code := "auto_reauth_status_unavailable"
		if state == "42P01" || state == "42703" {
			code = "auto_reauth_schema_unavailable"
		}
		response.Error(c, http.StatusServiceUnavailable, code)
		return
	}
	response.Success(c, gin.H{"worker_configured": h.reauth.WorkerConfigured(), "encryption_key_configured": h.reauth.EncryptionConfigured(), "accounts": statuses})
}

func (h *OpenAIOAuthHandler) BindAutoReauthCredentials(c *gin.Context) {
	if h.reauth == nil {
		response.Error(c, http.StatusServiceUnavailable, "auto_reauth_unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid_account")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var input service.OpenAIReauthCredentialsInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "invalid_format")
		return
	}
	status, err := h.reauth.BindCredentials(c.Request.Context(), id, input)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, status)
}

func (h *OpenAIOAuthHandler) ImportAutoReauth(c *gin.Context) {
	if h.reauth == nil {
		response.Error(c, http.StatusServiceUnavailable, "auto_reauth_unavailable")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	var input service.OpenAIReauthImportInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "invalid_format")
		return
	}
	results, err := h.reauth.Import(c.Request.Context(), input)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"results": results})
}

func (h *OpenAIOAuthHandler) SetAutoReauthEnabled(c *gin.Context) {
	if h.reauth == nil {
		response.Error(c, http.StatusServiceUnavailable, "auto_reauth_unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid_account")
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Enabled == nil {
		response.BadRequest(c, "invalid_request")
		return
	}
	if err = h.reauth.SetEnabled(c.Request.Context(), id, *input.Enabled); err != nil {
		response.BadRequest(c, "auto_reauth_configuration_failed")
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *OpenAIOAuthHandler) RunAutoReauth(c *gin.Context) {
	if h.reauth == nil {
		response.Error(c, http.StatusServiceUnavailable, "auto_reauth_unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid_account")
		return
	}
	if err = h.reauth.Retry(c.Request.Context(), id); err != nil {
		response.BadRequest(c, "auto_reauth_retry_failed")
		return
	}
	response.Success(c, gin.H{"success": true})
}
