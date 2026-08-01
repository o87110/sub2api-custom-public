package channelmonitor

import (
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/custom/groupaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// runtimeEligibleForGroupRate applies the request-independent runtime gates
// shared by every local key before its default group rate can be displayed.
// Request-specific IP, endpoint and model checks intentionally remain outside
// this read-only projection.
func runtimeEligibleForGroupRate(apiKey *dbent.APIKey, now time.Time) bool {
	if apiKey == nil || apiKey.Status != service.StatusAPIKeyActive {
		return false
	}
	if apiKey.ExpiresAt != nil && now.After(*apiKey.ExpiresAt) {
		return false
	}
	if apiKey.Quota > 0 && apiKey.QuotaUsed >= apiKey.Quota {
		return false
	}

	currentUser := apiKey.Edges.User
	currentGroup := apiKey.Edges.Group
	if currentUser == nil || currentUser.Status != service.StatusActive || currentGroup == nil || currentGroup.Status != service.StatusActive {
		return false
	}
	if currentGroup.SubscriptionType == service.SubscriptionTypeSubscription {
		if !hasRuntimeEligibleSubscription(currentUser, currentGroup, now) {
			return false
		}
	} else {
		if currentUser.Balance <= 0 || !hasGroupAccess(currentUser, currentGroup) {
			return false
		}
	}

	return groupaccess.EvaluateMinimumBalance(currentUser.Balance, currentGroup.MinimumBalance).Eligible
}

func hasGroupAccess(currentUser *dbent.User, currentGroup *dbent.Group) bool {
	if currentUser == nil || currentGroup == nil || !currentGroup.IsExclusive {
		return true
	}
	for _, allowedGroup := range currentUser.Edges.AllowedGroups {
		if allowedGroup != nil && allowedGroup.ID == currentGroup.ID {
			return true
		}
	}
	return false
}

func hasRuntimeEligibleSubscription(currentUser *dbent.User, currentGroup *dbent.Group, now time.Time) bool {
	if currentUser == nil || currentGroup == nil {
		return false
	}
	for _, subscription := range currentUser.Edges.Subscriptions {
		if subscriptionWithinRuntimeLimits(subscription, currentGroup, now) {
			return true
		}
	}
	return false
}

func subscriptionWithinRuntimeLimits(subscription *dbent.UserSubscription, currentGroup *dbent.Group, now time.Time) bool {
	if subscription == nil || currentGroup == nil || subscription.GroupID != currentGroup.ID ||
		subscription.Status != service.SubscriptionStatusActive || !now.Before(subscription.ExpiresAt) {
		return false
	}

	projectedSubscription := &service.UserSubscription{
		StartsAt:           subscription.StartsAt,
		ExpiresAt:          subscription.ExpiresAt,
		Status:             subscription.Status,
		DailyWindowStart:   subscription.DailyWindowStart,
		WeeklyWindowStart:  subscription.WeeklyWindowStart,
		MonthlyWindowStart: subscription.MonthlyWindowStart,
		DailyUsageUSD:      subscription.DailyUsageUsd,
		WeeklyUsageUSD:     subscription.WeeklyUsageUsd,
		MonthlyUsageUSD:    subscription.MonthlyUsageUsd,
	}
	if projectedSubscription.NeedsDailyResetAt(now) {
		projectedSubscription.DailyUsageUSD = 0
	}
	if subscriptionWindowExpired(projectedSubscription.WeeklyWindowStart, now, 7*24*time.Hour) {
		projectedSubscription.WeeklyUsageUSD = 0
	}
	if subscriptionWindowExpired(projectedSubscription.MonthlyWindowStart, now, 30*24*time.Hour) {
		projectedSubscription.MonthlyUsageUSD = 0
	}

	projectedGroup := &service.Group{
		DailyLimitUSD:   currentGroup.DailyLimitUsd,
		WeeklyLimitUSD:  currentGroup.WeeklyLimitUsd,
		MonthlyLimitUSD: currentGroup.MonthlyLimitUsd,
	}
	daily, weekly, monthly := projectedSubscription.CheckAllLimits(projectedGroup, 0)
	return daily && weekly && monthly
}

func subscriptionWindowExpired(windowStart *time.Time, now time.Time, duration time.Duration) bool {
	return windowStart != nil && !now.Before(windowStart.Add(duration))
}
