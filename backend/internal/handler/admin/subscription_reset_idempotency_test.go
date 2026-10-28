//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaResetProjectionStub struct {
	subscriptionQuotaResetter
	calls int
}

func (s *quotaResetProjectionStub) AdminResetQuota(context.Context, int64, bool, bool, bool) (*service.UserSubscription, error) {
	s.calls++
	return &service.UserSubscription{
		ID: 42, UserID: 7, GroupID: 8, DailyUsageUSD: 12,
		User:  &service.User{ID: 7, Email: "private@example.com", Balance: 99},
		Group: &service.Group{ID: 8, Name: "private", RateMultiplier: 42},
	}, nil
}

func (s *quotaResetProjectionStub) AdminResetQuotaIdempotent(ctx context.Context, id int64, daily, weekly, monthly bool, _ idempotencyexecution.Execution) (*service.UserSubscription, error) {
	return s.AdminResetQuota(ctx, id, daily, weekly, monthly)
}

func TestResetQuotaScopedResponseProjectsFreshAndReplayedResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := service.DefaultIdempotencyCoordinator()
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	for _, key := range []string{"", "reset-projection"} {
		service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
		resetter := &quotaResetProjectionStub{}
		handler := &SubscriptionHandler{quotaResetter: resetter}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if mode := c.GetHeader("X-Test-Mode"); mode != "admin" {
				permissions := []string{"subscriptions.reset"}
				if mode == "read" {
					permissions = append(permissions, "subscriptions.read")
				}
				c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{
					Policy: adminaccess.Policy{GlobalPermissions: permissions},
				}))
			}
		})
		router.POST("/subscriptions/:id/reset-quota", handler.ResetQuota)
		for i, mode := range []string{"admin", "write", "read"} {
			request := httptest.NewRequest(http.MethodPost, "/subscriptions/42/reset-quota", bytes.NewBufferString(`{"daily":true}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", key)
			request.Header.Set("X-Test-Mode", mode)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			if key != "" && i > 0 {
				require.Equal(t, "true", recorder.Header().Get("X-Idempotency-Replayed"))
			}
			var result struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			require.JSONEq(t, "42", string(result.Data["id"]))
			if mode == "admin" {
				require.Contains(t, result.Data, "user")
			} else {
				require.NotContains(t, result.Data, "user")
				require.NotContains(t, result.Data, "group")
				if mode == "write" {
					require.Len(t, result.Data, 1)
				} else {
					require.Contains(t, result.Data, "daily_usage_usd")
				}
			}
		}
		if key != "" {
			require.Equal(t, 1, resetter.calls)
		} else {
			require.Equal(t, 3, resetter.calls)
		}
	}
}

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
