package affiliatereversal

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const reversalScope = "admin.affiliates.rebates.reverse"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListRebateRecords(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := RecordFilter{
		Search:   c.Query("search"),
		Page:     page,
		PageSize: pageSize,
		SortBy:   c.Query("sort_by"),
		SortDesc: c.Query("sort_order") != "asc",
		Status:   c.Query("rebate_status"),
	}
	userTZ := c.Query("timezone")
	filter.StartAt = parseRecordStart(c.Query("start_at"), userTZ)
	filter.EndAt = parseRecordEnd(c.Query("end_at"), userTZ)
	items, total, err := h.service.ListRebateRecords(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

type PreviewRequest struct {
	OrderIDs []int64 `json:"order_ids" binding:"required,min=1,max=100"`
}

func (h *Handler) Preview(c *gin.Context) {
	middleware2.SetAuditAction(c, "admin.affiliates.rebates.reversal_preview")
	var req PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	preview, err := h.service.Preview(c.Request.Context(), req.OrderIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware2.SetAuditExtra(c, map[string]any{
		"requested_count": len(req.OrderIDs),
		"matched_count":   preview.OrderCount,
	})
	response.Success(c, preview)
}

func (h *Handler) Reverse(c *gin.Context) {
	middleware2.SetAuditAction(c, reversalScope)
	var req ReverseInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ids, err := NormalizeOrderIDs(req.OrderIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if _, err := NormalizeReason(req.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	idempotencyKey, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if idempotencyKey == "" {
		response.BadRequest(c, "Idempotency-Key header is required")
		return
	}
	ttl := service.DefaultWriteIdempotencyTTL()
	claimedAt := time.Now()
	fallbackExecution, err := idempotencyexecution.New(
		reversalScope,
		actorScope(c),
		service.HashIdempotencyKey(idempotencyKey),
		claimedAt,
		claimedAt.Add(ttl),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	execute := func(ctx context.Context) (any, error) {
		execution, ok := idempotencyexecution.FromContext(ctx)
		if !ok {
			execution = fallbackExecution
		}
		return h.service.Reverse(ctx, req, adminUserID(c), execution)
	}
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		data, execErr := execute(c.Request.Context())
		if execErr != nil {
			response.ErrorFrom(c, execErr)
			return
		}
		handleReverseSuccess(c, req, data)
		response.Success(c, data)
		return
	}
	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:          reversalScope,
		ActorScope:     actorScope(c),
		Method:         c.Request.Method,
		Route:          c.FullPath(),
		IdempotencyKey: idempotencyKey,
		Payload:        req,
		RequireKey:     true,
		TTL:            ttl,
	}, execute)
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), reversalScope, "handler_fail_close")
			logger.LegacyPrintf("custom.affiliate_reversal", "idempotency store unavailable: method=%s route=%s", c.Request.Method, c.FullPath())
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
		h.service.InvalidateCachesForOrders(ids)
	}
	if result == nil {
		response.ErrorFrom(c, ErrInvariant)
		return
	}
	handleReverseSuccess(c, req, result.Data)
	response.Success(c, result.Data)
}

func handleReverseSuccess(c *gin.Context, req ReverseInput, data any) {
	result, ok := data.(*ReverseResult)
	if !ok || result == nil {
		return
	}
	middleware2.SetAuditExtra(c, map[string]any{
		"requested_count":        len(req.OrderIDs),
		"reversed_count":         result.ReversedCount,
		"total_amount":           result.TotalRebateAmount,
		"balance_deducted":       result.TotalBalanceDeducted,
		"negative_balance_users": result.NegativeBalanceUsers,
	})
}

func actorScope(c *gin.Context) string {
	return "admin:" + strconv.FormatInt(adminUserID(c), 10)
}

func adminUserID(c *gin.Context) int64 {
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		return subject.UserID
	}
	return 0
}

func parseRecordStart(raw, userTZ string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed
	}
	if parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ); err == nil {
		return &parsed
	}
	return nil
}

func parseRecordEnd(raw, userTZ string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed
	}
	if parsed, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ); err == nil {
		end := parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
		return &end
	}
	return nil
}
