package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type BatchImageHandler struct {
	service  *service.BatchImagePublicService
	download *service.BatchImageDownloadService
	cleanup  *service.BatchImageCleanupService
	openAI   *OpenAIGatewayHandler
}

func NewBatchImageHandler(service *service.BatchImagePublicService, download *service.BatchImageDownloadService, cleanup *service.BatchImageCleanupService) *BatchImageHandler {
	return &BatchImageHandler{service: service, download: download, cleanup: cleanup}
}

func (h *BatchImageHandler) Submit(c *gin.Context) {
	var req service.BatchImageSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		batchImageError(c, service.ErrBatchImageInvalidItems)
		return
	}
	apiKey, hasAPIKey := middleware.GetAPIKeyFromContext(c)
	if !hasAPIKey || apiKey == nil {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	multiGroupRouting := len(apiKey.GroupIDs) > 1 && h.openAI != nil
	if !multiGroupRouting && !h.checkSecurityAuditBeforeSubmit(c, &req) {
		return
	}
	if sessionID := service.ExtractClientSessionID(c); sessionID != "" {
		req.SessionID = &sessionID
	}
	validated, err := h.service.ValidateSubmitRequest(req)
	if err != nil {
		batchImageError(c, err)
		return
	}
	req = validated

	if !multiGroupRouting {
		owner, ok := batchImageOwnerFromContext(c)
		if !ok {
			batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
			return
		}
		got, submitErr := h.service.Submit(c.Request.Context(), owner, req, c.GetHeader("Idempotency-Key"))
		if submitErr != nil {
			batchImageError(c, submitErr)
			return
		}
		c.JSON(http.StatusOK, got)
		return
	}

	groupRoute := h.openAI.newAPIKeyGroupRoute(
		c.Request.Context(), apiKey, groupRoutingProtocolBatchImages, "", false)
	defer groupRoute.finish(c)
	groupRoute.setCandidateCheck(func(candidateCtx *gin.Context, _ *service.APIKey) error {
		return h.batchImageCandidateSupportsRequest(candidateCtx, req)
	})
	securityAuditChecked := false
	for {
		_, _, routeErr := groupRoute.nextCandidate(c)
		if routeErr != nil {
			batchImageError(c, routeErr)
			return
		}
		if !securityAuditChecked {
			// Content moderation is request-scoped and must run once, but its
			// group targeting must observe the first actually eligible candidate
			// rather than the API key's compatibility primary group.
			if !h.checkSecurityAuditBeforeSubmit(c, &req) {
				return
			}
			securityAuditChecked = true
		}
		if reserveErr := groupRoute.reserveCurrent(c.Request.Context()); reserveErr != nil {
			if reservationCanTryNextGroup(reserveErr) {
				groupRoute.markCurrentUnavailable(c.Request.Context())
				continue
			}
			batchImageError(c, reserveErr)
			return
		}
		owner, ok := batchImageOwnerFromContext(c)
		if !ok {
			batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
			return
		}
		got, submitErr := h.service.Submit(c.Request.Context(), owner, req, c.GetHeader("Idempotency-Key"))
		if submitErr == nil {
			c.JSON(http.StatusOK, got)
			return
		}
		if apikeyrouting.ShouldCrossBatchImageGroup(c.Request.Context(), submitErr) {
			groupRoute.markCurrentUnavailable(c.Request.Context())
			continue
		}
		batchImageError(c, submitErr)
		return
	}
}

func (h *BatchImageHandler) batchImageCandidateSupportsRequest(c *gin.Context, req service.BatchImageSubmitRequest) error {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		return service.ErrGroupNotFound
	}
	models, err := h.service.ListModels(c.Request.Context(), owner)
	if err != nil {
		return err
	}
	for _, model := range models.Data {
		if model.ID != req.Model {
			continue
		}
		if req.Provider == "" || strings.EqualFold(model.Provider, req.Provider) {
			return nil
		}
	}
	return service.ErrBatchImageNoAccountAvailable
}

