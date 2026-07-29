//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom/groupaccess"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type balanceEligibilityCacheStub struct {
	billingCacheWorkerStub

	balance                  float64
	cacheMissAfterInvalidate bool
	invalidated              atomic.Bool
	deductCalls              atomic.Int64
	invalidateCalls          atomic.Int64
}

type minimumBalanceUserRepoStub struct {
	*userRepoStub
	balance      float64
	getByIDCalls atomic.Int64
}

type billingMinimumBalanceAPIKeyRepoStub struct {
	APIKeyRepository
	groups       map[int64]*Group
	getByIDCalls atomic.Int64
}

func (s *minimumBalanceUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	s.getByIDCalls.Add(1)
	return &User{ID: id, Balance: s.balance}, nil
}

func (s *billingMinimumBalanceAPIKeyRepoStub) GetGroupByIDForMinimumBalance(_ context.Context, id int64) (*Group, error) {
	s.getByIDCalls.Add(1)
	group, ok := s.groups[id]
	if !ok {
		return nil, errors.New("group not found")
	}
	return group, nil
}

func (s *balanceEligibilityCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	if s.cacheMissAfterInvalidate && s.invalidated.Load() {
		return 0, errors.New("cache miss")
	}
	return s.balance, nil
}

func (s *balanceEligibilityCacheStub) DeductUserBalance(context.Context, int64, float64) error {
	s.deductCalls.Add(1)
	return nil
}

func (s *balanceEligibilityCacheStub) InvalidateUserBalance(context.Context, int64) error {
	s.invalidateCalls.Add(1)
	s.invalidated.Store(true)
	return nil
}

func TestCheckBillingEligibility_RejectsBalanceBelowMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.005}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestCheckBillingEligibility_AllowsBalanceAtMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.01}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.NoError(t, err)
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesExhaustedBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{
		balance:                  0.50,
		cacheMissAfterInvalidate: true,
	}
	userRepo := &balanceLoadUserRepoStub{balance: -0.25}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := -0.25
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.75},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{
		NewBalance:         &newBalance,
		BalanceOverdrafted: true,
	})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), userRepo.calls.Load())
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesWhenBalanceFallsBelowReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.50}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.005
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.495},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())
}

func TestSyncBalanceCacheAfterDeduction_QueuesDeductWhenBalanceStillEligible(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 1}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.75
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.25},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(0), cache.invalidateCalls.Load())
	require.Eventually(t, func() bool {
		return cache.deductCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestCheckBillingEligibility_GroupMinimumBalanceUsesFreshBalance(t *testing.T) {
	userRepo := &minimumBalanceUserRepoStub{userRepoStub: &userRepoStub{}, balance: 100.01}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := NewBillingCacheService(&balanceEligibilityCacheStub{}, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	userSnapshot := &User{ID: 42, Balance: 999}
	group := &Group{ID: 7, Name: "分组 A", MinimumBalance: 100}

	require.NoError(t, svc.CheckBillingEligibility(context.Background(), userSnapshot, nil, group, nil, ""))

	userRepo.balance = 100
	err := svc.CheckBillingEligibility(context.Background(), userSnapshot, nil, group, nil, "")
	require.Equal(t, groupaccess.MinimumBalanceNotMetReason, pkgerrors.Reason(err))

	userRepo.balance = 80
	err = svc.CheckBillingEligibility(context.Background(), userSnapshot, nil, group, nil, "")
	require.Equal(t, groupaccess.MinimumBalanceNotMetReason, pkgerrors.Reason(err))

	userRepo.balance = 120
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), userSnapshot, nil, group, nil, ""))
	require.Equal(t, int64(4), userRepo.getByIDCalls.Load())
}

func TestCheckBillingEligibility_GroupMinimumBalanceDisabledSkipsFreshBalanceQuery(t *testing.T) {
	userRepo := &minimumBalanceUserRepoStub{userRepoStub: &userRepoStub{}, balance: 0}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := NewBillingCacheService(&balanceEligibilityCacheStub{}, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 42, Balance: 0},
		nil,
		&Group{ID: 7, Name: "普通分组", MinimumBalance: 0},
		nil,
		"",
	)
	require.NoError(t, err)
	require.Zero(t, userRepo.getByIDCalls.Load())
}

func TestCheckBillingEligibility_FallbackGroupMinimumBalanceUsesFreshBalance(t *testing.T) {
	fallbackID := int64(8)
	userRepo := &minimumBalanceUserRepoStub{userRepoStub: &userRepoStub{}, balance: 80}
	apiKeyRepo := &billingMinimumBalanceAPIKeyRepoStub{
		groups: map[int64]*Group{
			fallbackID: {
				ID:             fallbackID,
				Name:           "Fallback Group",
				MinimumBalance: 100,
			},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := NewBillingCacheService(
		&balanceEligibilityCacheStub{},
		userRepo,
		nil,
		apiKeyRepo,
		nil,
		nil,
		cfg,
		nil,
	)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 42, Balance: 999},
		nil,
		&Group{
			ID:              7,
			Name:            "Primary Group",
			ClaudeCodeOnly:  true,
			FallbackGroupID: &fallbackID,
		},
		nil,
		"",
	)

	require.Error(t, err)
	require.Equal(t, groupaccess.MinimumBalanceNotMetReason, pkgerrors.Reason(err))
	require.Equal(t, int64(1), apiKeyRepo.getByIDCalls.Load())
	require.Equal(t, int64(1), userRepo.getByIDCalls.Load())
}

func TestCheckBillingEligibility_DisabledFallbackGateSkipsFreshBalanceQuery(t *testing.T) {
	fallbackID := int64(8)
	userRepo := &minimumBalanceUserRepoStub{userRepoStub: &userRepoStub{}, balance: 0}
	apiKeyRepo := &billingMinimumBalanceAPIKeyRepoStub{
		groups: map[int64]*Group{
			fallbackID: {
				ID:             fallbackID,
				Name:           "Fallback Group",
				MinimumBalance: 0,
			},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := NewBillingCacheService(
		&balanceEligibilityCacheStub{},
		userRepo,
		nil,
		apiKeyRepo,
		nil,
		nil,
		cfg,
		nil,
	)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 42, Balance: 0},
		nil,
		&Group{
			ID:              7,
			Name:            "Primary Group",
			ClaudeCodeOnly:  true,
			FallbackGroupID: &fallbackID,
		},
		nil,
		"",
	)

	require.NoError(t, err)
	require.Equal(t, int64(1), apiKeyRepo.getByIDCalls.Load())
	require.Zero(t, userRepo.getByIDCalls.Load())
}
