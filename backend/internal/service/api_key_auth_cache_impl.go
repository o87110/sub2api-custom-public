package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/dgraph-io/ristretto"
)

const apiKeyAuthSnapshotVersion = 22 // v22: ordered groups, per-group RPM overrides and subscription snapshots

type apiKeyAuthCacheConfig struct {
	l1Size        int
	l1TTL         time.Duration
	l2TTL         time.Duration
	negativeTTL   time.Duration
	jitterPercent int
	singleflight  bool
}

func newAPIKeyAuthCacheConfig(cfg *config.Config) apiKeyAuthCacheConfig {
	if cfg == nil {
		return apiKeyAuthCacheConfig{}
	}
	auth := cfg.APIKeyAuth
	return apiKeyAuthCacheConfig{
		l1Size:        auth.L1Size,
		l1TTL:         time.Duration(auth.L1TTLSeconds) * time.Second,
		l2TTL:         time.Duration(auth.L2TTLSeconds) * time.Second,
		negativeTTL:   time.Duration(auth.NegativeTTLSeconds) * time.Second,
		jitterPercent: auth.JitterPercent,
		singleflight:  auth.Singleflight,
	}
}

func (c apiKeyAuthCacheConfig) l1Enabled() bool {
	return c.l1Size > 0 && c.l1TTL > 0
}

func (c apiKeyAuthCacheConfig) l2Enabled() bool {
	return c.l2TTL > 0
}

func (c apiKeyAuthCacheConfig) negativeEnabled() bool {
	return c.negativeTTL > 0
}

// jitterTTL 为缓存 TTL 添加抖动，避免多个请求在同一时刻同时过期触发集中回源。
// 这里直接使用 rand/v2 的顶层函数：并发安全，无需全局互斥锁。
func (c apiKeyAuthCacheConfig) jitterTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return ttl
	}
	if c.jitterPercent <= 0 {
		return ttl
	}
	percent := c.jitterPercent
	if percent > 100 {
		percent = 100
	}
	delta := float64(percent) / 100
	randVal := rand.Float64()
	factor := 1 - delta + randVal*(2*delta)
	if factor <= 0 {
		return ttl
	}
	return time.Duration(float64(ttl) * factor)
}

func (s *APIKeyService) initAuthCache(cfg *config.Config) {
	s.authCfg = newAPIKeyAuthCacheConfig(cfg)
	if s.authCfg.negativeEnabled() {
		negativeSize := defaultNegativeAuthCacheSize
		if s.authCfg.l1Size > 0 && s.authCfg.l1Size < negativeSize {
			negativeSize = s.authCfg.l1Size
		}
		cache, err := ristretto.NewCache(&ristretto.Config{
			NumCounters: int64(negativeSize) * 10,
			MaxCost:     int64(negativeSize),
			BufferItems: 64,
		})
		if err == nil {
			s.authNegativeCacheL1 = cache
		}
	}
	if s.authCfg.l1Enabled() {
		cache, err := ristretto.NewCache(&ristretto.Config{
			NumCounters: int64(s.authCfg.l1Size) * 10,
			MaxCost:     int64(s.authCfg.l1Size),
			BufferItems: 64,
		})
		if err == nil {
			s.authCacheL1 = cache
		}
	}
}

