package apikeyrouting

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const MaxGroups = 10

// State 保存单个客户端请求的候选分组状态。同一分组在一次请求内最多返回一次。
type State struct {
	apiKey       *service.APIKey
	orderedIDs   []int64
	boundGroupID int64
	attempted    map[int64]struct{}
	boundTried   bool
}

func NewState(apiKey *service.APIKey, boundGroupID int64) *State {
	state := &State{
		apiKey:     apiKey,
		attempted:  make(map[int64]struct{}),
		orderedIDs: orderedGroupIDs(apiKey),
	}
	if contains(state.orderedIDs, boundGroupID) {
		state.boundGroupID = boundGroupID
	}
	return state
}

func (s *State) MultiGroup() bool {
	return s != nil && len(s.orderedIDs) > 1
}

func (s *State) BoundGroupID() int64 {
	if s == nil {
		return 0
	}
	return s.boundGroupID
}

func (s *State) BoundFirst() bool {
	return s != nil && s.boundGroupID > 0 && s.boundTried && len(s.attempted) == 1
}

// Next 返回下一个候选。有效绑定先于优先级列表；绑定失败后则从列表顶部扫描。
func (s *State) Next() (*service.APIKey, bool) {
	if s == nil || s.apiKey == nil {
		return nil, false
	}
	if s.boundGroupID > 0 && !s.boundTried {
		s.boundTried = true
		s.attempted[s.boundGroupID] = struct{}{}
		if candidate, ok := APIKeyForGroup(s.apiKey, s.boundGroupID); ok {
			return candidate, true
		}
	}
	for _, groupID := range s.orderedIDs {
		if _, seen := s.attempted[groupID]; seen {
			continue
		}
		s.attempted[groupID] = struct{}{}
		if candidate, ok := APIKeyForGroup(s.apiKey, groupID); ok {
			return candidate, true
		}
	}
	return nil, false
}

func (s *State) AttemptedCount() int {
	if s == nil {
		return 0
	}
	return len(s.attempted)
}

func orderedGroupIDs(apiKey *service.APIKey) []int64 {
	if apiKey == nil {
		return nil
	}
	if len(apiKey.GroupIDs) > 0 {
		return append([]int64(nil), apiKey.GroupIDs...)
	}
	if apiKey.GroupID != nil && *apiKey.GroupID > 0 {
		return []int64{*apiKey.GroupID}
	}
	return nil
}

