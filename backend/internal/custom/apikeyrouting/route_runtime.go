package apikeyrouting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

const (
	ProtocolAnthropicMessages = "anthropic_messages"
	ProtocolOpenAIChat        = "openai_chat_completions"
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolOpenAIChain       = "openai_response_chain"
	ProtocolGemini            = "gemini_generate_content"
	ProtocolOpenAIEmbeddings  = "openai_embeddings"
	ProtocolOpenAIImages      = "openai_images"
	ProtocolOpenAIAlphaSearch = "openai_alpha_search"
	ProtocolOpenAILive        = "openai_live"
	ProtocolCountTokens       = "count_tokens"
	ProtocolGrokMedia         = "grok_media"
	ProtocolBatchImages       = "batch_images"
)

type preferredGroupContextKey struct{}

// WithPreferredGroup carries a request-local candidate preference across a
// thin protocol bridge (for example async image pre-audit -> background
// execution). It does not persist a sticky binding and is ignored when the
// group no longer belongs to the API key.
func WithPreferredGroup(ctx context.Context, groupID int64) context.Context {
	if ctx == nil || groupID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, preferredGroupContextKey{}, groupID)
}

func PreferredGroup(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	groupID, _ := ctx.Value(preferredGroupContextKey{}).(int64)
	return groupID
}

type apiKeyGroupRoute struct {
	billing          *service.BillingCacheService
	bindingStore     BindingStore
	state            *State
	apiKeyID         int64
	protocol         string
	sessionHash      string
	sticky           bool
	expectedBound    service.APIKeyGroupBinding
	responseChainID  string
	expectedChain    service.APIKeyGroupBinding
	loadedChain      int64
	source           *service.APIKey
	current          *service.APIKey
	currentSub       *service.UserSubscription
	userRPMClaimed   bool
	keyLimitsChecked bool
	groupRPMClaims   map[int64]struct{}
	unavailable      map[int64]struct{}
	composite        compositeCandidateResolver
	publicModel      string
	compositePath    string
	resetRequest     func()
	applyModel       func(string)
	candidateCheck   func(*gin.Context, *service.APIKey) error
	lastErr          error
	failoverReason   string
	currentSettled   bool
}

// RouteRuntime owns candidate selection, billing reservation and sticky-binding
// state. Gateway handlers only adapt protocol-specific request/response details.
type RouteRuntime interface {
	PreferGroup(groupID int64)
	ConfigureCompositeRequest(model *string, body *[]byte, after func(string, []byte))
	SetCandidateCheck(check func(*gin.Context, *service.APIKey) error)
	MultiGroup() bool
	PreferResponseChain(ctx context.Context, apiKey *service.APIKey, previousResponseID string)
	LoadedChainGroupID() int64
	NextCandidate(c *gin.Context) (*service.APIKey, *service.UserSubscription, error)
	ReserveCurrent(ctx context.Context) error
	MarkCurrentUnavailable(ctx context.Context)
	KeepCurrentBinding(ctx context.Context)
	SettleTerminalError(ctx context.Context, err error)
	BindResponseChain(ctx context.Context, responseID string)
	Finish(ctx context.Context)
}

type compositeCandidateResolver interface {
	ResolveCompositeRouteCandidate(
		ctx context.Context,
		group *service.Group,
		publicModel string,
		endpoint string,
	) (service.CompositeRouteDecision, bool, error)
}

// CandidateError marks a group-specific routing rejection so the HTTP
// bridge can preserve its field-level status/reason without changing legacy
// billing error mappings.
type CandidateError struct {
	err error
}

func (e *CandidateError) Error() string {
	if e == nil || e.err == nil {
		return "API key group candidate rejected"
	}
	return e.err.Error()
}