// StartAuthCacheInvalidationSubscriber starts the Pub/Sub subscriber for L1 cache invalidation.
// This should be called after the service is fully initialized.
func (s *APIKeyService) StartAuthCacheInvalidationSubscriber(ctx context.Context) {
	if s.cache == nil || (s.authCacheL1 == nil && s.authNegativeCacheL1 == nil) {
		return
	}
	s.authInvalidationStart.Do(func() {
		subscriberCtx, cancel := context.WithCancel(ctx)
		subscriberCtx = withAuthCacheSubscriptionReady(subscriberCtx, func() {
			s.authInvalidationConnected.Store(true)
		})
		s.authInvalidationCancel = cancel
		s.authInvalidationWG.Add(1)
		go func() {
			defer s.authInvalidationWG.Done()
			backoff := time.Second
			for {
				err := s.cache.SubscribeAuthCacheInvalidation(subscriberCtx, func(cacheKey string) {
					s.invalidateLocalAuthCache(cacheKey)
				})
				wasConnected := s.authInvalidationConnected.Swap(false)
				if subscriberCtx.Err() != nil {
					return
				}
				if wasConnected {
					backoff = time.Second
				}
				s.authInvalidationFailures.Add(1)
				if err == nil {
					err = errors.New("auth cache invalidation subscription closed")
				}
				slog.Warn("failed to start auth cache invalidation subscriber; retrying", "error", err, "retry_in", backoff)
				timer := time.NewTimer(backoff)
				select {
				case <-subscriberCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				if backoff < 30*time.Second {
					backoff *= 2
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
				}
			}
		}()
	})
}

func (s *APIKeyService) invalidateLocalAuthCache(cacheKey string) {
	if s == nil {
		return
	}
	if s.authCacheL1 != nil {
		s.authCacheL1.Del(cacheKey)
	}
	if s.authNegativeCacheL1 != nil {
		s.authNegativeCacheL1.Del(cacheKey)
	}
}

type AuthCacheInvalidationSubscriberHealth struct {
	Connected bool   `json:"connected"`
	Failures  uint64 `json:"failures"`
}

func (s *APIKeyService) AuthCacheInvalidationSubscriberHealth() AuthCacheInvalidationSubscriberHealth {
	if s == nil {
		return AuthCacheInvalidationSubscriberHealth{}
	}
	return AuthCacheInvalidationSubscriberHealth{
		Connected: s.authInvalidationConnected.Load(),
		Failures:  s.authInvalidationFailures.Load(),
	}
}

func (s *APIKeyService) StopAuthCacheInvalidationSubscriber() {
	if s == nil {
		return
	}
	s.authInvalidationStop.Do(func() {
		if s.authInvalidationCancel != nil {
			s.authInvalidationCancel()
		}
		s.authInvalidationWG.Wait()
	})
}

func (s *APIKeyService) authCacheKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *APIKeyService) getAuthCacheEntry(ctx context.Context, cacheKey string) (*APIKeyAuthCacheEntry, bool) {
	if s.authCacheL1 != nil {
		if val, ok := s.authCacheL1.Get(cacheKey); ok {
			if entry, ok := val.(*APIKeyAuthCacheEntry); ok {
				return entry, true
			}
		}
	}
	if s.authNegativeCacheL1 != nil {
		if val, ok := s.authNegativeCacheL1.Get(cacheKey); ok {
			if entry, ok := val.(*APIKeyAuthCacheEntry); ok && entry.NotFound {
				return entry, true
			}
		}
	}
	if s.cache == nil || !s.authCfg.l2Enabled() {
		return nil, false
	}
	entry, err := s.cache.GetAuthCache(ctx, cacheKey)
	if err != nil {
		return nil, false
	}
	s.setAuthCacheL1(cacheKey, entry)
	return entry, true
}

func (s *APIKeyService) setAuthCacheL1(cacheKey string, entry *APIKeyAuthCacheEntry) {
	if entry == nil {
		return
	}
	if entry.NotFound {
		if s.authNegativeCacheL1 != nil && s.authCfg.negativeTTL > 0 {
			_ = s.authNegativeCacheL1.SetWithTTL(cacheKey, entry, 1, s.authCfg.jitterTTL(s.authCfg.negativeTTL))
		}
		return
	}
	if s.authCacheL1 == nil {
		return
	}
	ttl := s.authCfg.l1TTL
	ttl = s.authCfg.jitterTTL(ttl)
	_ = s.authCacheL1.SetWithTTL(cacheKey, entry, 1, ttl)
}

func (s *APIKeyService) setAuthCacheEntry(ctx context.Context, cacheKey string, entry *APIKeyAuthCacheEntry, ttl time.Duration) {
	if entry == nil {
		return
	}
	s.setAuthCacheL1(cacheKey, entry)
	if s.cache == nil || !s.authCfg.l2Enabled() {
		return
	}
	_ = s.cache.SetAuthCache(ctx, cacheKey, entry, s.authCfg.jitterTTL(ttl))
}

