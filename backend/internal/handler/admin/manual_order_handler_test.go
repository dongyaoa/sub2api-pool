package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type orderUpstreamRepository struct {
	service.UpstreamCenterRepository
	calls int
	in    service.UpstreamOrderInput
	err   error
}

func (r *orderUpstreamRepository) SaveOrder(_ context.Context, in service.UpstreamOrderInput) error {
	r.calls++
	r.in = in
	return r.err
}

type orderIntelligenceRepository struct {
	service.IntelligenceMonitorRepository
	calls int
	in    service.IntelligenceOrderInput
	err   error
}

func (r *orderIntelligenceRepository) SaveOrder(_ context.Context, in service.IntelligenceOrderInput) error {
	r.calls++
	r.in = in
	return r.err
}

func TestManualOrderHandlersValidateAndReturnConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	u, i := &orderUpstreamRepository{}, &orderIntelligenceRepository{}
	upstream := NewUpstreamCenterHandler(service.NewUpstreamCenterService(u, nil, nil, nil), nil)
	intelligence := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(i, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.PUT("/upstream-center/order", upstream.SaveOrder)
	router.PUT("/intelligence-monitors/plans/order", intelligence.SaveOrder)
	send := func(path, body string, status int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, response.Body.String())
		if status == http.StatusBadRequest {
			require.Contains(t, response.Body.String(), "MANUAL_ORDER_INVALID")
		}
		if status == http.StatusConflict {
			require.Contains(t, response.Body.String(), "MANUAL_ORDER_CONFLICT")
			require.Contains(t, response.Body.String(), "refresh")
		}
	}
	for _, body := range []string{`{`, `{}`, `{"scope":"suppliers","ids":null}`, `{"scope":"suppliers","ids":[1.5]}`, `{"scope":"suppliers","ids":[1,1]}`, `{"scope":"groups","ids":[]}`, `{"scope":"suppliers","ids":[0]}`} {
		send("/upstream-center/order", body, http.StatusBadRequest)
	}
	for _, body := range []string{`{}`, `{"scope":"external","ids":[]}`, `{"scope":"oauth","ids":[1,1]}`, `{"scope":"oauth","ids":null}`, `{"scope":"oauth","ids":[-1]}`} {
		send("/intelligence-monitors/plans/order", body, http.StatusBadRequest)
	}
	require.Zero(t, u.calls)
	require.Zero(t, i.calls)
	send("/upstream-center/order", `{"scope":"groups","supplier_id":9,"ids":[3,1]}`, http.StatusOK)
	require.Equal(t, []int64{3, 1}, u.in.IDs)
	require.Equal(t, int64(9), *u.in.SupplierID)
	send("/intelligence-monitors/plans/order", `{"scope":"oauth","ids":[]}`, http.StatusOK)
	require.NotNil(t, i.in.IDs)
	u.err, i.err = service.ErrManualOrderConflict, service.ErrManualOrderConflict
	send("/upstream-center/order", `{"scope":"monitors","ids":[3,1]}`, http.StatusConflict)
	send("/intelligence-monitors/plans/order", `{"scope":"intelligence","ids":[3,1]}`, http.StatusConflict)
}
