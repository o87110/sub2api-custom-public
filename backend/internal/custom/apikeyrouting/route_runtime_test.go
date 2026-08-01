package apikeyrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupBindingStoreStub struct {
	mu       sync.Mutex
	values   map[string]int64
	versions map[string]string
}

type compositeGroupBindingStoreStub struct {
	*groupBindingStoreStub
	decisions map[int64]service.CompositeRouteDecision
}

type routingUserRepoStub struct {
	service.UserRepository
	balance float64
}

func (s *routingUserRepoStub) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Balance: s.balance, Status: service.StatusActive}, nil
}

func groupIDPtr(value int64) *int64 {
	return &value
}

func (s *compositeGroupBindingStoreStub) ResolveCompositeRouteCandidate(
	_ context.Context,
	group *service.Group,
	_ string,
	_ string,
) (service.CompositeRouteDecision, bool, error) {
	if group == nil {
		return service.CompositeRouteDecision{}, false, nil
	}
	decision, ok := s.decisions[group.ID]
	return decision, ok, nil
}

func newGroupBindingStoreStub() *groupBindingStoreStub {
	return &groupBindingStoreStub{
		values:   make(map[string]int64),
		versions: make(map[string]string),
	}
}

func bindingStubKey(apiKeyID int64, protocol, sessionHash string) string {
	return fmt.Sprintf("%d:%s:%s", apiKeyID, protocol, sessionHash)
}

func (s *groupBindingStoreStub) LoadGroupBinding(
	_ context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
) (service.APIKeyGroupBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bindingStubKey(apiKeyID, protocol, sessionHash)
	return service.APIKeyGroupBinding{GroupID: s.values[key], Version: s.versions[key]}, nil
}

func (s *groupBindingStoreStub) CompareAndSetGroupBinding(
	_ context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
	oldBinding, newBinding service.APIKeyGroupBinding,
	_ time.Duration,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bindingStubKey(apiKeyID, protocol, sessionHash)
	if s.values[key] != oldBinding.GroupID || s.versions[key] != oldBinding.Version {
		return false, nil
	}
	s.values[key] = newBinding.GroupID
	s.versions[key] = newBinding.Version
	return true, nil
}

func (s *groupBindingStoreStub) CompareAndDeleteGroupBinding(
	_ context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
	oldBinding service.APIKeyGroupBinding,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bindingStubKey(apiKeyID, protocol, sessionHash)
	if s.values[key] != oldBinding.GroupID || s.versions[key] != oldBinding.Version {
		return false, nil
	}
	delete(s.values, key)
	delete(s.versions, key)
	return true, nil
}

func TestAPIKeyGroupRoutePrefersAndPersistsResponseChain(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       77,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIResponses, "session")] = 1
	store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIChain, "resp-old")] = 2
	route := &apiKeyGroupRoute{
		bindingStore:   store,
		state:          NewState(apiKey, 1),
		apiKeyID:       apiKey.ID,
		protocol:       ProtocolOpenAIResponses,
		sessionHash:    "session",
		sticky:         true,
		expectedBound:  service.APIKeyGroupBinding{GroupID: 1},
		groupRPMClaims: make(map[int64]struct{}),
	}

	route.preferResponseChain(ctx, apiKey, " resp-old ")
	candidate, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(2), *candidate.GroupID)

	route.current = candidate
	route.keepCurrentBinding(ctx)
	route.bindResponseChain(ctx, " resp-new ")

	require.Equal(t, int64(2), store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIResponses, "session")])
	require.Equal(t, int64(2), store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIChain, "resp-old")])
	require.Equal(t, int64(2), store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIChain, "resp-new")])
}

func TestAPIKeyGroupRouteRequestPreferenceIsLocalAndFallsBackWhenRemoved(t *testing.T) {
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       76,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	ctx := WithPreferredGroup(context.Background(), 2)
	route := newAPIKeyGroupRoute(ctx, nil, store, apiKey, ProtocolOpenAIImages, "", false)
	route.PreferGroup(PreferredGroup(ctx))
	candidate, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(2), groupIDOf(candidate))
	require.Empty(t, store.values, "请求级偏好不得创建跨请求 Redis 粘性")

	removed := &service.APIKey{
		ID:       apiKey.ID,
		GroupIDs: []int64{1, 3},
		Groups:   []service.Group{{ID: 1}, {ID: 3}},
	}
	route = newAPIKeyGroupRoute(ctx, nil, store, removed, ProtocolOpenAIImages, "", false)
	route.PreferGroup(PreferredGroup(ctx))
	candidate, ok = route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(1), groupIDOf(candidate),
		"偏好分组已移除时应回到当前列表最高优先级")
	require.Empty(t, store.values)
}

