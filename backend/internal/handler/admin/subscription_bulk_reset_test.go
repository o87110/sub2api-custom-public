package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionbulkreset"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBulkResetQuotaRejectsRequestsAboveMaximumBatchSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := make([]int64, subscriptionbulkreset.MaxBatchSize+1)
	for index := range ids {
		ids[index] = int64(index + 1)
	}
	body, err := json.Marshal(BulkResetSubscriptionQuotaRequest{SubscriptionIDs: ids})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/bulk-reset-quota", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "oversized-batch")

	(&SubscriptionHandler{}).BulkResetQuota(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestUpdateCurrentCycleBulkResetEligibilityRequiresEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "7"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/subscriptions/7/current-cycle-bulk-reset-eligibility", bytes.NewBufferString(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	(&SubscriptionHandler{}).UpdateCurrentCycleBulkResetEligibility(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