func (e *CandidateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func MarkCandidateError(err error) error {
	if err == nil {
		return nil
	}
	return &CandidateError{err: err}
}

// ReservationCandidateError marks a last-moment eligibility or group-RPM
// rejection that may continue with the next configured group. Global limits
// (for example user RPM and API-key-owned windows) are returned unchanged and
// must terminate the client request.
type ReservationCandidateError struct {
	err error
}

func (e *ReservationCandidateError) Error() string {
	if e == nil || e.err == nil {
		return "API key group reservation rejected"
	}
	return e.err.Error()
}

func (e *ReservationCandidateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func ReservationCanTryNext(err error) bool {
	var candidateErr *ReservationCandidateError
	return errors.As(err, &candidateErr)
}

func newAPIKeyGroupRoute(
	ctx context.Context,
	billing *service.BillingCacheService,
	bindingStore BindingStore,
	apiKey *service.APIKey,
	protocol, sessionHash string,
	sticky bool,
) *apiKeyGroupRoute {
	state := NewState(apiKey, 0)
	route := &apiKeyGroupRoute{
		billing:        billing,
		bindingStore:   bindingStore,
		apiKeyID:       apiKey.ID,
		protocol:       protocol,
		sessionHash:    sessionHash,
		sticky:         sticky && sessionHash != "" && state.MultiGroup(),
		state:          state,
		source:         apiKey,
		groupRPMClaims: make(map[int64]struct{}),
		unavailable:    make(map[int64]struct{}),
	}
	if resolver, ok := bindingStore.(compositeCandidateResolver); ok {
		route.composite = resolver
	}
	route.publicModel, _ = service.RequestedPublicModelFromContext(ctx)
	route.compositePath, _ = service.CompositeRouteEndpointFromContext(ctx)
	var boundBinding service.APIKeyGroupBinding
	if route.sticky && route.bindingStore != nil {
		var err error
		boundBinding, err = route.bindingStore.LoadGroupBinding(ctx, apiKey.ID, protocol, sessionHash)
		if err != nil {
			RecordRedisFailOpen()
			requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
				zap.Int64("api_key_id", apiKey.ID),
				zap.String("protocol", protocol),
				zap.Error(err))
			boundBinding = service.APIKeyGroupBinding{}
		}
	}
	boundGroupID := boundBinding.GroupID
	route.expectedBound = boundBinding
	if boundGroupID > 0 {
		route.state = NewState(apiKey, boundGroupID)
	}
	if boundGroupID > 0 && route.state.BoundGroupID() > 0 {
		RecordStickyHit()
		requestLoggerFromContext(ctx).Debug("gateway.group_sticky_hit",
			zap.Int64("api_key_id", apiKey.ID),
			zap.String("protocol", protocol),
			zap.String("session_hash", shortSessionFingerprint(sessionHash)),
			zap.Int64("group_id", boundGroupID))
	} else if boundGroupID > 0 {
		route.clearInvalidLoadedBinding(ctx, boundBinding)
	}
	return route
}

func NewRouteRuntime(
	ctx context.Context,
	billing *service.BillingCacheService,
	bindingStore BindingStore,
	apiKey *service.APIKey,
	protocol, sessionHash string,
	sticky bool,
) RouteRuntime {
	return newAPIKeyGroupRoute(ctx, billing, bindingStore, apiKey, protocol, sessionHash, sticky)
}

func (r *apiKeyGroupRoute) PreferGroup(groupID int64) {
	r.preferGroup(groupID)
}

func (r *apiKeyGroupRoute) ConfigureCompositeRequest(
	model *string,
	body *[]byte,
	after func(string, []byte),
) {
	r.configureCompositeRequest(model, body, after)
}

func (r *apiKeyGroupRoute) SetCandidateCheck(check func(*gin.Context, *service.APIKey) error) {
	r.setCandidateCheck(check)
}

func (r *apiKeyGroupRoute) PreferResponseChain(
	ctx context.Context,
	apiKey *service.APIKey,
	previousResponseID string,
) {
	r.preferResponseChain(ctx, apiKey, previousResponseID)
}

func (r *apiKeyGroupRoute) LoadedChainGroupID() int64 {
	if r == nil {
		return 0
	}
	return r.loadedChain
}

func (r *apiKeyGroupRoute) NextCandidate(c *gin.Context) (*service.APIKey, *service.UserSubscription, error) {
	return r.nextCandidate(c)
}

func (r *apiKeyGroupRoute) ReserveCurrent(ctx context.Context) error {
	return r.reserveCurrent(ctx)
}

func (r *apiKeyGroupRoute) MarkCurrentUnavailable(ctx context.Context) {
	r.markCurrentUnavailable(ctx)
}

func (r *apiKeyGroupRoute) KeepCurrentBinding(ctx context.Context) {
	r.keepCurrentBinding(ctx)
}

func (r *apiKeyGroupRoute) SettleTerminalError(ctx context.Context, err error) {
	r.settleTerminalError(ctx, err)
}

func (r *apiKeyGroupRoute) BindResponseChain(ctx context.Context, responseID string) {
	r.bindResponseChain(ctx, responseID)
}

func (r *apiKeyGroupRoute) Finish(ctx context.Context) {
	r.finish(ctx)
}

func (r *apiKeyGroupRoute) preferGroup(groupID int64) {
	if r == nil || !r.MultiGroup() || r.source == nil || groupID <= 0 {
		return
	}
	preferred := NewState(r.source, groupID)
	if preferred.BoundGroupID() > 0 {
		r.state = preferred
	}
}

func (r *apiKeyGroupRoute) configureCompositeRequest(
	model *string,
	body *[]byte,
	after func(string, []byte),
) {
	if r == nil {
		return
	}
	canonicalModel := ""
	if model != nil {
		canonicalModel = *model
	}
	if r.publicModel == "" {
		r.publicModel = canonicalModel
	}
	canonicalBody := []byte(nil)
	if body != nil {
		canonicalBody = append([]byte(nil), (*body)...)
	}
	apply := func(upstreamModel string) {
		upstreamModel = strings.TrimSpace(upstreamModel)
		if upstreamModel == "" {
			upstreamModel = canonicalModel
		}
		if model != nil {
			*model = upstreamModel
		}
		updatedBody := append([]byte(nil), canonicalBody...)
		if body != nil {
			if gjson.ValidBytes(updatedBody) && upstreamModel != "" {
				if rewritten, err := sjson.SetBytes(updatedBody, "model", upstreamModel); err == nil {
					updatedBody = rewritten
				}
			}
			*body = updatedBody
		}
		if after != nil {
			after(upstreamModel, updatedBody)
		}
	}
	r.resetRequest = func() {
		apply(canonicalModel)
	}
	r.applyModel = func(upstreamModel string) {
		apply(upstreamModel)
	}
}

// setCandidateCheck 注册分组相关但不产生副作用的入口资格检查。它在每个
// 候选完成 composite 解析后、订阅/RPM 探测前执行，因此切组不会沿用首组能力。
func (r *apiKeyGroupRoute) setCandidateCheck(check func(*gin.Context, *service.APIKey) error) {
	if r != nil {
		r.candidateCheck = check
	}
}

func (r *apiKeyGroupRoute) clearInvalidLoadedBinding(ctx context.Context, binding service.APIKeyGroupBinding) {
	if r == nil || r.bindingStore == nil || binding.GroupID <= 0 {
		return
	}
	cleared, err := r.bindingStore.CompareAndDeleteGroupBinding(
		ctx, r.apiKeyID, r.protocol, r.sessionHash, binding)
	if err != nil {
		RecordRedisFailOpen()
		requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", r.protocol),
			zap.Error(err))
		return
	}
	if cleared {
		r.expectedBound = service.APIKeyGroupBinding{}
		RecordStickyInvalidated()
		requestLoggerFromContext(ctx).Info("gateway.group_sticky_invalidated",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", r.protocol),
			zap.String("session_hash", shortSessionFingerprint(r.sessionHash)),
			zap.Int64("group_id", binding.GroupID))
	}
}