func TestAPIKeyGroupRouteClearsOnlyMatchingResponseChainBinding(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       88,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIChain, "resp-old")] = 2
	route := &apiKeyGroupRoute{
		bindingStore:    store,
		state:           NewState(apiKey, 2),
		apiKeyID:        apiKey.ID,
		protocol:        ProtocolOpenAIResponses,
		responseChainID: "resp-old",
		expectedChain:   service.APIKeyGroupBinding{GroupID: 2},
		groupRPMClaims:  make(map[int64]struct{}),
	}
	candidate, ok := route.state.Next()
	require.True(t, ok)
	route.current = candidate

	route.markCurrentUnavailable(ctx)

	_, exists := store.values[bindingStubKey(apiKey.ID, ProtocolOpenAIChain, "resp-old")]
	require.False(t, exists)
	require.Zero(t, route.expectedChain)
}

func TestAPIKeyGroupRouteDoesNotRebindUnavailableCurrentGroup(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       89,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	bindingKey := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[bindingKey] = 2
	route := newAPIKeyGroupRoute(ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)

	candidate, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(2), groupIDOf(candidate))
	route.current = candidate
	route.markCurrentUnavailable(ctx)
	route.keepCurrentBinding(ctx)
	_, rebound := store.values[bindingKey]
	require.False(t, rebound, "已判定不可用的当前分组不得被错误重新绑定")

	next, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(1), groupIDOf(next))
	route.current = next
	route.keepCurrentBinding(ctx)
	require.Equal(t, int64(1), store.values[bindingKey])
}

func TestAPIKeyGroupRouteSettlesCommittedResponseBindingByFailureClass(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		wantBinding bool
	}{
		{name: "request error retains binding", statusCode: http.StatusBadRequest, wantBinding: true},
		{name: "group failure invalidates binding", statusCode: http.StatusServiceUnavailable, wantBinding: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newGroupBindingStoreStub()
			apiKey := &service.APIKey{
				ID:       90,
				GroupIDs: []int64{1, 2},
				Groups:   []service.Group{{ID: 1}, {ID: 2}},
			}
			bindingKey := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
			store.values[bindingKey] = 1
			route := newAPIKeyGroupRoute(
				ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)
			candidate, ok := route.state.Next()
			require.True(t, ok)
			route.current = candidate

			route.settleTerminalError(ctx, &service.UpstreamFailoverError{StatusCode: tt.statusCode})

			_, bound := store.values[bindingKey]
			require.Equal(t, tt.wantBinding, bound)
		})
	}
}

func TestAPIKeyGroupRouteFinishRetainsUnsettledEligibleBinding(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       901,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	bindingKey := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[bindingKey] = 2
	route := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)
	candidate, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(2), groupIDOf(candidate))
	route.current = candidate

	route.finish(ctx)

	require.Equal(t, int64(2), store.values[bindingKey])
	require.NotEmpty(t, store.versions[bindingKey], "finish should refresh the sliding binding")
}

func TestAPIKeyGroupRouteFinishCannotResurrectUnavailableBinding(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       902,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	bindingKey := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[bindingKey] = 2
	route := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)
	candidate, ok := route.state.Next()
	require.True(t, ok)
	route.current = candidate

	route.markCurrentUnavailable(ctx)
	route.finish(ctx)

	_, rebound := store.values[bindingKey]
	require.False(t, rebound)
}

func TestAPIKeyGroupRouteStaleFailureCannotDeleteConcurrentRefresh(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       91,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	bindingKey := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[bindingKey] = 2

	successRoute := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)
	staleFailureRoute := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)
	for _, route := range []*apiKeyGroupRoute{successRoute, staleFailureRoute} {
		candidate, ok := route.state.Next()
		require.True(t, ok)
		require.Equal(t, int64(2), groupIDOf(candidate))
		route.current = candidate
	}

	successRoute.keepCurrentBinding(ctx)
	refreshedVersion := store.versions[bindingKey]
	require.NotEmpty(t, refreshedVersion)

	staleFailureRoute.markCurrentUnavailable(ctx)

	require.Equal(t, int64(2), store.values[bindingKey])
	require.Equal(t, refreshedVersion, store.versions[bindingKey])
}

func TestNewAPIKeyGroupRouteClearsBindingOutsideCurrentList(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       99,
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
	}
	key := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[key] = 9

	route := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)

	_, exists := store.values[key]
	require.False(t, exists)
	require.Zero(t, route.expectedBound)
	require.Zero(t, route.state.BoundGroupID())
	candidate, ok := route.state.Next()
	require.True(t, ok)
	require.Equal(t, int64(1), *candidate.GroupID)
}

