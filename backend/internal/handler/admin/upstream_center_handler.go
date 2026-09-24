package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UpstreamCenterHandler is registered exclusively under the admin middleware.
// Credentials never appear in responses, including validation errors.
type UpstreamCenterHandler struct {
	svc     *service.UpstreamCenterService
	finance *service.UpstreamFinanceService
}

func NewUpstreamCenterHandler(svc *service.UpstreamCenterService, financeSvc *service.UpstreamFinanceService) *UpstreamCenterHandler {
	return &UpstreamCenterHandler{svc: svc, finance: financeSvc}
}

func (h *UpstreamCenterHandler) Overview(c *gin.Context) {
	result, err := h.svc.Overview(c.Request.Context(), c.DefaultQuery("window", "24h"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

type upstreamSupplierRequest struct {
	Name    *string `json:"name"`
	Website *string `json:"website"`
	Notes   *string `json:"notes"`
}

func (h *UpstreamCenterHandler) CreateSupplier(c *gin.Context) { h.saveSupplier(c, 0) }
func (h *UpstreamCenterHandler) UpdateSupplier(c *gin.Context) {
	if id, ok := parseUpstreamID(c); ok {
		h.saveSupplier(c, id)
	}
}
func (h *UpstreamCenterHandler) saveSupplier(c *gin.Context, id int64) {
	var in upstreamSupplierRequest
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "invalid supplier configuration")
		return
	}
	result, err := h.svc.SaveSupplier(c.Request.Context(), id, in.Name, in.Website, in.Notes)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *UpstreamCenterHandler) DeleteSupplier(c *gin.Context) {
	if id, ok := parseUpstreamID(c); ok {
		if response.ErrorFrom(c, h.svc.ArchiveSupplier(c.Request.Context(), id)) {
			return
		}
		response.Success(c, nil)
	}
}
func (h *UpstreamCenterHandler) CreateTarget(c *gin.Context) { h.saveTarget(c, 0) }
func (h *UpstreamCenterHandler) UpdateTarget(c *gin.Context) {
	if id, ok := parseUpstreamID(c); ok {
		h.saveTarget(c, id)
	}
}
func (h *UpstreamCenterHandler) saveTarget(c *gin.Context, id int64) {
	var in service.UpstreamTargetInput
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "invalid monitoring configuration")
		return
	}
	result, err := h.svc.SaveTarget(c.Request.Context(), id, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *UpstreamCenterHandler) DeleteTarget(c *gin.Context) {
	if id, ok := parseUpstreamID(c); ok {
		if response.ErrorFrom(c, h.svc.ArchiveTarget(c.Request.Context(), id)) {
			return
		}
		response.Success(c, nil)
	}
}
func (h *UpstreamCenterHandler) Run(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	result, err := h.svc.RunCheck(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *UpstreamCenterHandler) History(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	from, to, ok := parseUpstreamOptionalRange(c)
	if !ok {
		return
	}
	page, size := upstreamPagination(c)
	result, err := h.svc.History(c.Request.Context(), service.UpstreamHistoryQuery{TargetID: id, Model: strings.TrimSpace(c.Query("model")), Page: page, PageSize: size, From: from, To: to})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *UpstreamCenterHandler) Models(c *gin.Context) {
	var in service.UpstreamModelsInput
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "invalid model discovery request")
		return
	}
	models, err := h.svc.Models(c.Request.Context(), in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"models": models})
}
func (h *UpstreamCenterHandler) SyncBalance(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	result, err := h.finance.SyncBalance(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *UpstreamCenterHandler) Finance(c *gin.Context) {
	from, to, ok := parseUpstreamOptionalRange(c)
	if !ok {
		return
	}
	if from == nil {
		v := timezone.Today()
		from = &v
	}
	if to == nil {
		v := timezone.Today().AddDate(0, 0, 1)
		to = &v
	}
	if !from.Before(*to) {
		response.BadRequest(c, "from must precede to")
		return
	}
	supplierID, ok := parseUpstreamQueryID(c, "supplier_id")
	if !ok {
		return
	}
	targetID, ok := parseUpstreamQueryID(c, "target_id")
	if !ok {
		return
	}
	page, size := upstreamPagination(c)
	result, err := h.finance.Details(c.Request.Context(), service.UpstreamFinanceQuery{SupplierID: supplierID, TargetID: targetID, From: *from, To: *to, Page: page, PageSize: size})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func parseUpstreamID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid upstream ID")
		return 0, false
	}
	return id, true
}
func parseUpstreamQueryID(c *gin.Context, name string) (*int64, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return nil, false
	}
	return &id, true
}
func upstreamPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
func parseUpstreamOptionalRange(c *gin.Context) (*time.Time, *time.Time, bool) {
	var from, to *time.Time
	for _, item := range []struct {
		name string
		dst  **time.Time
	}{{"from", &from}, {"to", &to}} {
		if raw := strings.TrimSpace(c.Query(item.name)); raw != "" {
			v, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				response.BadRequest(c, "invalid "+item.name+"; use ISO 8601 with timezone")
				return nil, nil, false
			}
			*item.dst = &v
		}
	}
	if from != nil && to != nil && !from.Before(*to) {
		response.BadRequest(c, "from must precede to")
		return nil, nil, false
	}
	return from, to, true
}