func (r *apiKeyGroupRoute) MultiGroup() bool {
	return r != nil && r.state != nil && r.state.MultiGroup()
}

// preferResponseChain 使用哈希 Redis key 保存的 previous_response_id 映射覆盖普通
// 会话偏好。原始 response ID 不会进入 Redis key、value 或日志。
func (r *apiKeyGroupRoute) preferResponseChain(
	ctx context.Context,
	apiKey *service.APIKey,
	previousResponseID string,
) {
	previousResponseID = strings.TrimSpace(previousResponseID)
	if r == nil || !r.MultiGroup() || r.bindingStore == nil || previousResponseID == "" {
		return
	}
	binding, err := r.bindingStore.LoadGroupBinding(
		ctx,
		r.apiKeyID,
		ProtocolOpenAIChain,
		previousResponseID,
	)
	if err != nil {
		RecordRedisFailOpen()
		requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", ProtocolOpenAIChain),
			zap.Error(err))
		return
	}
	if binding.GroupID <= 0 {
		return
	}
	preferred := NewState(apiKey, binding.GroupID)
	if preferred.BoundGroupID() == 0 {
		_, _ = r.bindingStore.CompareAndDeleteGroupBinding(
			ctx,
			r.apiKeyID,
			ProtocolOpenAIChain,
			previousResponseID,
			binding,
		)
		return
	}
	r.responseChainID = previousResponseID
	r.expectedChain = binding
	r.loadedChain = binding.GroupID
	r.state = preferred
	RecordStickyHit()
	requestLoggerFromContext(ctx).Debug("gateway.group_response_chain_hit",
		zap.Int64("api_key_id", r.apiKeyID),
		zap.Int64("group_id", binding.GroupID))
}