func TestNewAPIKeyGroupRouteSingleGroupDoesNotTouchStickyStore(t *testing.T) {
	ctx := context.Background()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       100,
		GroupIDs: []int64{1},
		Groups:   []service.Group{{ID: 1}},
	}
	key := bindingStubKey(apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[key] = 9

	route := newAPIKeyGroupRoute(
		ctx, nil, store, apiKey, ProtocolOpenAIChat, "session", true)

	require.False(t, route.sticky)
	require.False(t, route.MultiGroup())
	require.Equal(t, int64(9), store.values[key], "legacy single-group requests must not read or clear multi-group sticky state")
}

func TestAPIKeyGroupRouteDoesNotAttributePreflightExhaustionToPrimaryGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID := int64(1)
	apiKey := &service.APIKey{
		ID:       101,
		GroupID:  &primaryID,
		Group:    &service.Group{ID: 1, Status: service.StatusDisabled},
		GroupIDs: []int64{1, 2},
		Groups: []service.Group{
			{ID: 1, Status: service.StatusDisabled},
			{ID: 2, Status: service.StatusDisabled},
		},
		User: &service.User{ID: 9, Status: service.StatusActive},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	route := newAPIKeyGroupRoute(
		c.Request.Context(), nil, nil, apiKey, ProtocolOpenAIResponses, "", false)
	_, _, err := route.nextCandidate(c)
	require.Error(t, err)

	actual, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Nil(t, actual.GroupID)
	require.Nil(t, actual.Group)
	require.Empty(t, actual.GroupIDs)
	require.Empty(t, actual.Groups)
	require.Equal(t, primaryID, *apiKey.GroupID, "source snapshot must remain unchanged")
}

func TestAPIKeyGroupRouteResolvesEveryCompositeCandidateFromCanonicalRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "public-model",
		TargetPlatform: service.PlatformAnthropic,
		UpstreamModel:  "stale-initial-model",
		Endpoint:       service.CompositeRouteEndpointResponses,
	})
	apiKey := &service.APIKey{
		ID:       101,
		UserID:   42,
		GroupID:  groupIDPtr(1),
		GroupIDs: []int64{1, 2},
		Group:    &service.Group{ID: 1, Platform: service.PlatformComposite, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: 1, Platform: service.PlatformComposite, Status: service.StatusActive},
			{ID: 2, Platform: service.PlatformComposite, Status: service.StatusActive},
		},
		User: &service.User{ID: 42},
	}
	store := &compositeGroupBindingStoreStub{
		groupBindingStoreStub: newGroupBindingStoreStub(),
		decisions: map[int64]service.CompositeRouteDecision{
			1: {
				Matched:        true,
				Source:         service.CompositeRouteSourceExplicit,
				PublicModel:    "public-model",
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "group-a-model",
				Endpoint:       service.CompositeRouteEndpointResponses,
			},
			2: {
				Matched:        true,
				Source:         service.CompositeRouteSourceExplicit,
				PublicModel:    "public-model",
				TargetPlatform: service.PlatformGrok,
				UpstreamModel:  "group-b-model",
				Endpoint:       service.CompositeRouteEndpointResponses,
			},
		},
	}
	billing := service.NewBillingCacheService(
		nil, nil, nil, nil, nil, nil,
		&config.Config{RunMode: config.RunModeSimple},
		nil,
	)
	t.Cleanup(billing.Stop)
	route := newAPIKeyGroupRoute(
		ctx, billing, store, apiKey, ProtocolOpenAIResponses, "", false)
	model := "public-model"
	body := []byte(`{"model":"public-model","input":"hello"}`)
	route.configureCompositeRequest(&model, &body, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)

	first, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(1), *first.GroupID)
	require.Equal(t, "group-a-model", model)
	firstPlatform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenAI, firstPlatform)

	// Simulate a channel-level mapping applied by the protocol handler after
	// selecting group A. The next candidate must still be rebuilt from the
	// canonical public request and group B's own composite decision.
	model = "group-a-channel-model"
	route.markCurrentUnavailable(c.Request.Context())
	second, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(2), *second.GroupID)
	require.Equal(t, "group-b-model", model)
	require.JSONEq(t, `{"model":"group-b-model","input":"hello"}`, string(body))
	secondPlatform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformGrok, secondPlatform)
}

