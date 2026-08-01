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

// ChatCompletions handles OpenAI Chat Completions API endpoint for Anthropic platform groups.
// POST /v1/chat/completions
// This converts Chat Completions requests to Anthropic format (via Responses format chain),
// forwards to Anthropic upstream, and converts responses back to Chat Completions format.
func (h *GatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// Read request body
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.chatCompletionsErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "chat_completions")
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
		c.Request.Context(), apiKey, groupRoutingProtocolOpenAIChat, sessionHash, true)
	defer groupRoute.finish(c)
	groupRoute.configureCompositeRequest(&reqModel, &body, nil)
	groupRoute.setCandidateCheck(func(candidateContext *gin.Context, candidate *service.APIKey) error {
		if !gatewayChatCompositeTargetAllowed(candidateContext, candidate, reqModel) {
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
			h.chatCompletionsErrorResponse(c, status, code, message)
			return
		}
	} else {
		subscription, _ = middleware2.GetSubscriptionFromContext(c)
	}

	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !gatewayChatCompositeTargetAllowed(c, apiKey, reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this gateway handler for composite groups")
		return
	}
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// Claude Code only restriction
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly {
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
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
		reqLog.Warn("gateway.cc.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if !groupRoute.MultiGroup() {
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			reqLog.Info("gateway.cc.billing_check_failed", zap.Error(err))
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.chatCompletionsErrorResponse(c, status, code, message)
			return
		}
	}
	var groupPlatform string
	var selectionSessionHash string
	var fs *FailoverState
	refreshCandidateRouting := func() {
		groupPlatform = effectiveAPIKeyPlatform(c, apiKey)
		selectionSessionHash = sessionHash
		if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
			selectionSessionHash = "gemini:" + selectionSessionHash
		}
		maxSwitches := h.maxAccountSwitches
		if groupPlatform == service.PlatformGemini {
			maxSwitches = h.maxAccountSwitchesGemini
		}
		fs = NewFailoverState(maxSwitches, false)
		singleAccountRetry := h.gatewayService.IsSingleAntigravityAccountGroup(
			c.Request.Context(), apiKey.GroupID)
		c.Request = c.Request.WithContext(service.WithSingleAccountRetry(
			c.Request.Context(), singleAccountRetry, h.metadataBridgeEnabled()))
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(
			c.Request.Context(), apiKey.GroupID, reqModel)
	}
	refreshCandidateRouting()

	// 3. Account selection + failover loop

	for {
		if c.Request.Context().Err() != nil {
			return
		}
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, selectionSessionHash, reqModel, fs.FailedAccountIDs, "", int64(0))
		if err != nil {
			action := selectionExhaustedAction(c.Request.Context(), fs)
			if action == FailoverContinue {
				continue
			}
			if action == FailoverCanceled {
				failoverClientGone(c)
				return
			}
			if groupRoute.MultiGroup() {
				groupRoute.markCurrentUnavailable(c.Request.Context())
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					refreshCandidateRouting()
					continue
				}
			}
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, groupPlatform)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available accounts: " + err.Error()
				}
				h.chatCompletionsErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			if fs.LastFailoverErr != nil {
				h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
			} else {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
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
			reqLog.Warn("gateway.cc.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(accountSlotErr))
			if isAccountCapacityUnavailable(accountSlotErr) && groupRoute.MultiGroup() {
				switch fs.HandleAccountCapacityUnavailable(c.Request.Context(), account.ID) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
				groupRoute.markCurrentUnavailable(c.Request.Context())
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

		if err := groupRoute.reserveCurrent(c.Request.Context()); err != nil {
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
			h.chatCompletionsErrorResponse(c, status, code, message)
			return
		}

		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}

		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var result *service.ForwardResult
		setActualUpstreamEndpoint(c, "")
		if account.Platform == service.PlatformGemini {
			if h.geminiCompatService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Gemini compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			result, err = h.geminiCompatService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody)
		} else if shouldUseAntigravityCompat(account) {
			if h.antigravityGatewayService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Antigravity compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
			result, err = h.antigravityGatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		} else {
			result, err = h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		}

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					h.handleCCFailoverExhausted(c, failoverErr, true)
					return
				}
				action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					crossGroup := apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false)
					if groupRoute.MultiGroup() && crossGroup {
						groupRoute.markCurrentUnavailable(c.Request.Context())
						nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
						if nextErr == nil {
							apiKey, subscription = nextKey, nextSub
							refreshCandidateRouting()
							continue
						}
					}
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
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
			reqLog.Error("gateway.cc.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			groupRoute.keepCurrentBinding(c.Request.Context())
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
				reqLog.Error("gateway.cc.record_usage_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
			}
		})
		groupRoute.keepCurrentBinding(c.Request.Context())
		return
	}
}

// chatCompletionsErrorResponse writes an error in OpenAI Chat Completions format.
func (h *GatewayHandler) chatCompletionsErrorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// handleCCFailoverExhausted writes a failover-exhausted error in CC format.
func (h *GatewayHandler) handleCCFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return
	}
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(lastErr)
		h.chatCompletionsErrorResponse(c, status, "server_error", message)
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.chatCompletionsErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