// nextCandidate 只执行候选只读资格检查，不占用 RPM。
func (r *apiKeyGroupRoute) nextCandidate(c *gin.Context) (*service.APIKey, *service.UserSubscription, error) {
	if r == nil || r.state == nil {
		return nil, nil, errors.New("api key group route is not initialized")
	}
	if err := c.Request.Context().Err(); err != nil {
		return nil, nil, err
	}
	if !r.keyLimitsChecked {
		if err := r.billing.CheckAPIKeyRateLimitsReadOnly(c.Request.Context(), r.source); err != nil {
			r.lastErr = err
			r.failoverReason = routingFailureReason(err)
			return nil, nil, err
		}
		r.keyLimitsChecked = true
	}
	for {
		candidate, ok := r.state.Next()
		if !ok {
			r.clearPreflightOpsGroup(c)
			RecordExhausted()
			requestLoggerFromContext(c.Request.Context()).Warn("gateway.group_route_exhausted",
				zap.Int64("api_key_id", r.apiKeyID),
				zap.String("protocol", r.protocol),
				zap.String("session_hash", shortSessionFingerprint(r.sessionHash)),
				zap.Int("attempted_groups", r.state.AttemptedCount()),
				zap.String("failover_reason", r.failoverReason),
				zap.Error(r.lastErr))
			if r.lastErr != nil {
				return nil, nil, r.lastErr
			}
			return nil, nil, service.ErrGroupNotFound
		}
		group := candidate.Group
		if group == nil || candidate.User == nil || !group.IsActive() ||
			(!group.IsSubscriptionType() && !candidate.User.CanBindGroup(group.ID, group.IsExclusive)) {
			r.lastErr = service.ErrGroupNotFound
			r.failoverReason = "candidate_ineligible"
			r.clearFailedBinding(c.Request.Context(), groupIDOf(candidate))
			continue
		}

		if r.resetRequest != nil {
			r.resetRequest()
		}
		ctx := service.WithoutCompositeRouteDecision(c.Request.Context())
		if r.MultiGroup() {
			ctx = service.WithMultiGroupRouting(ctx)
		}
		ctx = context.WithValue(ctx, ctxkey.Group, group)
		if group.Platform == service.PlatformComposite && r.composite != nil && r.publicModel != "" {
			decision, matched, err := r.composite.ResolveCompositeRouteCandidate(
				ctx, group, r.publicModel, r.compositePath)
			if err != nil || !matched {
				if err != nil {
					r.lastErr = MarkCandidateError(err)
				} else {
					r.lastErr = service.ErrNoAvailableAccounts
				}
				r.failoverReason = "composite_unavailable"
				r.clearFailedBinding(ctx, group.ID)
				continue
			}
			ctx = service.WithCompositeRouteDecision(ctx, decision)
			if r.applyModel != nil {
				r.applyModel(decision.UpstreamModel)
			}
		}
		c.Request = c.Request.WithContext(ctx)
		if r.candidateCheck != nil {
			if err := r.candidateCheck(c, candidate); err != nil {
				r.lastErr = MarkCandidateError(err)
				r.failoverReason = "candidate_policy"
				r.clearFailedBinding(ctx, group.ID)
				continue
			}
		}

		var subscription *service.UserSubscription
		if group.IsSubscriptionType() {
			var err error
			subscription, err = r.billing.GetActiveSubscriptionForRouting(
				ctx, candidate, group.ID)
			if err != nil {
				r.lastErr = err
				r.failoverReason = "subscription_unavailable"
				r.clearFailedBinding(ctx, group.ID)
				continue
			}
		}
		// The first actual candidate already owns the one user-global RPM slot
		// for this client request. Later candidates must still re-check their
		// own group RPM, but must not reject the same request because that one
		// reservation made the global counter reach its limit.
		eligibilityUser := routingEligibilityUser(candidate.User, r.userRPMClaimed)
		eligibilityKey := routingEligibilityAPIKey(candidate)
		err := r.billing.CheckBillingEligibilityReadOnly(
			ctx,
			eligibilityUser,
			eligibilityKey,
			group,
			subscription,
			service.QuotaPlatform(ctx, candidate),
		)
		if err != nil {
			r.lastErr = err
			r.failoverReason = routingFailureReason(err)
			if isGlobalRoutingEligibilityError(err) {
				return nil, nil, err
			}
			r.clearFailedBinding(ctx, group.ID)
			continue
		}

		r.current = candidate
		r.currentSub = subscription
		r.currentSettled = false
		c.Set(string(middleware2.ContextKeyAPIKey), candidate)
		requestLoggerFromContext(ctx).Debug("gateway.group_route_candidate",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", r.protocol),
			zap.Int64("group_id", group.ID),
			zap.Int("failover_depth", r.state.AttemptedCount()-1),
			zap.Bool("sticky_candidate", r.state.BoundGroupID() == group.ID))
		return candidate, subscription, nil
	}
}