func TestAPIKeyGroupRouteSkipsCompositeCandidateUnsupportedByProtocolAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{
		Matched:       true,
		PublicModel:   "public-model",
		UpstreamModel: "stale-primary-model",
	})
	apiKey := &service.APIKey{
		ID:       102,
		UserID:   42,
		GroupID:  groupIDPtr(1),
		GroupIDs: []int64{1, 2},
		Group:    &service.Group{ID: 1, Platform: service.PlatformComposite, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: 1, Platform: service.PlatformComposite, Status: service.StatusActive},
			{ID: 2, Platform: service.PlatformComposite, Status: service.StatusActive},
		},
		User: &service.User{ID: 42},
	}
	store := &compositeGroupBindingStoreStub{
		groupBindingStoreStub: newGroupBindingStoreStub(),
		decisions: map[int64]service.CompositeRouteDecision{
			1: {
				Matched:        true,
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "openai-model",
			},
			2: {
				Matched:        true,
				TargetPlatform: service.PlatformAnthropic,
				UpstreamModel:  "anthropic-model",
			},
		},
	}
	billing := service.NewBillingCacheService(
		nil, nil, nil, nil, nil, nil,
		&config.Config{RunMode: config.RunModeSimple},
		nil,
	)
	t.Cleanup(billing.Stop)
	route := newAPIKeyGroupRoute(ctx, billing, store, apiKey, ProtocolOpenAIChat, "", false)
	model := "public-model"
	body := []byte(`{"model":"public-model","messages":[]}`)
	route.configureCompositeRequest(&model, &body, nil)
	route.setCandidateCheck(func(c *gin.Context, _ *service.APIKey) error {
		platform, _ := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		if platform != service.PlatformAnthropic {
			return fmt.Errorf("protocol adapter does not support %s", platform)
		}
		return nil
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	candidate, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(2), groupIDOf(candidate))
	require.Equal(t, "anthropic-model", model)
	require.JSONEq(t, `{"model":"anthropic-model","messages":[]}`, string(body))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformAnthropic, platform)
	require.Equal(t, 2, route.state.AttemptedCount(),
		"不兼容候选只读跳过，不得被发送到错误协议适配器")
}

func TestAPIKeyGroupRouteDoesNotAdvanceOrClearBindingAfterClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newGroupBindingStoreStub()
	apiKey := &service.APIKey{
		ID:       202,
		GroupIDs: []int64{1, 2},
		Groups: []service.Group{
			{ID: 1, Status: service.StatusActive},
			{ID: 2, Status: service.StatusActive},
		},
	}
	bindingKey := bindingStubKey(
		apiKey.ID, ProtocolOpenAIChat, "session")
	store.values[bindingKey] = 1
	route := &apiKeyGroupRoute{
		bindingStore:   store,
		state:          NewState(apiKey, 1),
		apiKeyID:       apiKey.ID,
		protocol:       ProtocolOpenAIChat,
		sessionHash:    "session",
		sticky:         true,
		expectedBound:  service.APIKeyGroupBinding{GroupID: 1},
		current:        &service.APIKey{GroupID: groupIDPtr(1)},
		groupRPMClaims: make(map[int64]struct{}),
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)

	_, _, err := route.nextCandidate(c)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, route.state.AttemptedCount())

	route.markCurrentUnavailable(ctx)
	require.Equal(t, int64(1), store.values[bindingKey])
}