func contains(values []int64, target int64) bool {
	if target <= 0 {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// APIKeyForGroup 构建请求私有快照，避免切组时修改认证缓存共享对象。
func APIKeyForGroup(source *service.APIKey, groupID int64) (*service.APIKey, bool) {
	if source == nil || groupID <= 0 {
		return nil, false
	}
	var selected *service.Group
	for i := range source.Groups {
		if source.Groups[i].ID == groupID {
			group := cloneGroup(source.Groups[i])
			selected = &group
			break
		}
	}
	if selected == nil && source.Group != nil && source.Group.ID == groupID {
		group := cloneGroup(*source.Group)
		selected = &group
	}
	if selected == nil {
		return nil, false
	}

	cloned := *source
	cloned.GroupID = int64Ptr(groupID)
	cloned.Group = selected
	cloned.GroupIDs = append([]int64(nil), source.GroupIDs...)
	cloned.Groups = make([]service.Group, len(source.Groups))
	for i := range source.Groups {
		cloned.Groups[i] = cloneGroup(source.Groups[i])
	}
	cloned.IPWhitelist = append([]string(nil), source.IPWhitelist...)
	cloned.IPBlacklist = append([]string(nil), source.IPBlacklist...)
	cloned.LastUsedAt = cloneTime(source.LastUsedAt)
	cloned.LastUsedIP = cloneString(source.LastUsedIP)
	cloned.ExpiresAt = cloneTime(source.ExpiresAt)
	cloned.Window5hStart = cloneTime(source.Window5hStart)
	cloned.Window1dStart = cloneTime(source.Window1dStart)
	cloned.Window7dStart = cloneTime(source.Window7dStart)
	cloned.UserGroupRPMOverrides = cloneOverrides(source.UserGroupRPMOverrides)
	cloned.UserGroupRPMOverridesResolved = cloneOverrideResolution(source.UserGroupRPMOverridesResolved)
	cloned.GroupSubscriptions = cloneSubscriptions(source.GroupSubscriptions)
	cloned.GroupSubscriptionsResolved = cloneOverrideResolution(source.GroupSubscriptionsResolved)

	if source.User != nil {
		user := cloneUser(source.User)
		user.UserGroupRPMOverride = nil
		resolved, stateKnown := cloned.UserGroupRPMOverridesResolved[groupID]
		if !stateKnown {
			_, resolved = cloned.UserGroupRPMOverrides[groupID]
		}
		user.UserGroupRPMOverrideResolved = resolved
		if override, ok := cloned.UserGroupRPMOverrides[groupID]; ok {
			user.UserGroupRPMOverride = cloneInt(override)
		}
		cloned.User = user
	}
	return &cloned, true
}

func cloneOverrides(source map[int64]*int) map[int64]*int {
	if source == nil {
		return nil
	}
	cloned := make(map[int64]*int, len(source))
	for groupID, override := range source {
		cloned[groupID] = cloneInt(override)
	}
	return cloned
}

func cloneOverrideResolution(source map[int64]bool) map[int64]bool {
	if source == nil {
		return nil
	}
	cloned := make(map[int64]bool, len(source))
	for groupID, resolved := range source {
		cloned[groupID] = resolved
	}
	return cloned
}

func cloneSubscriptions(source map[int64]*service.UserSubscription) map[int64]*service.UserSubscription {
	if source == nil {
		return nil
	}
	cloned := make(map[int64]*service.UserSubscription, len(source))
	for groupID, subscription := range source {
		cloned[groupID] = cloneSubscription(subscription)
	}
	return cloned
}

func cloneSubscription(source *service.UserSubscription) *service.UserSubscription {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.DailyWindowStart = cloneTime(source.DailyWindowStart)
	cloned.WeeklyWindowStart = cloneTime(source.WeeklyWindowStart)
	cloned.MonthlyWindowStart = cloneTime(source.MonthlyWindowStart)
	cloned.AssignedBy = cloneInt64(source.AssignedBy)
	cloned.DeletedAt = cloneTime(source.DeletedAt)
	cloned.User = nil
	cloned.Group = nil
	cloned.AssignedByUser = nil
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUser(source *service.User) *service.User {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.AllowedGroups = append([]int64(nil), source.AllowedGroups...)
	cloned.LastLoginAt = cloneTime(source.LastLoginAt)
	cloned.LastActiveAt = cloneTime(source.LastActiveAt)
	cloned.LastUsedAt = cloneTime(source.LastUsedAt)
	cloned.DeletedAt = cloneTime(source.DeletedAt)
	cloned.TotpSecretEncrypted = cloneString(source.TotpSecretEncrypted)
	cloned.TotpEnabledAt = cloneTime(source.TotpEnabledAt)
	cloned.BalanceNotifyThreshold = cloneFloat64(source.BalanceNotifyThreshold)
	cloned.BalanceNotifyExtraEmails = append([]service.NotifyEmailEntry(nil), source.BalanceNotifyExtraEmails...)
	cloned.UserGroupRPMOverride = cloneInt(source.UserGroupRPMOverride)
	if source.GroupRates != nil {
		cloned.GroupRates = make(map[int64]float64, len(source.GroupRates))
		for groupID, rate := range source.GroupRates {
			cloned.GroupRates[groupID] = rate
		}
	}
	// Authentication snapshots do not normally hydrate these relations. Copy
	// their slice headers when a test or compatibility caller does, so request
	// code cannot append into the shared backing arrays.
	cloned.APIKeys = append([]service.APIKey(nil), source.APIKeys...)
	cloned.Subscriptions = make([]service.UserSubscription, len(source.Subscriptions))
	for i := range source.Subscriptions {
		clonedSubscription := cloneSubscription(&source.Subscriptions[i])
		cloned.Subscriptions[i] = *clonedSubscription
	}
	return &cloned
}

func int64Ptr(value int64) *int64 {
	return &value
}

func cloneGroup(source service.Group) service.Group {
	cloned := source
	cloned.DailyLimitUSD = cloneFloat64(source.DailyLimitUSD)
	cloned.WeeklyLimitUSD = cloneFloat64(source.WeeklyLimitUSD)
	cloned.MonthlyLimitUSD = cloneFloat64(source.MonthlyLimitUSD)
	cloned.ImagePrice1K = cloneFloat64(source.ImagePrice1K)
	cloned.ImagePrice2K = cloneFloat64(source.ImagePrice2K)
	cloned.ImagePrice4K = cloneFloat64(source.ImagePrice4K)
	cloned.VideoPrice480P = cloneFloat64(source.VideoPrice480P)
	cloned.VideoPrice720P = cloneFloat64(source.VideoPrice720P)
	cloned.VideoPrice1080P = cloneFloat64(source.VideoPrice1080P)
	cloned.WebSearchPricePerCall = cloneFloat64(source.WebSearchPricePerCall)
	cloned.FallbackGroupID = cloneInt64(source.FallbackGroupID)
	cloned.FallbackGroupIDOnInvalidRequest = cloneInt64(source.FallbackGroupIDOnInvalidRequest)
	cloned.SupportedModelScopes = append([]string(nil), source.SupportedModelScopes...)
	cloned.ReasoningEffortMappings = append([]service.ReasoningEffortMapping(nil), source.ReasoningEffortMappings...)
	cloned.AccountGroups = append([]service.AccountGroup(nil), source.AccountGroups...)
	cloned.ModelsListConfig.Models = append([]string(nil), source.ModelsListConfig.Models...)
	if source.MessagesDispatchModelConfig.ExactModelMappings != nil {
		cloned.MessagesDispatchModelConfig.ExactModelMappings = make(map[string]string, len(source.MessagesDispatchModelConfig.ExactModelMappings))
		for model, mappedModel := range source.MessagesDispatchModelConfig.ExactModelMappings {
			cloned.MessagesDispatchModelConfig.ExactModelMappings[model] = mappedModel
		}
	}
	if source.ModelRouting != nil {
		cloned.ModelRouting = make(map[string][]int64, len(source.ModelRouting))
		for model, accountIDs := range source.ModelRouting {
			cloned.ModelRouting[model] = append([]int64(nil), accountIDs...)
		}
	}
	return cloned
}

// ShouldCrossGroup 判断账号级重试耗尽后是否允许跨组。
// 参数/内容策略等非重试型 4xx、客户端取消及已经提交响应均禁止透明切组。
func ShouldCrossGroup(ctx context.Context, err error, responseCommitted bool) bool {
	if responseCommitted || err == nil || (ctx != nil && ctx.Err() != nil) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var failoverErr *service.UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		return false
	}
	if failoverErr.Scope == service.GatewayFailureScopeRequest {
		return false
	}
	if failoverErr.Stage == service.GatewayFailureStageAccountAuth &&
		failoverErr.Scope == service.GatewayFailureScopeAccount {
		return true
	}
	status := failoverErr.StatusCode
	return status == 0 ||
		status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout ||
		status >= http.StatusInternalServerError
}

// ShouldCrossBatchImageGroup classifies pre-response batch-image failures.
// Validation, idempotency and post-submit queue errors are request scoped and
// must not create a duplicate upstream batch in another group.
func ShouldCrossBatchImageGroup(ctx context.Context, err error) bool {
	if err == nil || (ctx != nil && ctx.Err() != nil) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, service.ErrBatchImageNoAccountAvailable) ||
		errors.Is(err, service.ErrBatchImageGroupDisabled) ||
		errors.Is(err, service.ErrBatchImageProviderSubmitFailed) ||
		errors.Is(err, service.ErrBatchImageProviderMissingAPIKey) ||
		errors.Is(err, service.ErrBatchImageProviderMissingServiceAccount) ||
		errors.Is(err, service.ErrBatchImageProviderUnsupportedAccount) ||
		errors.Is(err, service.ErrBatchImageVertexGCSBucketMissing)
}