// clearPreflightOpsGroup prevents an all-preflight-rejected request from being
// attributed to the compatibility primary group. Once a candidate has reached
// account selection r.current is non-nil and remains the actual attempted
// group, so only the no-attempt case is cleared here.
func (r *apiKeyGroupRoute) clearPreflightOpsGroup(c *gin.Context) {
	if r == nil || r.current != nil || r.source == nil || c == nil || c.Request == nil {
		return
	}
	apiKey := *r.source
	apiKey.GroupID = nil
	apiKey.Group = nil
	apiKey.GroupIDs = []int64{}
	apiKey.Groups = []service.Group{}
	c.Set(string(middleware2.ContextKeyAPIKey), &apiKey)
	ctx := service.WithoutCompositeRouteDecision(c.Request.Context())
	ctx = context.WithValue(ctx, ctxkey.Group, (*service.Group)(nil))
	c.Request = c.Request.WithContext(ctx)
}

func routingEligibilityAPIKey(apiKey *service.APIKey) *service.APIKey {
	if apiKey == nil || !apiKey.HasRateLimits() {
		return apiKey
	}
	cloned := *apiKey
	cloned.RateLimit5h = 0
	cloned.RateLimit1d = 0
	cloned.RateLimit7d = 0
	return &cloned
}

func routingEligibilityUser(user *service.User, userRPMClaimed bool) *service.User {
	if user == nil || !userRPMClaimed || user.RPMLimit <= 0 {
		return user
	}
	cloned := *user
	cloned.RPMLimit = 0
	return &cloned
}

func isGlobalRoutingEligibilityError(err error) bool {
	return errors.Is(err, service.ErrUserRPMExceeded) ||
		errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded) ||
		errors.Is(err, service.ErrAPIKeyRateLimit1dExceeded) ||
		errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded) ||
		errors.Is(err, service.ErrBillingServiceUnavailable)
}

// reserveCurrent 在已选到可调度账号后调用。用户全局 RPM 每个客户端请求一次，
// 每个真正尝试的分组一次。
func (r *apiKeyGroupRoute) reserveCurrent(ctx context.Context) error {
	if !r.MultiGroup() || r.current == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Candidate discovery can happen before user/account concurrency waits.
	// Re-check the billing-owned, read-only eligibility immediately before RPM
	// reservation so a balance, minimum-balance, subscription or quota change
	// during that wait cannot reach the upstream using stale eligibility.
	if err := r.billing.CheckBillingEligibilityReadOnly(
		ctx,
		routingEligibilityUser(r.current.User, r.userRPMClaimed),
		routingEligibilityAPIKey(r.current),
		r.current.Group,
		r.currentSub,
		service.QuotaPlatform(ctx, r.current),
	); err != nil {
		r.lastErr = err
		r.failoverReason = routingFailureReason(err)
		if isGlobalRoutingEligibilityError(err) {
			return err
		}
		r.markCurrentUnavailable(ctx)
		return &ReservationCandidateError{err: err}
	}
	if !r.userRPMClaimed {
		if err := r.billing.ReserveUserRPM(ctx, r.current.User); err != nil {
			return err
		}
		r.userRPMClaimed = true
	}
	groupID := groupIDOf(r.current)
	if _, claimed := r.groupRPMClaims[groupID]; claimed {
		return nil
	}
	if err := r.billing.ReserveGroupRPM(ctx, r.current.User, r.current.Group); err != nil {
		r.lastErr = err
		r.failoverReason = "group_rpm_race"
		// A group RPM race makes this candidate unavailable for the current
		// request. Mark it as settled as well as clearing an old sticky binding,
		// otherwise the request finalizer could bind the session back to the
		// candidate that just lost the reservation race.
		r.markCurrentUnavailable(ctx)
		return &ReservationCandidateError{err: err}
	}
	r.groupRPMClaims[groupID] = struct{}{}
	depth := 0
	if r.state != nil && r.state.AttemptedCount() > 1 {
		depth = r.state.AttemptedCount() - 1
	}
	RecordGroupAttempt(depth)
	MaybeLogMetrics(ctx)
	requestLoggerFromContext(ctx).Info("gateway.group_route_attempt",
		zap.Int64("api_key_id", r.apiKeyID),
		zap.String("protocol", r.protocol),
		zap.String("session_hash", shortSessionFingerprint(r.sessionHash)),
		zap.Int64("group_id", groupID),
		zap.Int("failover_depth", depth),
		zap.String("failover_reason", r.failoverReason))
	r.failoverReason = ""
	return nil
}