func TestAPIKeyGroupRouteMarksContextOnlyForActualMultiGroup(t *testing.T) {
	tests := []struct {
		name     string
		groupIDs []int64
		want     bool
	}{
		{name: "single group keeps legacy fallback", groupIDs: []int64{1}, want: false},
		{name: "multiple groups disable implicit fallback", groupIDs: []int64{1, 2}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := make([]service.Group, 0, len(tt.groupIDs))
			for _, groupID := range tt.groupIDs {
				groups = append(groups, service.Group{ID: groupID, Status: service.StatusActive})
			}
			apiKey := &service.APIKey{
				ID:       303,
				UserID:   404,
				User:     &service.User{ID: 404},
				GroupIDs: append([]int64(nil), tt.groupIDs...),
				Groups:   groups,
			}
			route := newAPIKeyGroupRoute(
				context.Background(), nil, nil, apiKey,
				ProtocolOpenAIChat, "", false,
			)
			var observed []bool
			route.setCandidateCheck(func(c *gin.Context, _ *service.APIKey) error {
				observed = append(observed, service.IsMultiGroupRouting(c.Request.Context()))
				return fmt.Errorf("stop before billing")
			})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

			_, _, err := route.nextCandidate(c)
			require.Error(t, err)
			require.NotEmpty(t, observed)
			for _, got := range observed {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRoutingEligibilityUserSkipsAlreadyReservedGlobalRPMLimit(t *testing.T) {
	user := &service.User{ID: 9, RPMLimit: 1}

	first := routingEligibilityUser(user, false)
	require.Same(t, user, first)
	require.Equal(t, 1, first.RPMLimit)

	next := routingEligibilityUser(user, true)
	require.NotSame(t, user, next)
	require.Equal(t, int64(9), next.ID)
	require.Zero(t, next.RPMLimit)
	require.Equal(t, 1, user.RPMLimit, "routing probe must not mutate the cached user snapshot")
}

func TestRoutingEligibilityAPIKeyClearsOnlyPerKeyRateWindows(t *testing.T) {
	apiKey := &service.APIKey{
		ID: 7, Quota: 12, QuotaUsed: 3,
		RateLimit5h: 1, RateLimit1d: 2, RateLimit7d: 3,
	}

	eligibility := routingEligibilityAPIKey(apiKey)
	require.NotSame(t, apiKey, eligibility)
	require.Zero(t, eligibility.RateLimit5h)
	require.Zero(t, eligibility.RateLimit1d)
	require.Zero(t, eligibility.RateLimit7d)
	require.Equal(t, apiKey.Quota, eligibility.Quota)
	require.Equal(t, apiKey.QuotaUsed, eligibility.QuotaUsed)
	require.Equal(t, 1.0, apiKey.RateLimit5h, "routing view must not mutate the auth snapshot")
}

func TestAPIKeyGroupRouteMetricsUseCandidateScanDepth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	billing := service.NewBillingCacheService(
		nil, nil, nil, nil, nil, nil,
		&config.Config{RunMode: config.RunModeSimple},
		nil,
	)
	t.Cleanup(billing.Stop)
	apiKey := &service.APIKey{
		ID: 505, UserID: 606,
		GroupIDs: []int64{1, 2},
		Groups: []service.Group{
			{ID: 1, Status: service.StatusDisabled},
			{ID: 2, Status: service.StatusActive},
		},
		User: &service.User{ID: 606, Status: service.StatusActive},
	}
	route := newAPIKeyGroupRoute(
		context.Background(), billing, nil, apiKey,
		ProtocolOpenAIChat, "session-value", true,
	)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	candidate, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(2), groupIDOf(candidate))
	before := SnapshotMetrics()
	require.NoError(t, route.reserveCurrent(c.Request.Context()))
	after := SnapshotMetrics()

	require.Equal(t, before.GroupAttempts+1, after.GroupAttempts)
	require.Equal(t, before.CrossGroup+1, after.CrossGroup)
	require.Equal(t, before.FailoverDepth+1, after.FailoverDepth)
	require.Equal(t, "", route.failoverReason, "attempt log must consume the pending reason")
}

func TestAPIKeyGroupRouteRechecksEligibilityImmediatelyBeforeReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userRepo := &routingUserRepoStub{balance: 10}
	billing := service.NewBillingCacheService(
		nil, userRepo, nil, nil, nil, nil,
		&config.Config{},
		nil,
	)
	t.Cleanup(billing.Stop)
	groups := []service.Group{
		{ID: 1, Status: service.StatusActive},
		{ID: 2, Status: service.StatusActive},
	}
	apiKey := &service.APIKey{
		ID:       707,
		UserID:   808,
		GroupID:  groupIDPtr(1),
		Group:    &groups[0],
		GroupIDs: []int64{1, 2},
		Groups:   groups,
		User:     &service.User{ID: 808, Balance: 10, Status: service.StatusActive},
	}
	route := newAPIKeyGroupRoute(
		context.Background(), billing, nil, apiKey,
		ProtocolOpenAIChat, "", false,
	)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	first, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(1), groupIDOf(first))

	// Simulate a balance change while the request waits for concurrency. The
	// stale candidate must be rejected before either RPM counter is occupied.
	userRepo.balance = 0
	err = route.reserveCurrent(c.Request.Context())
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.True(t, ReservationCanTryNext(err))
	_, unavailable := route.unavailable[int64(1)]
	require.True(t, unavailable)

	// Once eligibility is restored, the same request continues from the next
	// priority without trying the exhausted first group again.
	userRepo.balance = 10
	second, _, err := route.nextCandidate(c)
	require.NoError(t, err)
	require.Equal(t, int64(2), groupIDOf(second))
}

func TestShortSessionFingerprintNeverReturnsRawSession(t *testing.T) {
	raw := "raw-client-session-identifier"
	fingerprint := shortSessionFingerprint(raw)
	require.NotEmpty(t, fingerprint)
	require.NotEqual(t, raw, fingerprint)
	require.Len(t, fingerprint, 12)
	require.Empty(t, shortSessionFingerprint(""))
}