func (s *APIKeyService) deleteAuthCache(ctx context.Context, cacheKey string) {
	if s.authCacheL1 != nil {
		s.authCacheL1.Del(cacheKey)
	}
	if s.authNegativeCacheL1 != nil {
		s.authNegativeCacheL1.Del(cacheKey)
	}
	if s.cache == nil {
		return
	}
	_ = s.cache.DeleteAuthCache(ctx, cacheKey)
	// Publish invalidation message to other instances
	_ = s.cache.PublishAuthCacheInvalidation(ctx, cacheKey)
}

func (s *APIKeyService) loadAuthCacheEntry(ctx context.Context, key, cacheKey string) (*APIKeyAuthCacheEntry, error) {
	apiKey, err := s.lookupAPIKeyForAuth(ctx, key)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			entry := &APIKeyAuthCacheEntry{NotFound: true}
			if s.authCfg.negativeEnabled() {
				// Invalid keys are attacker-controlled and high-cardinality. Keep their
				// negative entries in the bounded process-local cache; do not amplify
				// random-key scans into Redis writes on every instance.
				s.setAuthCacheL1(cacheKey, entry)
			}
			return entry, nil
		}
		return nil, fmt.Errorf("get api key: %w", err)
	}
	apiKey.Key = key
	if apiKey.Group != nil && apiKey.Group.MinimumBalance == 0 {
		if loader, ok := s.apiKeyRepo.(groupMinimumBalanceLoader); ok {
			group, loadErr := loader.GetGroupByIDForMinimumBalance(ctx, apiKey.Group.ID)
			if loadErr != nil {
				return nil, fmt.Errorf("get api key group minimum balance: %w", loadErr)
			}
			if group == nil {
				return nil, fmt.Errorf("get api key group minimum balance: %w", ErrGroupNotFound)
			}
			apiKey.Group.MinimumBalance = group.MinimumBalance
		}
	}
	snapshot := s.snapshotFromAPIKey(ctx, apiKey)
	if snapshot == nil {
		return nil, fmt.Errorf("get api key: %w", ErrAPIKeyNotFound)
	}
	entry := &APIKeyAuthCacheEntry{Snapshot: snapshot}
	s.setAuthCacheEntry(ctx, cacheKey, entry, s.authCfg.l2TTL)
	return entry, nil
}

func (s *APIKeyService) lookupAPIKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	if s == nil || s.apiKeyRepo == nil {
		return nil, ErrAPIKeyNotFound
	}
	if s.authLookupSlots == nil {
		return s.apiKeyRepo.GetByKeyForAuth(ctx, key)
	}
	s.authLookupTotal.Add(1)
	select {
	case s.authLookupSlots <- struct{}{}:
		s.authLookupInFlight.Add(1)
		defer func() {
			s.authLookupInFlight.Add(-1)
			<-s.authLookupSlots
		}()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		s.authLookupRejected.Add(1)
		return nil, ErrAPIKeyAuthOverloaded
	}
	return s.apiKeyRepo.GetByKeyForAuth(ctx, key)
}

func (s *APIKeyService) applyAuthCacheEntry(key string, entry *APIKeyAuthCacheEntry) (*APIKey, bool, error) {
	if entry == nil {
		return nil, false, nil
	}
	if entry.NotFound {
		return nil, true, ErrAPIKeyNotFound
	}
	if entry.Snapshot == nil {
		return nil, false, nil
	}
	if entry.Snapshot.Version != apiKeyAuthSnapshotVersion {
		return nil, false, nil
	}
	return s.snapshotToAPIKey(key, entry.Snapshot), true, nil
}