func (r *apiKeyGroupRoute) clearFailedBinding(ctx context.Context, groupID int64) {
	if r == nil || r.bindingStore == nil || groupID <= 0 {
		return
	}
	if r.sticky && r.expectedBound.GroupID == groupID {
		cleared, err := r.bindingStore.CompareAndDeleteGroupBinding(
			ctx, r.apiKeyID, r.protocol, r.sessionHash, r.expectedBound)
		if err != nil {
			RecordRedisFailOpen()
			requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
				zap.Int64("api_key_id", r.apiKeyID),
				zap.String("protocol", r.protocol),
				zap.Error(err))
		} else if cleared {
			r.expectedBound = service.APIKeyGroupBinding{}
			RecordStickyInvalidated()
			requestLoggerFromContext(ctx).Info("gateway.group_sticky_invalidated",
				zap.Int64("api_key_id", r.apiKeyID),
				zap.String("protocol", r.protocol),
				zap.String("session_hash", shortSessionFingerprint(r.sessionHash)),
				zap.Int64("group_id", groupID))
		}
	}
	if r.responseChainID != "" && r.expectedChain.GroupID == groupID {
		cleared, err := r.bindingStore.CompareAndDeleteGroupBinding(
			ctx,
			r.apiKeyID,
			ProtocolOpenAIChain,
			r.responseChainID,
			r.expectedChain,
		)
		if err != nil {
			RecordRedisFailOpen()
			requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
				zap.Int64("api_key_id", r.apiKeyID),
				zap.String("protocol", ProtocolOpenAIChain),
				zap.Error(err))
		} else if cleared {
			r.expectedChain = service.APIKeyGroupBinding{}
			RecordStickyInvalidated()
		}
	}
}

func (r *apiKeyGroupRoute) markCurrentUnavailable(ctx context.Context) {
	if r == nil || r.current == nil {
		return
	}
	r.currentSettled = true
	if r.failoverReason == "" {
		r.failoverReason = "group_unavailable"
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	groupID := groupIDOf(r.current)
	if groupID <= 0 {
		return
	}
	if r.unavailable == nil {
		r.unavailable = make(map[int64]struct{})
	}
	r.unavailable[groupID] = struct{}{}
	r.clearFailedBinding(ctx, groupID)
}

// keepCurrentBinding 在成功响应或非分组故障的客户端错误后建立/刷新 1 小时绑定。
func (r *apiKeyGroupRoute) keepCurrentBinding(ctx context.Context) {
	if r == nil || !r.MultiGroup() || r.current == nil {
		return
	}
	r.currentSettled = true
	if !r.sticky || r.bindingStore == nil || (ctx != nil && ctx.Err() != nil) {
		return
	}
	currentGroupID := groupIDOf(r.current)
	if _, unavailable := r.unavailable[currentGroupID]; unavailable {
		return
	}
	newBinding := newVersionedBinding(currentGroupID)
	updated, err := r.bindingStore.CompareAndSetGroupBinding(
		ctx,
		r.apiKeyID,
		r.protocol,
		r.sessionHash,
		r.expectedBound,
		newBinding,
		BindingTTL,
	)
	if err != nil {
		RecordRedisFailOpen()
		requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", r.protocol),
			zap.Error(err))
		return
	}
	if updated {
		r.expectedBound = newBinding
	}
	if r.responseChainID != "" && r.expectedChain.GroupID == currentGroupID {
		newChainBinding := newVersionedBinding(currentGroupID)
		chainUpdated, err := r.bindingStore.CompareAndSetGroupBinding(
			ctx,
			r.apiKeyID,
			ProtocolOpenAIChain,
			r.responseChainID,
			r.expectedChain,
			newChainBinding,
			BindingTTL,
		)
		if err != nil {
			RecordRedisFailOpen()
			requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
				zap.Int64("api_key_id", r.apiKeyID),
				zap.String("protocol", ProtocolOpenAIChain),
				zap.Error(err))
		} else if chainUpdated {
			r.expectedChain = newChainBinding
		}
	}
}

