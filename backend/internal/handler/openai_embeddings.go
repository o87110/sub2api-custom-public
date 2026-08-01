package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Embeddings handles the OpenAI-compatible Embeddings API.
// POST /v1/embeddings
func (h *OpenAIGatewayHandler) Embeddings(c *gin.Context) {
	streamStarted := false
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.embeddings",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	groupRoute := h.newAPIKeyGroupRoute(
		c.Request.Context(), apiKey, groupRoutingProtocolOpenAIEmbeddings, "", false)
	defer groupRoute.finish(c)
	groupRoute.configureCompositeRequest(&reqModel, &body, nil)
	groupRoute.setCandidateCheck(func(candidateContext *gin.Context, candidate *service.APIKey) error {
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
		var routeErr error
		apiKey, subscription, routeErr = groupRoute.nextCandidate(c)
		if routeErr != nil {
			status, code, message, retryAfter := billingErrorDetails(routeErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}
	} else {
		subscription, _ = middleware2.GetSubscriptionFromContext(c)
	}
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformAllowed(c, apiKey, reqModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel))
	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_embeddings", reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if !groupRoute.MultiGroup() {
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			reqLog.Info("openai_embeddings.billing_check_failed", zap.Error(err))
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}
	}

	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	switchCount := 0
	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	routingStart := time.Now()

	for {
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			"",
			reqModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			service.OpenAIEndpointCapabilityEmbeddings,
			false,
			false,
			true,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_embeddings.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_embeddings.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if groupRoute.MultiGroup() {
				groupRoute.markCurrentUnavailable(c.Request.Context())
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
					continue
				}
			}
			if len(failedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, service.PlatformOpenAI)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
			}
			return
		}
		if selection == nil || selection.Account == nil {
			if groupRoute.MultiGroup() {
				groupRoute.markCurrentUnavailable(c.Request.Context())
				nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
				if nextErr == nil {
					apiKey, subscription = nextKey, nextSub
					channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
					continue
				}
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, service.PlatformOpenAI)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, accountSlotErr := h.tryAcquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &streamStarted, reqLog)
		if accountSlotErr != nil {
			if groupRoute.MultiGroup() && isAccountCapacityUnavailable(accountSlotErr) {
				switch accountCapacityFailoverAction(c.Request.Context(), failedAccountIDs, account.ID, &switchCount, maxAccountSwitches) {
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
					channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
					continue
				}
			}
			h.handleResponsesAccountSlotError(c, accountSlotErr, streamStarted)
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
					channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
					continue
				}
			}
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardEmbeddings(c.Request.Context(), c, account, forwardBody, "")
		}()

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), false, nil)
				if failoverClientGone(c) {
					reqLog.Info("openai_embeddings.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					if groupRoute.MultiGroup() && apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) {
						groupRoute.markCurrentUnavailable(c.Request.Context())
						nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
						if nextErr == nil {
							apiKey, subscription = nextKey, nextSub
							channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
							failedAccountIDs = make(map[int64]struct{})
							sameAccountRetryCount = make(map[int64]int)
							switchCount = 0
							lastFailoverErr = nil
							oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
							continue
						}
					}
					h.handleFailoverExhausted(c, failoverErr, false)
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
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					if groupRoute.MultiGroup() && apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) {
						groupRoute.markCurrentUnavailable(c.Request.Context())
						nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
						if nextErr == nil {
							apiKey, subscription = nextKey, nextSub
							channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
							failedAccountIDs = make(map[int64]struct{})
							sameAccountRetryCount = make(map[int64]int)
							switchCount = 0
							lastFailoverErr = nil
							oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
							continue
						}
					}
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				if h.gatewayService.ShouldStopOpenAIOAuth429Failover(
					account, failoverErr.StatusCode, switchCount, &oauth429FailoverState,
				) {
					if groupRoute.MultiGroup() && apikeyrouting.ShouldCrossGroup(c.Request.Context(), failoverErr, false) {
						groupRoute.markCurrentUnavailable(c.Request.Context())
						nextKey, nextSub, nextErr := groupRoute.nextCandidate(c)
						if nextErr == nil {
							apiKey, subscription = nextKey, nextSub
							channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
							failedAccountIDs = make(map[int64]struct{})
							sameAccountRetryCount = make(map[int64]int)
							switchCount = 0
							lastFailoverErr = nil
							oauth429FailoverState = service.OpenAIOAuth429FailoverState{}
							continue
						}
					}
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				reqLog.Warn("openai_embeddings.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), false, nil)
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_embeddings.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), true, nil)
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)

		h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result:             result,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				APIKeyService:      h.apiKeyService,
				QuotaPlatform:      quotaPlatform,
				SessionID:          sessionID,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.openai_gateway.embeddings"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("openai_embeddings.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("openai_embeddings.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}