func (s *APIKeyService) snapshotFromAPIKey(ctx context.Context, apiKey *APIKey) *APIKeyAuthSnapshot {
	if apiKey == nil || apiKey.User == nil {
		return nil
	}
	snapshot := &APIKeyAuthSnapshot{
		Version:     apiKeyAuthSnapshotVersion,
		APIKeyID:    apiKey.ID,
		UserID:      apiKey.UserID,
		GroupID:     clonePtr(apiKey.GroupID),
		Name:        apiKey.Name,
		Status:      apiKey.Status,
		IPWhitelist: append([]string(nil), apiKey.IPWhitelist...),
		IPBlacklist: append([]string(nil), apiKey.IPBlacklist...),
		Quota:       apiKey.Quota,
		QuotaUsed:   apiKey.QuotaUsed,
		ExpiresAt:   clonePtr(apiKey.ExpiresAt),
		RateLimit5h: apiKey.RateLimit5h,
		RateLimit1d: apiKey.RateLimit1d,
		RateLimit7d: apiKey.RateLimit7d,
		User: APIKeyAuthUserSnapshot{
			ID:                         apiKey.User.ID,
			Status:                     apiKey.User.Status,
			Role:                       apiKey.User.Role,
			Balance:                    apiKey.User.Balance,
			Concurrency:                apiKey.User.Concurrency,
			AllowedGroups:              append([]int64(nil), apiKey.User.AllowedGroups...),
			Email:                      apiKey.User.Email,
			Username:                   apiKey.User.Username,
			BalanceNotifyEnabled:       apiKey.User.BalanceNotifyEnabled,
			BalanceNotifyThresholdType: apiKey.User.BalanceNotifyThresholdType,
			BalanceNotifyThreshold:     clonePtr(apiKey.User.BalanceNotifyThreshold),
			BalanceNotifyExtraEmails:   append([]NotifyEmailEntry(nil), apiKey.User.BalanceNotifyExtraEmails...),
			TotalRecharged:             apiKey.User.TotalRecharged,
			RPMLimit:                   apiKey.User.RPMLimit,
		},
	}

	groups := apiKey.Groups
	if len(groups) == 0 && apiKey.Group != nil {
		groups = []Group{*apiKey.Group}
	}
	subscriptions, subscriptionResolution := s.resolveAuthSubscriptions(ctx, apiKey, groups)
	snapshot.Groups = make([]APIKeyAuthGroupSnapshot, 0, len(groups))
	snapshot.GroupIDs = make([]int64, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		var override *int
		resolved := false
		if cachedResolved, stateKnown := apiKey.UserGroupRPMOverridesResolved[group.ID]; stateKnown {
			if cachedResolved {
				override = clonePtr(apiKey.UserGroupRPMOverrides[group.ID])
				resolved = true
			}
		} else if cached, ok := apiKey.UserGroupRPMOverrides[group.ID]; ok {
			// 兼容进程内旧对象：map 中存在键（包括 nil）即表示已经完成查询。
			override = clonePtr(cached)
			resolved = true
		}
		if !resolved && s.userGroupRateRepo != nil {
			loaded, err := s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, apiKey.UserID, group.ID)
			if err == nil {
				override = clonePtr(loaded)
				resolved = true
			}
		}
		groupSnapshot := authGroupSnapshotFromGroup(
			group,
			override,
			resolved,
			subscriptions[group.ID],
			subscriptionResolution[group.ID],
		)
		snapshot.Groups = append(snapshot.Groups, groupSnapshot)
		snapshot.GroupIDs = append(snapshot.GroupIDs, group.ID)
	}
	if len(snapshot.Groups) > 0 {
		primary := snapshot.Groups[0]
		snapshot.Group = &primary
		primaryID := primary.ID
		snapshot.GroupID = &primaryID
		snapshot.User.UserGroupRPMOverride = clonePtr(primary.UserGroupRPMOverride)
	}
	return snapshot
}