// settleTerminalError updates only the sticky binding after a response can no
// longer transparently fail over. Group availability failures invalidate the
// current binding; request errors retain it so cache locality is preserved.
func (r *apiKeyGroupRoute) settleTerminalError(ctx context.Context, err error) {
	if ShouldCrossGroup(ctx, err, false) {
		r.markCurrentUnavailable(ctx)
		return
	}
	r.keepCurrentBinding(ctx)
}

// finish preserves a selected, still-eligible group when a handler exits on a
// request-scoped condition (for example user concurrency or global RPM). All
// group failures explicitly settle the current candidate first, so this
// fallback cannot resurrect a group that was marked unavailable.
func (r *apiKeyGroupRoute) finish(ctx context.Context) {
	if r == nil || r.current == nil || r.currentSettled {
		return
	}
	r.keepCurrentBinding(ctx)
}

func (r *apiKeyGroupRoute) bindResponseChain(ctx context.Context, responseID string) {
	responseID = strings.TrimSpace(responseID)
	if r == nil || !r.MultiGroup() || r.bindingStore == nil ||
		r.current == nil || responseID == "" {
		return
	}
	_, err := r.bindingStore.CompareAndSetGroupBinding(
		ctx,
		r.apiKeyID,
		ProtocolOpenAIChain,
		responseID,
		service.APIKeyGroupBinding{},
		newVersionedBinding(groupIDOf(r.current)),
		BindingTTL,
	)
	if err != nil {
		RecordRedisFailOpen()
		requestLoggerFromContext(ctx).Warn("gateway.group_sticky_redis_fail_open",
			zap.Int64("api_key_id", r.apiKeyID),
			zap.String("protocol", ProtocolOpenAIChain),
			zap.Error(err))
	}
}

func groupIDOf(apiKey *service.APIKey) int64 {
	if apiKey == nil || apiKey.GroupID == nil {
		return 0
	}
	return *apiKey.GroupID
}

func requestLoggerFromContext(ctx context.Context) *zap.Logger {
	// logger.FromContext 已在请求入口绑定 request_id；这里不记录原始会话标识。
	return logger.FromContext(ctx).Named("handler.api_key_group_routing")
}

func shortSessionFingerprint(session string) string {
	if session == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(session))
	return hex.EncodeToString(sum[:6])
}

func routingFailureReason(err error) string {
	switch {
	case errors.Is(err, service.ErrGroupRPMExceeded):
		return "group_rpm"
	case errors.Is(err, service.ErrUserRPMExceeded):
		return "user_rpm"
	case errors.Is(err, service.ErrSubscriptionInvalid),
		errors.Is(err, service.ErrDailyLimitExceeded),
		errors.Is(err, service.ErrWeeklyLimitExceeded),
		errors.Is(err, service.ErrMonthlyLimitExceeded):
		return "subscription_unavailable"
	case errors.Is(err, service.ErrInsufficientBalance):
		return "balance_unavailable"
	case errors.Is(err, service.ErrUserPlatformDailyQuotaExhausted),
		errors.Is(err, service.ErrUserPlatformWeeklyQuotaExhausted),
		errors.Is(err, service.ErrUserPlatformMonthlyQuotaExhausted):
		return "platform_quota"
	case errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded),
		errors.Is(err, service.ErrAPIKeyRateLimit1dExceeded),
		errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded):
		return "api_key_limit"
	case errors.Is(err, service.ErrBillingServiceUnavailable):
		return "billing_unavailable"
	default:
		return "candidate_unavailable"
	}
}
