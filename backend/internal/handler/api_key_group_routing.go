package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	groupRoutingProtocolAnthropicMessages = apikeyrouting.ProtocolAnthropicMessages
	groupRoutingProtocolOpenAIChat        = apikeyrouting.ProtocolOpenAIChat
	groupRoutingProtocolOpenAIResponses   = apikeyrouting.ProtocolOpenAIResponses
	groupRoutingProtocolOpenAIChain       = apikeyrouting.ProtocolOpenAIChain
	groupRoutingProtocolGemini            = apikeyrouting.ProtocolGemini
	groupRoutingProtocolOpenAIEmbeddings  = apikeyrouting.ProtocolOpenAIEmbeddings
	groupRoutingProtocolOpenAIImages      = apikeyrouting.ProtocolOpenAIImages
	groupRoutingProtocolOpenAIAlphaSearch = apikeyrouting.ProtocolOpenAIAlphaSearch
	groupRoutingProtocolOpenAILive        = apikeyrouting.ProtocolOpenAILive
	groupRoutingProtocolCountTokens       = apikeyrouting.ProtocolCountTokens
	groupRoutingProtocolGrokMedia         = apikeyrouting.ProtocolGrokMedia
	groupRoutingProtocolBatchImages       = apikeyrouting.ProtocolBatchImages
)

type apiKeyGroupRoute struct {
	runtime apikeyrouting.RouteRuntime
}

type apiKeyCandidateError = apikeyrouting.CandidateError

func markAPIKeyCandidateError(err error) error {
	return apikeyrouting.MarkCandidateError(err)
}

func reservationCanTryNextGroup(err error) bool {
	return apikeyrouting.ReservationCanTryNext(err)
}

func (h *GatewayHandler) newAPIKeyGroupRoute(
	ctx context.Context,
	apiKey *service.APIKey,
	protocol, sessionHash string,
	sticky bool,
) *apiKeyGroupRoute {
	return newAPIKeyGroupRoute(
		ctx, h.billingCacheService, h.gatewayService, apiKey, protocol, sessionHash, sticky)
}

func (h *OpenAIGatewayHandler) newAPIKeyGroupRoute(
	ctx context.Context,
	apiKey *service.APIKey,
	protocol, sessionHash string,
	sticky bool,
) *apiKeyGroupRoute {
	return newAPIKeyGroupRoute(
		ctx, h.billingCacheService, h.gatewayService, apiKey, protocol, sessionHash, sticky)
}

func newAPIKeyGroupRoute(
	ctx context.Context,
	billing *service.BillingCacheService,
	bindingStore apikeyrouting.BindingStore,
	apiKey *service.APIKey,
	protocol, sessionHash string,
	sticky bool,
) *apiKeyGroupRoute {
	runtime := apikeyrouting.NewRouteRuntime(
		ctx, billing, bindingStore, apiKey, protocol, sessionHash, sticky)
	if preferredGroupID := apikeyrouting.PreferredGroup(ctx); preferredGroupID > 0 {
		runtime.PreferGroup(preferredGroupID)
	}
	return &apiKeyGroupRoute{runtime: runtime}
}

func (r *apiKeyGroupRoute) preferGroup(groupID int64) {
	r.runtime.PreferGroup(groupID)
}

func (r *apiKeyGroupRoute) configureCompositeRequest(
	model *string,
	body *[]byte,
	after func(string, []byte),
) {
	r.runtime.ConfigureCompositeRequest(model, body, after)
}

func (r *apiKeyGroupRoute) setCandidateCheck(check func(*gin.Context, *service.APIKey) error) {
	r.runtime.SetCandidateCheck(check)
}

func (r *apiKeyGroupRoute) MultiGroup() bool {
	return r.runtime.MultiGroup()
}

func (r *apiKeyGroupRoute) preferResponseChain(
	ctx context.Context,
	apiKey *service.APIKey,
	previousResponseID string,
) {
	r.runtime.PreferResponseChain(ctx, apiKey, previousResponseID)
}

func (r *apiKeyGroupRoute) loadedChainGroupID() int64 {
	return r.runtime.LoadedChainGroupID()
}

func (r *apiKeyGroupRoute) nextCandidate(c *gin.Context) (*service.APIKey, *service.UserSubscription, error) {
	return r.runtime.NextCandidate(c)
}

func (r *apiKeyGroupRoute) reserveCurrent(ctx context.Context) error {
	return r.runtime.ReserveCurrent(ctx)
}

func (r *apiKeyGroupRoute) markCurrentUnavailable(ctx context.Context) {
	r.runtime.MarkCurrentUnavailable(ctx)
}

func (r *apiKeyGroupRoute) keepCurrentBinding(ctx context.Context) {
	r.runtime.KeepCurrentBinding(ctx)
}

func (r *apiKeyGroupRoute) settleTerminalError(ctx context.Context, err error) {
	r.runtime.SettleTerminalError(ctx, err)
}

func (r *apiKeyGroupRoute) bindResponseChain(ctx context.Context, responseID string) {
	r.runtime.BindResponseChain(ctx, responseID)
}

func (r *apiKeyGroupRoute) finish(c *gin.Context) {
	if r == nil || c == nil || c.Request == nil {
		return
	}
	r.runtime.Finish(c.Request.Context())
}

func groupIDOf(apiKey *service.APIKey) int64 {
	if apiKey == nil || apiKey.GroupID == nil {
		return 0
	}
	return *apiKey.GroupID
}

func maySwitchOpenAIResponsesWSGroup(
	ctx context.Context,
	multiGroup bool,
	upstreamOutputCommitted bool,
	continuationMovable bool,
	failoverErr *service.UpstreamFailoverError,
) bool {
	if !multiGroup || upstreamOutputCommitted || !continuationMovable {
		return false
	}
	if failoverErr == nil {
		return true
	}
	return apikeyrouting.ShouldCrossGroup(ctx, failoverErr, false)
}

func openAIResponseChainID(result *service.OpenAIForwardResult) string {
	if result == nil {
		return ""
	}
	for _, candidate := range []string{result.ResponseID, result.RequestID} {
		candidate = strings.TrimSpace(candidate)
		if service.ClassifyOpenAIPreviousResponseIDKind(candidate) == service.OpenAIPreviousResponseIDKindResponseID {
			return candidate
		}
	}
	return ""
}
