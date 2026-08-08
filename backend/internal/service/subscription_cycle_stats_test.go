package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/stretchr/testify/require"
)

type cycleAwareSubscriptionRepo struct {
	userSubRepoNoop
	sub                 UserSubscription
	appendedCycleStarts time.Time
	appendedCycleEnds   time.Time
	advanceCalls        int
	resetCalls          int
}

func (r *cycleAwareSubscriptionRepo) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) GetByIDForUpdate(context.Context, int64) (*UserSubscription, error) {
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) AppendRenewalCycle(_ context.Context, _ int64, startsAt, endsAt time.Time, _ string, _ *string) error {
	r.appendedCycleStarts = startsAt
	r.appendedCycleEnds = endsAt
	r.sub.ExpiresAt = endsAt
	return nil
}

func (r *cycleAwareSubscriptionRepo) RenewExpiredWithCycle(_ context.Context, sub *UserSubscription) error {
	r.sub = *sub
	return nil
}

func (r *cycleAwareSubscriptionRepo) AdvanceCycle(_ context.Context, _ int64, now, dailyStart time.Time) (bool, error) {
	r.advanceCalls++
	if now.Before(r.sub.CurrentCycleEndsAt) || !r.sub.ExpiresAt.After(r.sub.CurrentCycleEndsAt) {
		return false, nil
	}
	nextStartsAt := r.sub.CurrentCycleEndsAt
	r.sub.CurrentCycleStartsAt = nextStartsAt
	r.sub.CurrentCycleEndsAt = r.sub.ExpiresAt
	r.sub.DailyUsageUSD = 0
	r.sub.WeeklyUsageUSD = 0
	r.sub.MonthlyUsageUSD = 0
	r.sub.CycleUsageUSD = 0
	r.sub.ManualQuotaResetCount = 0
	r.sub.DailyWindowStart = &dailyStart
	r.sub.WeeklyWindowStart = &now
	r.sub.MonthlyWindowStart = &now
	return true, nil
}

func (r *cycleAwareSubscriptionRepo) CaptureTermSnapshot(context.Context, int64) (*subscriptionquota.TermSnapshot, error) {
	return &subscriptionquota.TermSnapshot{
		SubscriptionID:       r.sub.ID,
		UserID:               r.sub.UserID,
		GroupID:              r.sub.GroupID,
		ExpiresAt:            r.sub.ExpiresAt,
		Status:               r.sub.Status,
		CurrentCycleStartsAt: r.sub.CurrentCycleStartsAt,
		CurrentCycleEndsAt:   r.sub.CurrentCycleEndsAt,
	}, nil
}

func (r *cycleAwareSubscriptionRepo) AdjustExpiryWithSnapshot(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) (*subscriptionquota.TermSnapshot, error) {
	snapshot, err := r.CaptureTermSnapshot(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	r.sub.ExpiresAt = newExpiresAt
	r.sub.CurrentCycleEndsAt = newExpiresAt
	snapshot.Expected = &subscriptionquota.TermExpectedState{
		ExpiresAt:            newExpiresAt,
		Status:               r.sub.Status,
		DeletedAt:            r.sub.DeletedAt,
		CurrentCycleStartsAt: r.sub.CurrentCycleStartsAt,
		CurrentCycleEndsAt:   r.sub.CurrentCycleEndsAt,
	}
	return snapshot, nil
}

func (r *cycleAwareSubscriptionRepo) RestoreTermSnapshot(_ context.Context, snapshot *subscriptionquota.TermSnapshot) error {
	r.sub.ExpiresAt = snapshot.ExpiresAt
	r.sub.Status = snapshot.Status
	r.sub.CurrentCycleStartsAt = snapshot.CurrentCycleStartsAt
	r.sub.CurrentCycleEndsAt = snapshot.CurrentCycleEndsAt
	return nil
}

func (r *cycleAwareSubscriptionRepo) ResetUsageWindows(_ context.Context, _ int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	r.resetCalls++
	r.sub.ManualQuotaResetCount++
	if resetDaily {
		r.sub.DailyUsageUSD = 0
		r.sub.DailyWindowStart = &dailyStart
	}
	if resetWeekly {
		r.sub.WeeklyUsageUSD = 0
		r.sub.WeeklyWindowStart = &periodicStart
	}
	if resetMonthly {
		r.sub.MonthlyUsageUSD = 0
		r.sub.MonthlyWindowStart = &periodicStart
	}
	return nil
}

func (r *cycleAwareSubscriptionRepo) UpdateStatus(_ context.Context, _ int64, status string) error {
	r.sub.Status = status
	return nil
}

func (r *cycleAwareSubscriptionRepo) UpdateNotes(_ context.Context, _ int64, notes string) error {
	r.sub.Notes = notes
	return nil
}

func (r *cycleAwareSubscriptionRepo) RenewExistingTerm(_ context.Context, _ int64, validityDays int, _ string, _ string, _ *string, _ bool, _ time.Time, _ time.Time) error {
	return r.AppendRenewalCycle(context.Background(), r.sub.ID, r.sub.ExpiresAt, r.sub.ExpiresAt.AddDate(0, 0, validityDays), "", nil)
}

func (r *cycleAwareSubscriptionRepo) AdjustTerm(ctx context.Context, subscriptionID int64, days int, captureSnapshot, _ bool, _ time.Time, _ time.Time, _ int) (*UserSubscription, *subscriptionquota.TermSnapshot, error) {
	newExpiresAt := r.sub.ExpiresAt.AddDate(0, 0, days)
	var snapshot *subscriptionquota.TermSnapshot
	var err error
	if captureSnapshot {
		snapshot, err = r.AdjustExpiryWithSnapshot(ctx, subscriptionID, newExpiresAt)
	} else {
		r.sub.ExpiresAt = newExpiresAt
		r.sub.CurrentCycleEndsAt = newExpiresAt
	}
	copy := r.sub
	return &copy, snapshot, err
}

func (r *cycleAwareSubscriptionRepo) RestoreTermSnapshotExact(ctx context.Context, snapshot *subscriptionquota.TermSnapshot) (*UserSubscription, error) {
	if err := r.RestoreTermSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) ResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool, _ *idempotencyexecution.Execution, now time.Time) (*UserSubscription, error) {
	if _, err := r.AdvanceCycle(ctx, subscriptionID, now, now); err != nil {
		return nil, err
	}
	if err := r.ResetUsageWindows(ctx, subscriptionID, resetDaily, resetWeekly, resetMonthly, now, now); err != nil {
		return nil, err
	}
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription, now time.Time) (*UserSubscription, error) {
	if _, err := r.AdvanceCycle(ctx, sub.ID, now, now); err != nil {
		return nil, err
	}
	copy := r.sub
	return &copy, nil
}