func (s *APIKeyService) snapshotToAPIKey(key string, snapshot *APIKeyAuthSnapshot) *APIKey {
	if snapshot == nil {
		return nil
	}
	apiKey := &APIKey{
		ID:          snapshot.APIKeyID,
		UserID:      snapshot.UserID,
		GroupID:     clonePtr(snapshot.GroupID),
		Key:         key,
		Name:        snapshot.Name,
		Status:      snapshot.Status,
		IPWhitelist: append([]string(nil), snapshot.IPWhitelist...),
		IPBlacklist: append([]string(nil), snapshot.IPBlacklist...),
		Quota:       snapshot.Quota,
		QuotaUsed:   snapshot.QuotaUsed,
		ExpiresAt:   clonePtr(snapshot.ExpiresAt),
		RateLimit5h: snapshot.RateLimit5h,
		RateLimit1d: snapshot.RateLimit1d,
		RateLimit7d: snapshot.RateLimit7d,
		User: &User{
			ID:                         snapshot.User.ID,
			Status:                     snapshot.User.Status,
			Role:                       snapshot.User.Role,
			Balance:                    snapshot.User.Balance,
			Concurrency:                snapshot.User.Concurrency,
			AllowedGroups:              append([]int64(nil), snapshot.User.AllowedGroups...),
			Email:                      snapshot.User.Email,
			Username:                   snapshot.User.Username,
			BalanceNotifyEnabled:       snapshot.User.BalanceNotifyEnabled,
			BalanceNotifyThresholdType: snapshot.User.BalanceNotifyThresholdType,
			BalanceNotifyThreshold:     clonePtr(snapshot.User.BalanceNotifyThreshold),
			BalanceNotifyExtraEmails:   append([]NotifyEmailEntry(nil), snapshot.User.BalanceNotifyExtraEmails...),
			TotalRecharged:             snapshot.User.TotalRecharged,
			RPMLimit:                   snapshot.User.RPMLimit,
		},
		UserGroupRPMOverrides:         make(map[int64]*int, len(snapshot.Groups)),
		UserGroupRPMOverridesResolved: make(map[int64]bool, len(snapshot.Groups)),
		GroupSubscriptions:            make(map[int64]*UserSubscription, len(snapshot.Groups)),
		GroupSubscriptionsResolved:    make(map[int64]bool, len(snapshot.Groups)),
	}
	groupSnapshots := snapshot.Groups
	if len(groupSnapshots) == 0 && snapshot.Group != nil {
		groupSnapshots = []APIKeyAuthGroupSnapshot{*snapshot.Group}
	}
	apiKey.Groups = make([]Group, 0, len(groupSnapshots))
	apiKey.GroupIDs = make([]int64, 0, len(groupSnapshots))
	for i := range groupSnapshots {
		group := groupFromAuthSnapshot(&groupSnapshots[i])
		apiKey.Groups = append(apiKey.Groups, *group)
		apiKey.GroupIDs = append(apiKey.GroupIDs, group.ID)
		apiKey.UserGroupRPMOverrides[group.ID] = clonePtr(groupSnapshots[i].UserGroupRPMOverride)
		apiKey.UserGroupRPMOverridesResolved[group.ID] = groupSnapshots[i].UserGroupRPMOverrideResolved
		apiKey.GroupSubscriptions[group.ID] = subscriptionFromAuthSnapshot(groupSnapshots[i].Subscription)
		apiKey.GroupSubscriptionsResolved[group.ID] = groupSnapshots[i].SubscriptionResolved
	}
	if len(apiKey.Groups) > 0 {
		primary := apiKey.Groups[0]
		apiKey.Group = &primary
		primaryID := primary.ID
		apiKey.GroupID = &primaryID
		apiKey.User.UserGroupRPMOverride = clonePtr(apiKey.UserGroupRPMOverrides[primaryID])
		apiKey.User.UserGroupRPMOverrideResolved = apiKey.UserGroupRPMOverridesResolved[primaryID]
	}
	s.compileAPIKeyIPRules(apiKey)
	return apiKey
}

