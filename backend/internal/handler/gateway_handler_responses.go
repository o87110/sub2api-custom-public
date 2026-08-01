package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Responses handles OpenAI Responses API endpoint for Anthropic platform groups.
// POST /v1/responses
// This converts Responses API requests to Anthropic format, forwards to Anthropic
// upstream, and converts responses back to Responses format.
func (h *GatewayHandler) Responses(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// Read request body
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.responsesErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream using gjson (like OpenAI handler)
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	requestCtx := c.Request.Context()
	imageIntent := service.IsImageGenerationIntentForPlatform(
		"/v1/responses", reqModel, body, openAICompatibleRequestPlatform(c.Request.Context(), apiKey))
	if imageIntent {
		requestCtx = service.WithOpenAIImageGenerationIntent(requestCtx)
		c.Request = c.Request.WithContext(requestCtx)
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "responses")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	if failoverClientGone(c) {
		return
	}
	groupRoute := h.newAPIKeyGroupRoute(
		requestCtx, apiKey, groupRoutingProtocolOpenAIResponses, sessionHash, true)
	defer groupRoute.finish(c)
	groupRoute.preferResponseChain(
		requestCtx,
		apiKey,
		gjson.GetBytes(body, "previous_response_id").String(),
	)
	groupRoute.configureCompositeRequest(&reqModel, &body, nil)
	groupRoute.setCandidateCheck(func(candidateContext *gin.Context, candidate *service.APIKey) error {
		if !gatewayResponsesCompositeTargetAllowed(candidateContext, candidate, reqModel) {
			return infraerrors.BadRequest(
				"COMPOSITE_MODEL_UNSUPPORTED",
				"Model is not supported by this gateway handler for composite groups",
			)
		}
		if candidate.Group != nil && candidate.Group.ClaudeCodeOnly {
			return infraerrors.Forbidden(
				"CLAUDE_CODE_ONLY",
				"This group is restricted to Claude Code clients (/v1/messages only)",
			)
		}
		if imageIntent && !service.GroupAllowsImageGeneration(candidate.Group) {
			return infraerrors.Forbidden(
				"IMAGE_GENERATION_DISABLED",
				service.ImageGenerationPermissionMessage(),
			)
		}
		return nil
	})
	var subscription *service.UserSubscription
	if groupRoute.MultiGroup() {
		var routeErr error
		apiKey, subscription, routeErr = groupRoute.nextCandidate(c)
		if routeErr != nil {
			status, code, message, retryAfter := billingErrorDetails(routeErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return
		}
		requestCtx = c.Request.Context()
	} else {
		subscription, _ = middleware2.GetSubscriptionFromContext(c)
	}
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !gatewayResponsesCompositeTargetAllowed(c, apiKey, reqModel) {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this gateway handler for composite groups")
		return
	}
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(requestCtx, apiKey.GroupID, reqModel)

	// Claude Code only restriction:
	// /v1/responses is never a Claude Code endpoint.
	// When claude_code_only is enabled, this endpoint is rejected.
	// The existing service-layer checkClaudeCodeRestriction handles degradation
	// to fallback groups when the Forward path calls SelectAccountForModelWithExclusions.
	// Here we just reject at handler level since /v1/responses clients can't be Claude Code.
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly {
		h.responsesErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.responsesSecurityAuditError(c, decision)
		groupRoute.keepCurrentBinding(c.Request.Context())
		return
	}

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.responses.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if !groupRoute.MultiGroup() {
		if err := h.billingCacheService.CheckBillingEligibility(requestCtx, apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(requestCtx, apiKey)); err != nil {
			reqLog.Info("gateway.responses.billing_check_failed", zap.Error(err))
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return
		}
	}

	// 3. Account selection + failover loop
	var fs *FailoverState
	refreshCandidateRouting := func() {
		singleAccountRetry := h.gatewayService.IsSingleAntigravityAccountGroup(
			c.Request.Context(), apiKey.GroupID)
		c.Request = c.Request.WithContext(service.WithSingleAccountRetry(
			c.Request.Context(), singleAccountRetry, h.metadataBridgeEnabled()))
		requestCtx = c.Request.Context()
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(
			requestCtx, apiKey.GroupID, reqModel)
		fs = NewFailoverState(h.maxAccountSwitches, false)
	}
	refreshCandidateRouting()

	for {
		if requestCtx.Err() != nil {
			return
		}
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(requestCtx, apiKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, "", int64(0))
		if err != nil {
			action := selectionExhaustedAction(requestCtx, fs)
			if action == FailoverContinue {
				continue
			}
			if action == FailoverCanceled {
				failoverClientGone(c)
				return
			}
			if groupRoute.MultiGroup() {
				groupRoute.markCurrentUnavailable(requestCtx)
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					refreshCandidateRouting()
					continue
				}
			}
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, effectiveAPIKeyPlatform(c, apiKey))
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available accounts: " + err.Error()
				}
				h.responsesErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			if fs.LastFailoverErr != nil {
				h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
			} else {
				h.responsesErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
			}
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 4. Acquire account concurrency slot without committing an error before
		// all configured groups have been considered.
		accountReleaseFunc, accountSlotErr := h.concurrencyHelper.acquireSelectedAccountSlot(
			c, selection, reqStream, &streamStarted)
		if accountSlotErr != nil {
			reqLog.Warn("gateway.responses.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(accountSlotErr))
			if isAccountCapacityUnavailable(accountSlotErr) && groupRoute.MultiGroup() {
				switch fs.HandleAccountCapacityUnavailable(requestCtx, account.ID) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
				groupRoute.markCurrentUnavailable(requestCtx)
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					refreshCandidateRouting()
					continue
				}
			}
			h.handleConcurrencyError(c, accountSlotErr, "account", streamStarted)
			return
		}

		if err := groupRoute.reserveCurrent(requestCtx); err != nil {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			if reservationCanTryNextGroup(err) && groupRoute.MultiGroup() {
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					refreshCandidateRouting()
					continue
				}
			}
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return
		}

		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var result *service.ForwardResult
		setActualUpstreamEndpoint(c, "")
		if shouldUseAntigravityCompat(account) {
			if h.antigravityGatewayService == nil {
				h.responsesErrorResponse(c, http.StatusBadGateway, "upstream_error", "Antigravity compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
			result, err = h.antigravityGatewayService.ForwardAsResponses(requestCtx, c, account, forwardBody, parsedReq)
		} else {
			result, err = h.gatewayService.ForwardAsResponses(requestCtx, c, account, forwardBody, parsedReq)
		}

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				// Can't failover if streaming content already sent
				if c.Writer.Size() != writerSizeBeforeForward {
					groupRoute.settleTerminalError(requestCtx, failoverErr)
					h.handleResponsesFailoverExhausted(c, failoverErr, true)
					return
				}
				action := fs.HandleFailoverError(requestCtx, h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					crossGroup := apikeyrouting.ShouldCrossGroup(requestCtx, failoverErr, false)
					if groupRoute.MultiGroup() && crossGroup {
						groupRoute.markCurrentUnavailable(requestCtx)
						nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
						if nextErr == nil {
							apiKey, subscription = nextKey, nextSub
							refreshCandidateRouting()
							continue
						}
					}
					groupRoute.settleTerminalError(requestCtx, failoverErr)
					h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					return
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
			}
			upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Error("gateway.responses.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			groupRoute.keepCurrentBinding(requestCtx)
			return
		}

		// 6. Record usage
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				SessionID:          sessionID,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			}); err != nil {
				reqLog.Error("gateway.responses.record_usage_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
			}
		})
		groupRoute.bindResponseChain(requestCtx, result.ResponseID)
		groupRoute.keepCurrentBinding(requestCtx)
		return
	}
}

// responsesErrorResponse writes an error in OpenAI Responses API format.
func (h *GatewayHandler) responsesErrorResponse(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

// handleResponsesFailoverExhausted writes a failover-exhausted error in Responses format.
func (h *GatewayHandler) handleResponsesFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return // Can't write error after stream started
	}
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(lastErr)
		h.responsesErrorResponse(c, status, "server_error", message)
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.responsesErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.responsesErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