func (r *cycleAwareSubscriptionRepo) NeedsCycleAdvance(sub *UserSubscription, now time.Time) bool {
	return sub != nil && subscriptionquota.NeedsAdvance(sub.CurrentCycleEndsAt, sub.ExpiresAt, now)
}

func TestEarlyRenewalKeepsCurrentCycleStatisticsUntilBoundary(t *testing.T) {
	startsAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	currentEndsAt := startsAt.AddDate(0, 0, 30)
	now := startsAt.AddDate(0, 0, 25)
	repo := &cycleAwareSubscriptionRepo{sub: UserSubscription{
		ID: 1, UserID: 2, GroupID: 3,
		StartsAt: startsAt, ExpiresAt: currentEndsAt, Status: SubscriptionStatusActive,
		CurrentCycleStartsAt: startsAt, CurrentCycleEndsAt: currentEndsAt,
		CycleUsageUSD: 42.5, ManualQuotaResetCount: 2,
	}}
	svc := NewSubscriptionService(
		&subscriptionGroupRepoStub{group: &Group{ID: 3, SubscriptionType: SubscriptionTypeSubscription}},
		repo,
		nil,
		nil,
		nil,
	)
	svc.now = func() time.Time { return now }

	got, renewed, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 2, GroupID: 3, ValidityDays: 30,
	})

	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, currentEndsAt, repo.appendedCycleStarts)
	require.Equal(t, currentEndsAt.AddDate(0, 0, 30), repo.appendedCycleEnds)
	require.Equal(t, currentEndsAt, got.CurrentCycleEndsAt)
	require.Equal(t, 42.5, got.CycleUsageUSD)
	require.Equal(t, int64(2), got.ManualQuotaResetCount)
}

func TestCycleBoundaryClearsStatisticsOnlyWhenNewCycleStarts(t *testing.T) {
	startsAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	currentEndsAt := startsAt.AddDate(0, 0, 30)
	nextEndsAt := currentEndsAt.AddDate(0, 0, 30)
	now := currentEndsAt
	windowStart := startsAt
	repo := &cycleAwareSubscriptionRepo{sub: UserSubscription{
		ID: 11, UserID: 12, GroupID: 13,
		StartsAt: startsAt, ExpiresAt: nextEndsAt, Status: SubscriptionStatusActive,
		CurrentCycleStartsAt: startsAt, CurrentCycleEndsAt: currentEndsAt,
		DailyWindowStart: &windowStart, WeeklyWindowStart: &windowStart, MonthlyWindowStart: &windowStart,
		DailyUsageUSD: 20, WeeklyUsageUSD: 20, MonthlyUsageUSD: 20,
		CycleUsageUSD: 55.5, ManualQuotaResetCount: 3,
	}}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	limit := 10.0
	snapshot := repo.sub

	needsMaintenance, err := svc.ValidateAndCheckLimits(&snapshot, &Group{
		DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit,
	})
	require.NoError(t, err)
	require.True(t, needsMaintenance)

	got, err := svc.EnsureWindowMaintenance(context.Background(), &snapshot)
	require.NoError(t, err)
	require.Equal(t, 1, repo.advanceCalls)
	require.Equal(t, currentEndsAt, got.CurrentCycleStartsAt)
	require.Equal(t, nextEndsAt, got.CurrentCycleEndsAt)
	require.Zero(t, got.CycleUsageUSD)
	require.Zero(t, got.ManualQuotaResetCount)
	require.Zero(t, got.DailyUsageUSD)
}

func TestAdminResetQuotaAtCycleBoundaryCountsResetInNewCycle(t *testing.T) {
	startsAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	boundary := startsAt.AddDate(0, 0, 30)
	nextEndsAt := boundary.AddDate(0, 0, 30)
	repo := &cycleAwareSubscriptionRepo{sub: UserSubscription{
		ID: 21, UserID: 22, GroupID: 23,
		StartsAt: startsAt, ExpiresAt: nextEndsAt, Status: SubscriptionStatusActive,
		CurrentCycleStartsAt: startsAt, CurrentCycleEndsAt: boundary,
		CycleUsageUSD: 88.5, ManualQuotaResetCount: 4,
	}}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return boundary }

	got, err := svc.AdminResetQuota(context.Background(), repo.sub.ID, true, true, true)

	require.NoError(t, err)
	require.Equal(t, 1, repo.advanceCalls)
	require.Equal(t, 1, repo.resetCalls)
	require.Equal(t, boundary, got.CurrentCycleStartsAt)
	require.Equal(t, nextEndsAt, got.CurrentCycleEndsAt)
	require.Zero(t, got.CycleUsageUSD)
	require.Equal(t, int64(1), got.ManualQuotaResetCount)
}
