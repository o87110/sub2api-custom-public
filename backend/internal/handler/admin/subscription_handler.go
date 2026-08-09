package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionbulkreset"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// toResponsePagination converts pagination.PaginationResult to response.PaginationResult
func toResponsePagination(p *pagination.PaginationResult) *response.PaginationResult {
	if p == nil {
		return nil
	}
	return &response.PaginationResult{
		Total:    p.Total,
		Page:     p.Page,
		PageSize: p.PageSize,
		Pages:    p.Pages,
	}
}

// SubscriptionHandler handles admin subscription management
type SubscriptionHandler struct {
	subscriptionService *service.SubscriptionService
	quotaResetter       subscriptionQuotaResetter
	bulkResetService    *subscriptionbulkreset.Service
}

type subscriptionQuotaResetter interface {
	AdminResetQuota(context.Context, int64, bool, bool, bool) (*service.UserSubscription, error)
	AdminResetQuotaIdempotent(context.Context, int64, bool, bool, bool, idempotencyexecution.Execution) (*service.UserSubscription, error)
}

const (
	subscriptionResetQuotaScope     = "admin.subscriptions.reset_quota"
	subscriptionBulkResetQuotaScope = "admin.subscriptions.bulk_reset_quota"
)

// NewSubscriptionHandler creates a new admin subscription handler
func NewSubscriptionHandler(subscriptionService *service.SubscriptionService, bulkResetService *subscriptionbulkreset.Service) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
		quotaResetter:       subscriptionService,
		bulkResetService:    bulkResetService,
	}
}

// AssignSubscriptionRequest represents assign subscription request
type AssignSubscriptionRequest struct {
	UserID              int64  `json:"user_id" binding:"required"`
	GroupID             int64  `json:"group_id" binding:"required"`
	ValidityDays        int    `json:"validity_days" binding:"omitempty,max=36500"` // max 100 years
	Notes               string `json:"notes"`
	AllowBulkQuotaReset bool   `json:"allow_bulk_quota_reset"`
}

// BulkAssignSubscriptionRequest represents bulk assign subscription request
type BulkAssignSubscriptionRequest struct {
	UserIDs             []int64 `json:"user_ids" binding:"required,min=1"`
	GroupID             int64   `json:"group_id" binding:"required"`
	ValidityDays        int     `json:"validity_days" binding:"omitempty,max=36500"` // max 100 years
	Notes               string  `json:"notes"`
	AllowBulkQuotaReset bool    `json:"allow_bulk_quota_reset"`
}

// AdjustSubscriptionRequest represents adjust subscription request (extend or shorten)
type AdjustSubscriptionRequest struct {
	Days int `json:"days" binding:"required,min=-36500,max=36500"` // negative to shorten, positive to extend
}

// List handles listing all subscriptions with pagination and filters
// GET /api/v1/admin/subscriptions
func (h *SubscriptionHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	// Parse optional filters
	var userID, groupID *int64
	if userIDStr := c.Query("user_id"); userIDStr != "" {
		if id, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
			userID = &id
		}
	}
	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		if id, err := strconv.ParseInt(groupIDStr, 10, 64); err == nil {
			groupID = &id
		}
	}
	status := c.Query("status")
	platform := c.Query("platform")

	// Parse sorting parameters
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	subscriptions, pagination, err := h.subscriptionService.List(c.Request.Context(), page, pageSize, userID, groupID, status, platform, sortBy, sortOrder)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminUserSubscription, 0, len(subscriptions))
	for i := range subscriptions {
		out = append(out, *dto.UserSubscriptionFromServiceAdmin(&subscriptions[i]))
	}
	response.PaginatedWithResult(c, out, toResponsePagination(pagination))
}

// GetByID handles getting a subscription by ID
// GET /api/v1/admin/subscriptions/:id
func (h *SubscriptionHandler) GetByID(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	subscription, err := h.subscriptionService.GetByID(c.Request.Context(), subscriptionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserSubscriptionFromServiceAdmin(subscription))
}

// GetProgress handles getting subscription usage progress
// GET /api/v1/admin/subscriptions/:id/progress
func (h *SubscriptionHandler) GetProgress(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	progress, err := h.subscriptionService.GetSubscriptionProgress(c.Request.Context(), subscriptionID)
	if err != nil {
		response.NotFound(c, "Subscription not found")
		return
	}

	response.Success(c, progress)
}