func authGroupSnapshotFromGroup(
	group *Group,
	override *int,
	overrideResolved bool,
	subscription *UserSubscription,
	subscriptionResolved bool,
) APIKeyAuthGroupSnapshot {
	if group == nil {
		return APIKeyAuthGroupSnapshot{}
	}
	return APIKeyAuthGroupSnapshot{
		ID:                              group.ID,
		Name:                            group.Name,
		Platform:                        group.Platform,
		IsExclusive:                     group.IsExclusive,
		Status:                          group.Status,
		SubscriptionType:                group.SubscriptionType,
		RateMultiplier:                  group.RateMultiplier,
		MinimumBalance:                  group.MinimumBalance,
		DailyLimitUSD:                   clonePtr(group.DailyLimitUSD),
		WeeklyLimitUSD:                  clonePtr(group.WeeklyLimitUSD),
		MonthlyLimitUSD:                 clonePtr(group.MonthlyLimitUSD),
		AllowImageGeneration:            group.AllowImageGeneration,
		AllowBatchImageGeneration:       group.AllowBatchImageGeneration,
		ImageRateIndependent:            group.ImageRateIndependent,
		ImageRateMultiplier:             group.ImageRateMultiplier,
		BatchImageDiscountMultiplier:    group.BatchImageDiscountMultiplier,
		BatchImageHoldMultiplier:        group.BatchImageHoldMultiplier,
		ImagePrice1K:                    clonePtr(group.ImagePrice1K),
		ImagePrice2K:                    clonePtr(group.ImagePrice2K),
		ImagePrice4K:                    clonePtr(group.ImagePrice4K),
		VideoRateIndependent:            group.VideoRateIndependent,
		VideoRateMultiplier:             group.VideoRateMultiplier,
		VideoPrice480P:                  clonePtr(group.VideoPrice480P),
		VideoPrice720P:                  clonePtr(group.VideoPrice720P),
		VideoPrice1080P:                 clonePtr(group.VideoPrice1080P),
		WebSearchPricePerCall:           clonePtr(group.WebSearchPricePerCall),
		ClaudeCodeOnly:                  group.ClaudeCodeOnly,
		FallbackGroupID:                 clonePtr(group.FallbackGroupID),
		FallbackGroupIDOnInvalidRequest: clonePtr(group.FallbackGroupIDOnInvalidRequest),
		ModelRouting:                    cloneModelRouting(group.ModelRouting),
		ModelRoutingEnabled:             group.ModelRoutingEnabled,
		MCPXMLInject:                    group.MCPXMLInject,
		SupportedModelScopes:            append([]string(nil), group.SupportedModelScopes...),
		AllowMessagesDispatch:           group.AllowMessagesDispatch,
		AllowLive:                       group.AllowLive,
		RequireOAuthOnly:                group.RequireOAuthOnly,
		RequirePrivacySet:               group.RequirePrivacySet,
		DefaultMappedModel:              group.DefaultMappedModel,
		MessagesDispatchModelConfig:     cloneMessagesDispatchModelConfig(group.MessagesDispatchModelConfig),
		ModelsListConfig:                cloneGroupModelsListConfig(group.ModelsListConfig),
		RPMLimit:                        group.RPMLimit,
		MaxReasoningEffort:              group.MaxReasoningEffort,
		ReasoningEffortMappings:         append([]ReasoningEffortMapping(nil), group.ReasoningEffortMappings...),
		PeakRateEnabled:                 group.PeakRateEnabled,
		PeakStart:                       group.PeakStart,
		PeakEnd:                         group.PeakEnd,
		PeakRateMultiplier:              group.PeakRateMultiplier,
		UserGroupRPMOverride:            clonePtr(override),
		UserGroupRPMOverrideResolved:    overrideResolved,
		Subscription:                    authSubscriptionSnapshotFromSubscription(subscription),
		SubscriptionResolved:            subscriptionResolved,
	}
}