func (h *BatchImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, req *service.BatchImageSubmitRequest) bool {
	if h == nil || h.openAI == nil || req == nil {
		return true
	}
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusInternalServerError, "USER_CONTEXT_REQUIRED", "User context not found"))
		return false
	}
	items := make([]map[string]string, 0, len(req.Items))
	for _, item := range req.Items {
		if prompt := strings.TrimSpace(item.Prompt); prompt != "" {
			items = append(items, map[string]string{"prompt": prompt})
		}
	}
	if len(items) == 0 {
		return true
	}
	body, err := json.Marshal(map[string]any{"request": map[string]any{"items": items}})
	if err != nil {
		batchImageError(c, infraerrors.New(http.StatusBadRequest, "INVALID_BATCH_PROMPT", "batch prompts are invalid"))
		return false
	}
	reqLog := requestLogger(c, "handler.batch_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", req.Model))
	decision := h.openAI.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, req.Model, body)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *BatchImageHandler) Get(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Get(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) List(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.List(c.Request.Context(), owner, service.BatchImageJobsQuery{
		Status:     c.Query("status"),
		TaskName:   c.Query("task_name"),
		Downloaded: c.Query("downloaded"),
		From:       c.Query("from"),
		To:         c.Query("to"),
		Limit:      limit,
		Cursor:     c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Models(c *gin.Context) {
	apiKey, hasAPIKey := middleware.GetAPIKeyFromContext(c)
	if !hasAPIKey || apiKey == nil {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	if len(apiKey.GroupIDs) <= 1 || h.openAI == nil {
		owner, ok := batchImageOwnerFromContext(c)
		if !ok {
			batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
			return
		}
		got, err := h.service.ListModels(c.Request.Context(), owner)
		if err != nil {
			batchImageError(c, err)
			return
		}
		c.JSON(http.StatusOK, got)
		return
	}

	groupRoute := h.openAI.newAPIKeyGroupRoute(
		c.Request.Context(), apiKey, groupRoutingProtocolBatchImages, "", false)
	defer groupRoute.finish(c)
	var empty *service.BatchImagePublicModelsResponse
	var candidateErr error
	for {
		_, _, routeErr := groupRoute.nextCandidate(c)
		if routeErr != nil {
			if empty != nil {
				c.JSON(http.StatusOK, empty)
				return
			}
			if candidateErr != nil {
				batchImageError(c, candidateErr)
				return
			}
			batchImageError(c, routeErr)
			return
		}
		owner, ok := batchImageOwnerFromContext(c)
		if !ok {
			batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
			return
		}
		got, err := h.service.ListModels(c.Request.Context(), owner)
		if err != nil {
			if errors.Is(err, service.ErrBatchImageGroupDisabled) {
				candidateErr = err
				groupRoute.markCurrentUnavailable(c.Request.Context())
				continue
			}
			batchImageError(c, err)
			return
		}
		if len(got.Data) == 0 {
			if empty == nil {
				empty = got
			}
			groupRoute.markCurrentUnavailable(c.Request.Context())
			continue
		}
		c.JSON(http.StatusOK, got)
		return
	}
}

func (h *BatchImageHandler) Items(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.ListItems(c.Request.Context(), owner, c.Param("id"), service.BatchImageItemsQuery{
		Status: c.Query("status"),
		Limit:  limit,
		Cursor: c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Cancel(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Cancel(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) ItemContent(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	imageIndex := 0
	if raw := c.Query("image_index"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			batchImageError(c, service.ErrBatchImageItemImageIndexOutOfRange)
			return
		}
		imageIndex = parsed
	}
	stream, err := h.download.OpenItemContent(c.Request.Context(), owner, c.Param("id"), c.Param("custom_id"), imageIndex)
	if err != nil {
		batchImageError(c, err)
		return
	}
	defer func() { _ = stream.Reader.Close() }()

	c.Header("Content-Type", stream.ContentType)
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(stream.Filename))
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	if stream.ContentLength != nil && *stream.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(*stream.ContentLength, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, stream.Reader); err != nil {
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

// markDownloadedBestEffort 在响应体已写出后标记下载状态；
// 此时无法再向客户端返回错误，失败只能记日志（不能静默丢弃）。
func (h *BatchImageHandler) markDownloadedBestEffort(c *gin.Context, owner service.BatchImageOwner) {
	if err := h.service.MarkDownloaded(c.Request.Context(), owner, c.Param("id")); err != nil {
		logger.L().Warn("batch_image.mark_downloaded_failed",
			zap.String("batch_id", c.Param("id")),
			zap.Error(err),
		)
	}
}

func (h *BatchImageHandler) Download(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	maxItems, _ := strconv.Atoi(c.Query("max_items"))

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(c.Param("id")+".zip"))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	result, err := h.download.StreamZip(c.Request.Context(), owner, c.Param("id"), service.BatchImageZipOptions{
		Status:          c.Query("status"),
		MaxItems:        maxItems,
		IncludeManifest: true,
	}, c.Writer)
	if err != nil {
		if result == nil || !c.Writer.Written() {
			batchImageError(c, err)
		}
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

func (h *BatchImageHandler) DeleteRecord(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	if err := h.service.DeleteRecord(c.Request.Context(), owner, c.Param("id")); err != nil {
		batchImageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *BatchImageHandler) DeleteOutputs(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.cleanup.DeleteOutputsForOwner(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func batchImageOwnerFromContext(c *gin.Context) (service.BatchImageOwner, bool) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.ID <= 0 || apiKey.UserID <= 0 {
		return service.BatchImageOwner{}, false
	}
	return service.BatchImageOwner{
		UserID:   apiKey.UserID,
		APIKeyID: apiKey.ID,
		GroupID:  apiKey.GroupID,
	}, true
}

func batchImageError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if err == nil {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if status == 0 || (status == http.StatusInternalServerError && strings.TrimSpace(code) == "") {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if errors.Is(err, service.ErrBatchImageJobNotFound) {
		status = http.StatusNotFound
		code = "BATCH_IMAGE_NOT_FOUND"
		message = "batch image job not found"
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	})
}
