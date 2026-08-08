//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaResetterIdempotencyStub struct {
	seen        map[string]struct{}
	executions  []idempotencyexecution.Execution
	idemCalls   int
	sideEffects int
}

func (s *quotaResetterIdempotencyStub) AdminResetQuota(context.Context, int64, bool, bool, bool) (*service.UserSubscription, error) {
	panic("non-idempotent quota reset must not be used when a key is present")
}

func (s *quotaResetterIdempotencyStub) AdminResetQuotaIdempotent(_ context.Context, subscriptionID int64, _, _, _ bool, execution idempotencyexecution.Execution) (*service.UserSubscription, error) {
	s.idemCalls++
	s.executions = append(s.executions, execution)
	if _, exists := s.seen[execution.OperationKeyHash]; !exists {
		s.seen[execution.OperationKeyHash] = struct{}{}
		s.sideEffects++
	}
	return &service.UserSubscription{ID: subscriptionID}, nil
}

func TestResetQuotaUsesNormalizedTransactionalIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })

	resetter := &quotaResetterIdempotencyStub{seen: make(map[string]struct{})}
	handler := &SubscriptionHandler{quotaResetter: resetter}
	router := gin.New()
	router.POST("/subscriptions/:id/reset-quota", handler.ResetQuota)

	call := func(key string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/subscriptions/42/reset-quota", bytes.NewBufferString(`{"daily":true,"weekly":true,"monthly":true}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		router.ServeHTTP(recorder, request)
		return recorder
	}

	require.Equal(t, http.StatusOK, call("  reset-key-42  ").Code)
	require.Equal(t, http.StatusOK, call("reset-key-42").Code)
	require.Equal(t, 2, resetter.idemCalls)
	require.Equal(t, 1, resetter.sideEffects)
	require.Len(t, resetter.seen, 1)
	require.Len(t, resetter.executions, 2)
	require.Equal(t, subscriptionResetQuotaScope, resetter.executions[0].Scope)
	require.Equal(t, "admin:0", resetter.executions[0].ActorScope)
	require.Equal(t, resetter.executions[0].OperationKeyHash, resetter.executions[1].OperationKeyHash)
	claimedAt := time.Now()
	expected, err := idempotencyexecution.New(subscriptionResetQuotaScope, "admin:0", service.HashIdempotencyKey("reset-key-42"), claimedAt, claimedAt.Add(time.Hour))
	require.NoError(t, err)
	require.Contains(t, resetter.seen, expected.OperationKeyHash)
}