func (s *APIKeyService) resolveAuthSubscriptions(
	ctx context.Context,
	apiKey *APIKey,
	groups []Group,
) (map[int64]*UserSubscription, map[int64]bool) {
	subscriptions := make(map[int64]*UserSubscription, len(groups))
	resolved := make(map[int64]bool, len(groups))
	needsLookup := false
	for i := range groups {
		groupID := groups[i].ID
		if state, known := apiKey.GroupSubscriptionsResolved[groupID]; known {
			if state {
				subscriptions[groupID] = cloneUserSubscription(apiKey.GroupSubscriptions[groupID])
				resolved[groupID] = true
			}
		} else if cached, ok := apiKey.GroupSubscriptions[groupID]; ok {
			// 兼容同一进程内尚未携带解析状态的对象。
			subscriptions[groupID] = cloneUserSubscription(cached)
			resolved[groupID] = true
		}
		if groups[i].IsSubscriptionType() && !resolved[groupID] {
			needsLookup = true
		}
	}
	if !needsLookup || s.userSubRepo == nil {
		return subscriptions, resolved
	}

	active, err := s.userSubRepo.ListActiveByUserID(ctx, apiKey.UserID)
	if err != nil {
		// 查询失败保持 unresolved，实际尝试候选时再回源，避免缓存瞬时故障。
		return subscriptions, resolved
	}
	activeByGroup := make(map[int64]*UserSubscription, len(active))
	for i := range active {
		subscription := active[i]
		if _, exists := activeByGroup[subscription.GroupID]; !exists {
			activeByGroup[subscription.GroupID] = &subscription
		}
	}
	for i := range groups {
		groupID := groups[i].ID
		if !groups[i].IsSubscriptionType() || resolved[groupID] {
			continue
		}
		subscriptions[groupID] = cloneUserSubscription(activeByGroup[groupID])
		resolved[groupID] = true
	}
	return subscriptions, resolved
}

func authSubscriptionSnapshotFromSubscription(subscription *UserSubscription) *APIKeyAuthSubscriptionSnapshot {
	if subscription == nil {
		return nil
	}
	return &APIKeyAuthSubscriptionSnapshot{
		ID:                 subscription.ID,
		UserID:             subscription.UserID,
		GroupID:            subscription.GroupID,
		StartsAt:           subscription.StartsAt,
		ExpiresAt:          subscription.ExpiresAt,
		Status:             subscription.Status,
		DailyWindowStart:   clonePtr(subscription.DailyWindowStart),
		WeeklyWindowStart:  clonePtr(subscription.WeeklyWindowStart),
		MonthlyWindowStart: clonePtr(subscription.MonthlyWindowStart),
	}
}

func subscriptionFromAuthSnapshot(snapshot *APIKeyAuthSubscriptionSnapshot) *UserSubscription {
	if snapshot == nil {
		return nil
	}
	return &UserSubscription{
		ID:                 snapshot.ID,
		UserID:             snapshot.UserID,
		GroupID:            snapshot.GroupID,
		StartsAt:           snapshot.StartsAt,
		ExpiresAt:          snapshot.ExpiresAt,
		Status:             snapshot.Status,
		DailyWindowStart:   clonePtr(snapshot.DailyWindowStart),
		WeeklyWindowStart:  clonePtr(snapshot.WeeklyWindowStart),
		MonthlyWindowStart: clonePtr(snapshot.MonthlyWindowStart),
	}
}

func cloneUserSubscription(source *UserSubscription) *UserSubscription {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.DailyWindowStart = clonePtr(source.DailyWindowStart)
	cloned.WeeklyWindowStart = clonePtr(source.WeeklyWindowStart)
	cloned.MonthlyWindowStart = clonePtr(source.MonthlyWindowStart)
	cloned.AssignedBy = clonePtr(source.AssignedBy)
	cloned.DeletedAt = clonePtr(source.DeletedAt)
	cloned.User = nil
	cloned.Group = nil
	cloned.AssignedByUser = nil
	return &cloned
}

