package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GrokCountTokens handles Anthropic-compatible count_tokens requests locally.
// The route middleware already authenticates the API key and resolves the
// group; this handler intentionally does not select an account or check billing.
func (h *OpenAIGatewayHandler) GrokCountTokens(c *gin.Context) {
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(requestLogger(c, "handler.openai_gateway.grok_count_tokens"), body, err)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	if parsedReq.Model == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	estimated, err := service.EstimateGrokCountTokens(parsedReq.Body.Bytes())
	if err != nil {
		requestLogger(c, "handler.openai_gateway.grok_count_tokens").Warn("grok_count_tokens.local_estimate_failed", zap.Error(err))
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	setOpsRequestContext(c, parsedReq.Model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))
	c.JSON(http.StatusOK, gin.H{"input_tokens": estimated})
}

// CountTokens handles Anthropic-compatible POST /v1/messages/count_tokens for OpenAI groups.
// It validates billing and routes to an OpenAI token-count bridge without taking concurrency slots
// or recording usage.
func (h *OpenAIGatewayHandler) CountTokens(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.count_tokens",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	body = parsedReq.Body.Bytes()
	if parsedReq.Model == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	reqModel := parsedReq.Model
	groupRoute := h.newAPIKeyGroupRoute(
		c.Request.Context(), apiKey, groupRoutingProtocolCountTokens, "", false)
	defer groupRoute.finish(c)
	groupRoute.configureCompositeRequest(&reqModel, &body, func(model string, updatedBody []byte) {
		parsedReq.Model = model
		if len(updatedBody) > 0 {
			_ = parsedReq.ReplaceBody(updatedBody)
		}
	})
	groupRoute.setCandidateCheck(func(candidateContext *gin.Context, candidate *service.APIKey) error {
		if candidate.Group != nil && !candidate.Group.AllowMessagesDispatch {
			return infraerrors.Forbidden(
				"MESSAGES_DISPATCH_DISABLED",
				"This group does not allow /v1/messages dispatch",
			)
		}
		if compositeTargetPlatformAllowed(
			candidateContext, candidate, reqModel, service.PlatformOpenAI) {
			return nil
		}
		return infraerrors.BadRequest(
			"COMPOSITE_MODEL_UNSUPPORTED",
			"Model is not supported by this OpenAI-compatible endpoint for composite groups",
		)
	})
	var subscription *service.UserSubscription
	if groupRoute.MultiGroup() {
		for {
			var routeErr error
			apiKey, subscription, routeErr = groupRoute.nextCandidate(c)
			if routeErr != nil {
				status, code, message, retryAfter := billingErrorDetails(routeErr)
				if retryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(retryAfter))
				}
				h.anthropicErrorResponse(c, status, code, message)
				return
			}
			if apiKey.Group == nil || apiKey.Group.AllowMessagesDispatch {
				break
			}
			groupRoute.markCurrentUnavailable(c.Request.Context())
		}
	} else {
		if apiKey.Group != nil && !apiKey.Group.AllowMessagesDispatch {
			h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
				"This group does not allow /v1/messages dispatch")
			return
		}
		subscription, _ = middleware2.GetSubscriptionFromContext(c)
	}
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformAllowed(c, apiKey, reqModel, service.PlatformOpenAI) {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	preferredMappedModel := resolveOpenAIMessagesDispatchMappedModel(apiKey, reqModel)
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", parsedReq.Stream))

	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(false, false)))

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	mappedBodyForMessages := newOpenAIModelMappedBodyCache(body, h.gatewayService.ReplaceModelInBody)
	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	switchCount := 0
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	advanceGroup := func() bool {
		if !groupRoute.MultiGroup() {
			return false
		}
		groupRoute.markCurrentUnavailable(c.Request.Context())
		for {
			nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
			if nextErr != nil {
				return false
			}
			if nextKey.Group != nil && !nextKey.Group.AllowMessagesDispatch {
				groupRoute.markCurrentUnavailable(c.Request.Context())
				continue
			}
			apiKey, subscription = nextKey, nextSub
			channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
			preferredMappedModel = resolveOpenAIMessagesDispatchMappedModel(apiKey, reqModel)
			failedAccountIDs = make(map[int64]struct{})
			sameAccountRetryCount = make(map[int64]int)
			switchCount = 0
			lastFailoverErr = nil
			oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
			return true
		}
	}

	if !groupRoute.MultiGroup() {
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			reqLog.Info("openai_count_tokens.billing_eligibility_check_failed", zap.Error(err))
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.anthropicErrorResponse(c, status, code, message)
			return
		}
	}

	requestStart := time.Now()
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	for {
		// reqModel is reset and resolved again for every composite candidate by
		// nextCandidate. Do not retain the first group's resolved model here.
		currentRoutingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
		if preferredMappedModel != "" {
			currentRoutingModel = preferredMappedModel
		}
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			currentRoutingModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			false,
			openAICompatibleRequestPlatform(c.Request.Context(), apiKey),
		)
		service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
		if err != nil || selection == nil || selection.Account == nil {
			if failoverClientGone(c) {
				return
			}
			if advanceGroup() {
				continue
			}
			if groupRoute.MultiGroup() && lastFailoverErr != nil {
				h.handleAnthropicFailoverExhausted(c, lastFailoverErr, false)
				return
			}
			requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
			reqLog.Warn("openai_count_tokens.account_select_failed", zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)))
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, currentRoutingModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			}
			h.anthropicErrorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}

		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		if err := groupRoute.reserveCurrent(c.Request.Context()); err != nil {
			if selection.Acquired && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if reservationCanTryNextGroup(err) && groupRoute.MultiGroup() {
				if advanceGroup() {
					continue
				}
			}
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.anthropicErrorResponse(c, status, code, message)
			return
		}
		forwardBody := mappedBodyForMessages(channelMapping.Mapped, channelMapping.MappedModel)
		defaultMappedModel := preferredMappedModel

		writerSizeBeforeForward := c.Writer.Size()
		forwardErr := h.gatewayService.ForwardCountTokensAsAnthropic(c.Request.Context(), c, account, forwardBody, defaultMappedModel)
		if selection.Acquired && selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if forwardErr != nil {
			var failoverErr *service.UpstreamFailoverError
			if groupRoute.MultiGroup() && errors.As(forwardErr, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					return
				}
				if failoverClientGone(c) {
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					if apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) && advanceGroup() {
						continue
					}
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					h.handleAnthropicFailoverExhausted(c, failoverErr, false)
					return
				}
				if failoverErr.RetryableOnSameAccount {
					retryLimit := account.GetPoolModeRetryCount()
					if sameAccountRetryCount[account.ID] < retryLimit {
						sameAccountRetryCount[account.ID]++
						if !sleepWithContext(c.Request.Context(), sameAccountRetryDelay) {
							return
						}
						continue
					}
				}
				failedAccountIDs[account.ID] = struct{}{}
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					if apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) && advanceGroup() {
						continue
					}
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					h.handleAnthropicFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				if h.gatewayService.ShouldStopOpenAIOAuth429Failover(
					account, failoverErr.StatusCode, switchCount, &oauth429FailoverState,
				) {
					if apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) && advanceGroup() {
						continue
					}
					groupRoute.settleTerminalError(c.Request.Context(), failoverErr)
					h.handleAnthropicFailoverExhausted(c, failoverErr, false)
					return
				}
				continue
			}
			reqLog.Error("openai_count_tokens.forward_failed", zap.Int64("account_id", account.ID), zap.Error(forwardErr))
		}
		return
	}
}