// Assign handles assigning a subscription to a user
// POST /api/v1/admin/subscriptions/assign
func (h *SubscriptionHandler) Assign(c *gin.Context) {
	var req AssignSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	// Get admin user ID from context
	adminID := getAdminIDFromContext(c)

	subscription, err := h.subscriptionService.AssignSubscription(c.Request.Context(), &service.AssignSubscriptionInput{
		UserID:              req.UserID,
		GroupID:             req.GroupID,
		ValidityDays:        req.ValidityDays,
		AssignedBy:          adminID,
		Notes:               req.Notes,
		AllowBulkQuotaReset: req.AllowBulkQuotaReset,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserSubscriptionFromServiceAdmin(subscription))
}

// BulkAssign handles bulk assigning subscriptions to multiple users
// POST /api/v1/admin/subscriptions/bulk-assign
func (h *SubscriptionHandler) BulkAssign(c *gin.Context) {
	var req BulkAssignSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Get admin user ID from context
	adminID := getAdminIDFromContext(c)

	result, err := h.subscriptionService.BulkAssignSubscription(c.Request.Context(), &service.BulkAssignSubscriptionInput{
		UserIDs:             req.UserIDs,
		GroupID:             req.GroupID,
		ValidityDays:        req.ValidityDays,
		AssignedBy:          adminID,
		Notes:               req.Notes,
		AllowBulkQuotaReset: req.AllowBulkQuotaReset,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.BulkAssignResultFromService(result))
}

// Extend handles adjusting a subscription (extend or shorten)
// POST /api/v1/admin/subscriptions/:id/extend
func (h *SubscriptionHandler) Extend(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	var req AdjustSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	idempotencyPayload := struct {
		SubscriptionID int64                     `json:"subscription_id"`
		Body           AdjustSubscriptionRequest `json:"body"`
	}{
		SubscriptionID: subscriptionID,
		Body:           req,
	}
	executeAdminIdempotentJSON(c, "admin.subscriptions.extend", idempotencyPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		subscription, execErr := h.subscriptionService.ExtendSubscription(ctx, subscriptionID, req.Days)
		if execErr != nil {
			return nil, execErr
		}
		return dto.UserSubscriptionFromServiceAdmin(subscription), nil
	})
}

// ResetSubscriptionQuotaRequest represents the reset quota request
type ResetSubscriptionQuotaRequest struct {
	Daily   bool `json:"daily"`
	Weekly  bool `json:"weekly"`
	Monthly bool `json:"monthly"`
}

// ResetQuota resets daily, weekly, and/or monthly usage for a subscription.
// POST /api/v1/admin/subscriptions/:id/reset-quota
func (h *SubscriptionHandler) ResetQuota(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	var req ResetSubscriptionQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !req.Daily && !req.Weekly && !req.Monthly {
		response.BadRequest(c, "At least one of 'daily', 'weekly', or 'monthly' must be true")
		return
	}
	resetter := h.quotaResetter
	if resetter == nil {
		resetter = h.subscriptionService
	}
	idempotencyKey, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if idempotencyKey == "" {
		sub, err := resetter.AdminResetQuota(c.Request.Context(), subscriptionID, req.Daily, req.Weekly, req.Monthly)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, dto.UserSubscriptionFromServiceAdmin(sub))
		return
	}
	idempotencyPayload := struct {
		SubscriptionID int64                         `json:"subscription_id"`
		Body           ResetSubscriptionQuotaRequest `json:"body"`
	}{SubscriptionID: subscriptionID, Body: req}
	ttl := service.DefaultWriteIdempotencyTTL()
	claimedAt := time.Now()
	fallbackExecution, err := idempotencyexecution.New(subscriptionResetQuotaScope, adminActorScope(c), service.HashIdempotencyKey(idempotencyKey), claimedAt, claimedAt.Add(ttl))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	executeAdminIdempotentJSON(c, subscriptionResetQuotaScope, idempotencyPayload, ttl, func(ctx context.Context) (any, error) {
		execution, ok := idempotencyexecution.FromContext(ctx)
		if !ok {
			execution = fallbackExecution
		}
		sub, execErr := resetter.AdminResetQuotaIdempotent(ctx, subscriptionID, req.Daily, req.Weekly, req.Monthly, execution)
		if execErr != nil {
			return nil, execErr
		}
		return dto.UserSubscriptionFromServiceAdmin(sub), nil
	})
}