func groupFromAuthSnapshot(snapshot *APIKeyAuthGroupSnapshot) *Group {
	if snapshot == nil {
		return nil
	}
	return &Group{
		ID:                              snapshot.ID,
		Name:                            snapshot.Name,
		Platform:                        snapshot.Platform,
		IsExclusive:                     snapshot.IsExclusive,
		Status:                          snapshot.Status,
		Hydrated:                        true,
		SubscriptionType:                snapshot.SubscriptionType,
		RateMultiplier:                  snapshot.RateMultiplier,
		MinimumBalance:                  snapshot.MinimumBalance,
		DailyLimitUSD:                   clonePtr(snapshot.DailyLimitUSD),
		WeeklyLimitUSD:                  clonePtr(snapshot.WeeklyLimitUSD),
		MonthlyLimitUSD:                 clonePtr(snapshot.MonthlyLimitUSD),
		AllowImageGeneration:            snapshot.AllowImageGeneration,
		AllowBatchImageGeneration:       snapshot.AllowBatchImageGeneration,
		ImageRateIndependent:            snapshot.ImageRateIndependent,
		ImageRateMultiplier:             snapshot.ImageRateMultiplier,
		BatchImageDiscountMultiplier:    snapshot.BatchImageDiscountMultiplier,
		BatchImageHoldMultiplier:        snapshot.BatchImageHoldMultiplier,
		ImagePrice1K:                    clonePtr(snapshot.ImagePrice1K),
		ImagePrice2K:                    clonePtr(snapshot.ImagePrice2K),
		ImagePrice4K:                    clonePtr(snapshot.ImagePrice4K),
		VideoRateIndependent:            snapshot.VideoRateIndependent,
		VideoRateMultiplier:             snapshot.VideoRateMultiplier,
		VideoPrice480P:                  clonePtr(snapshot.VideoPrice480P),
		VideoPrice720P:                  clonePtr(snapshot.VideoPrice720P),
		VideoPrice1080P:                 clonePtr(snapshot.VideoPrice1080P),
		WebSearchPricePerCall:           clonePtr(snapshot.WebSearchPricePerCall),
		ClaudeCodeOnly:                  snapshot.ClaudeCodeOnly,
		FallbackGroupID:                 clonePtr(snapshot.FallbackGroupID),
		FallbackGroupIDOnInvalidRequest: clonePtr(snapshot.FallbackGroupIDOnInvalidRequest),
		ModelRouting:                    cloneModelRouting(snapshot.ModelRouting),
		ModelRoutingEnabled:             snapshot.ModelRoutingEnabled,
		MCPXMLInject:                    snapshot.MCPXMLInject,
		SupportedModelScopes:            append([]string(nil), snapshot.SupportedModelScopes...),
		AllowMessagesDispatch:           snapshot.AllowMessagesDispatch,
		AllowLive:                       snapshot.AllowLive,
		RequireOAuthOnly:                snapshot.RequireOAuthOnly,
		RequirePrivacySet:               snapshot.RequirePrivacySet,
		DefaultMappedModel:              snapshot.DefaultMappedModel,
		MessagesDispatchModelConfig:     cloneMessagesDispatchModelConfig(snapshot.MessagesDispatchModelConfig),
		ModelsListConfig:                cloneGroupModelsListConfig(snapshot.ModelsListConfig),
		RPMLimit:                        snapshot.RPMLimit,
		MaxReasoningEffort:              snapshot.MaxReasoningEffort,
		ReasoningEffortMappings:         append([]ReasoningEffortMapping(nil), snapshot.ReasoningEffortMappings...),
		PeakRateEnabled:                 snapshot.PeakRateEnabled,
		PeakStart:                       snapshot.PeakStart,
		PeakEnd:                         snapshot.PeakEnd,
		PeakRateMultiplier:              snapshot.PeakRateMultiplier,
	}
}

func cloneModelRouting(source map[string][]int64) map[string][]int64 {
	if source == nil {
		return nil
	}
	cloned := make(map[string][]int64, len(source))
	for model, accountIDs := range source {
		cloned[model] = append([]int64(nil), accountIDs...)
	}
	return cloned
}

func cloneMessagesDispatchModelConfig(source OpenAIMessagesDispatchModelConfig) OpenAIMessagesDispatchModelConfig {
	cloned := source
	if source.ExactModelMappings != nil {
		cloned.ExactModelMappings = make(map[string]string, len(source.ExactModelMappings))
		for model, mappedModel := range source.ExactModelMappings {
			cloned.ExactModelMappings[model] = mappedModel
		}
	}
	return cloned
}

func cloneGroupModelsListConfig(source GroupModelsListConfig) GroupModelsListConfig {
	cloned := source
	cloned.Models = append([]string(nil), source.Models...)
	return cloned
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