// ListBulkResetQuotaCandidates lists active subscriptions whose effective
// payment plan or manual cycle allows bulk quota reset.
// GET /api/v1/admin/subscriptions/bulk-reset-quota/candidates
func (h *SubscriptionHandler) ListBulkResetQuotaCandidates(c *gin.Context) {
	result, err := h.bulkResetService.ListCandidates(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type BulkResetSubscriptionQuotaRequest struct {
	SubscriptionIDs []int64 `json:"subscription_ids" binding:"required,min=1"`
}

// BulkResetQuota resets all quota windows for the selected eligible subscriptions.
// POST /api/v1/admin/subscriptions/bulk-reset-quota
func (h *SubscriptionHandler) BulkResetQuota(c *gin.Context) {
	var req BulkResetSubscriptionQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.SubscriptionIDs) > subscriptionbulkreset.MaxBatchSize {
		response.BadRequest(c, "subscription_ids must not contain more than "+strconv.Itoa(subscriptionbulkreset.MaxBatchSize)+" items")
		return
	}
	for _, subscriptionID := range req.SubscriptionIDs {
		if subscriptionID <= 0 {
			response.BadRequest(c, "subscription_ids must contain only positive IDs")
			return
		}
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
	fallbackExecution, err := idempotencyexecution.New(subscriptionBulkResetQuotaScope, adminActorScope(c), service.HashIdempotencyKey(idempotencyKey), claimedAt, claimedAt.Add(ttl))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	executeAdminIdempotentJSON(c, subscriptionBulkResetQuotaScope, req, ttl, func(ctx context.Context) (any, error) {
		execution, ok := idempotencyexecution.FromContext(ctx)
		if !ok {
			execution = fallbackExecution
		}
		return h.bulkResetService.ResetSelected(ctx, req.SubscriptionIDs, execution)
	})
}

type UpdateCurrentCycleBulkResetEligibilityRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// UpdateCurrentCycleBulkResetEligibility changes per-cycle eligibility for an
// active assignment or legacy subscription cycle.
// PUT /api/v1/admin/subscriptions/:id/current-cycle-bulk-reset-eligibility
func (h *SubscriptionHandler) UpdateCurrentCycleBulkResetEligibility(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	var req UpdateCurrentCycleBulkResetEligibilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.bulkResetService.UpdateCurrentCycleManualEligibility(c.Request.Context(), subscriptionID, *req.Enabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subscription, err := h.subscriptionService.GetByID(c.Request.Context(), subscriptionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserSubscriptionFromServiceAdmin(subscription))
}

// Revoke handles revoking a subscription.
// POST /api/v1/admin/subscriptions/:id/revoke
// DELETE /api/v1/admin/subscriptions/:id is kept for backward compatibility.
func (h *SubscriptionHandler) Revoke(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	err = h.subscriptionService.RevokeSubscription(c.Request.Context(), subscriptionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Subscription revoked successfully"})
}

// Restore handles restoring a revoked subscription.
// POST /api/v1/admin/subscriptions/:id/restore
func (h *SubscriptionHandler) Restore(c *gin.Context) {
	subscriptionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}

	subscription, err := h.subscriptionService.RestoreSubscription(c.Request.Context(), subscriptionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.UserSubscriptionFromServiceAdmin(subscription))
}

// ListByGroup handles listing subscriptions for a specific group
// GET /api/v1/admin/groups/:id/subscriptions
func (h *SubscriptionHandler) ListByGroup(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid group ID")
		return
	}

	page, pageSize := response.ParsePagination(c)

	subscriptions, pagination, err := h.subscriptionService.ListGroupSubscriptions(c.Request.Context(), groupID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminUserSubscription, 0, len(subscriptions))
	for i := range subscriptions {
		out = append(out, *dto.UserSubscriptionFromServiceAdmin(&subscriptions[i]))
	}
	response.PaginatedWithResult(c, out, toResponsePagination(pagination))
}

// ListByUser handles listing subscriptions for a specific user
// GET /api/v1/admin/users/:id/subscriptions
func (h *SubscriptionHandler) ListByUser(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	subscriptions, err := h.subscriptionService.ListUserSubscriptions(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminUserSubscription, 0, len(subscriptions))
	for i := range subscriptions {
		out = append(out, *dto.UserSubscriptionFromServiceAdmin(&subscriptions[i]))
	}
	response.Success(c, out)
}

// Helper function to get admin ID from context
func getAdminIDFromContext(c *gin.Context) int64 {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}
