package subscriptionquota

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/ent/usersubscriptioncycle"
)

var ErrTermSnapshotStale = errors.New("subscription term snapshot is stale")

type TermCycleSnapshot struct {
	ID                         int64
	StartsAt                   time.Time
	EndsAt                     time.Time
	Status                     string
	SourceType                 string
	SourceRef                  *string
	FinalUsageUSD              float64
	FinalManualQuotaResetCount int64
	CompletedAt                *time.Time
}

// TermExpectedState is the exact post-deduction state that refund rollback is
// allowed to replace. It acts as a full in-memory CAS token without requiring a
// database version column.
type TermExpectedState struct {
	ExpiresAt            time.Time
	Status               string
	DeletedAt            *time.Time
	CurrentCycleStartsAt time.Time
	CurrentCycleEndsAt   time.Time
	Cycles               []TermCycleSnapshot
}

// TermSnapshot contains only fields changed by subscription term adjustment
// and revocation. Payment refund compensation uses it to restore exact cycle
// attribution instead of extending whichever cycle happens to be the tail.
type TermSnapshot struct {
	SubscriptionID       int64
	UserID               int64
	GroupID              int64
	ExpiresAt            time.Time
	Status               string
	DeletedAt            *time.Time
	CurrentCycleStartsAt time.Time
	CurrentCycleEndsAt   time.Time
	Cycles               []TermCycleSnapshot
	Expected             *TermExpectedState
}

func CaptureTermSnapshot(ctx context.Context, client *dbent.Client, subscriptionID int64) (*TermSnapshot, error) {
	queryCtx := mixins.SkipSoftDelete(ctx)
	sub, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(queryCtx)
	if err != nil {
		return nil, err
	}
	cycles, err := client.UserSubscriptionCycle.Query().
		Where(usersubscriptioncycle.SubscriptionIDEQ(subscriptionID)).
		Order(dbent.Asc(usersubscriptioncycle.FieldID)).
		ForUpdate().
		All(queryCtx)
	if err != nil {
		return nil, err
	}
	snapshot := &TermSnapshot{
		SubscriptionID:       sub.ID,
		UserID:               sub.UserID,
		GroupID:              sub.GroupID,
		ExpiresAt:            sub.ExpiresAt,
		Status:               sub.Status,
		DeletedAt:            cloneTimePointer(sub.DeletedAt),
		CurrentCycleStartsAt: sub.CurrentCycleStartsAt,
		CurrentCycleEndsAt:   sub.CurrentCycleEndsAt,
		Cycles:               make([]TermCycleSnapshot, 0, len(cycles)),
	}
	for _, cycle := range cycles {
		snapshot.Cycles = append(snapshot.Cycles, TermCycleSnapshot{
			ID:                         cycle.ID,
			StartsAt:                   cycle.StartsAt,
			EndsAt:                     cycle.EndsAt,
			Status:                     cycle.Status,
			SourceType:                 cycle.SourceType,
			SourceRef:                  cloneStringPointer(cycle.SourceRef),
			FinalUsageUSD:              cycle.FinalUsageUsd,
			FinalManualQuotaResetCount: cycle.FinalManualQuotaResetCount,
			CompletedAt:                cloneTimePointer(cycle.CompletedAt),
		})
	}
	return snapshot, nil
}

// CaptureExpectedTermState records the exact state produced by a successful
// deduction/revocation while the same transaction still owns the row locks.
func CaptureExpectedTermState(ctx context.Context, client *dbent.Client, subscriptionID int64) (*TermExpectedState, error) {
	queryCtx := mixins.SkipSoftDelete(ctx)
	sub, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(queryCtx)
	if err != nil {
		return nil, err
	}
	cycles, err := client.UserSubscriptionCycle.Query().
		Where(usersubscriptioncycle.SubscriptionIDEQ(subscriptionID)).
		Order(dbent.Asc(usersubscriptioncycle.FieldID)).
		ForUpdate().
		All(queryCtx)
	if err != nil {
		return nil, err
	}
	expected := &TermExpectedState{
		ExpiresAt:            sub.ExpiresAt,
		Status:               sub.Status,
		DeletedAt:            cloneTimePointer(sub.DeletedAt),
		CurrentCycleStartsAt: sub.CurrentCycleStartsAt,
		CurrentCycleEndsAt:   sub.CurrentCycleEndsAt,
		Cycles:               make([]TermCycleSnapshot, 0, len(cycles)),
	}
	for _, cycle := range cycles {
		expected.Cycles = append(expected.Cycles, termCycleSnapshot(cycle))
	}
	return expected, nil
}

func RestoreTermSnapshot(ctx context.Context, client *dbent.Client, snapshot *TermSnapshot) error {
	if snapshot == nil || snapshot.SubscriptionID <= 0 {
		return fmt.Errorf("%w: invalid snapshot", ErrTermSnapshotStale)
	}
	queryCtx := mixins.SkipSoftDelete(ctx)
	sub, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(snapshot.SubscriptionID)).
		ForUpdate().
		Only(queryCtx)
	if err != nil {
		return err
	}
	if snapshot.Expected == nil ||
		!sub.ExpiresAt.Equal(snapshot.Expected.ExpiresAt) ||
		sub.Status != snapshot.Expected.Status ||
		!equalTimePointers(sub.DeletedAt, snapshot.Expected.DeletedAt) ||
		!sub.CurrentCycleStartsAt.Equal(snapshot.Expected.CurrentCycleStartsAt) ||
		!sub.CurrentCycleEndsAt.Equal(snapshot.Expected.CurrentCycleEndsAt) {
		return fmt.Errorf("%w: subscription changed after deduction", ErrTermSnapshotStale)
	}

	cycles, err := client.UserSubscriptionCycle.Query().
		Where(usersubscriptioncycle.SubscriptionIDEQ(snapshot.SubscriptionID)).
		Order(dbent.Asc(usersubscriptioncycle.FieldID)).
		ForUpdate().
		All(queryCtx)
	if err != nil {
		return err
	}
	if len(cycles) != len(snapshot.Expected.Cycles) || len(cycles) != len(snapshot.Cycles) {
		return fmt.Errorf("%w: cycle count changed after deduction", ErrTermSnapshotStale)
	}
	cycleByID := make(map[int64]*dbent.UserSubscriptionCycle, len(cycles))
	for _, cycle := range cycles {
		cycleByID[cycle.ID] = cycle
	}
	for _, expected := range snapshot.Expected.Cycles {
		cycle := cycleByID[expected.ID]
		if cycle == nil || !cycleMatchesSnapshot(cycle, expected) {
			return fmt.Errorf("%w: cycle set changed after deduction", ErrTermSnapshotStale)
		}
	}

	update := client.UserSubscription.UpdateOneID(snapshot.SubscriptionID).
		SetExpiresAt(snapshot.ExpiresAt).
		SetStatus(snapshot.Status).
		SetCurrentCycleStartsAt(snapshot.CurrentCycleStartsAt).
		SetCurrentCycleEndsAt(snapshot.CurrentCycleEndsAt)
	if snapshot.DeletedAt == nil {
		update.ClearDeletedAt()
	} else {
		update.SetDeletedAt(*snapshot.DeletedAt)
	}
	if _, err := update.Save(queryCtx); err != nil {
		return err
	}
	for _, saved := range snapshot.Cycles {
		update := cycleByID[saved.ID].Update().
			SetStartsAt(saved.StartsAt).
			SetEndsAt(saved.EndsAt).
			SetStatus(saved.Status).
			SetSourceType(saved.SourceType).
			SetNillableSourceRef(saved.SourceRef).
			SetFinalUsageUsd(saved.FinalUsageUSD).
			SetFinalManualQuotaResetCount(saved.FinalManualQuotaResetCount).
			SetNillableCompletedAt(saved.CompletedAt)
		if _, err := update.Save(queryCtx); err != nil {
			return err
		}
	}
	return nil
}

func termCycleSnapshot(cycle *dbent.UserSubscriptionCycle) TermCycleSnapshot {
	return TermCycleSnapshot{
		ID:                         cycle.ID,
		StartsAt:                   cycle.StartsAt,
		EndsAt:                     cycle.EndsAt,
		Status:                     cycle.Status,
		SourceType:                 cycle.SourceType,
		SourceRef:                  cloneStringPointer(cycle.SourceRef),
		FinalUsageUSD:              cycle.FinalUsageUsd,
		FinalManualQuotaResetCount: cycle.FinalManualQuotaResetCount,
		CompletedAt:                cloneTimePointer(cycle.CompletedAt),
	}
}

func cycleMatchesSnapshot(cycle *dbent.UserSubscriptionCycle, expected TermCycleSnapshot) bool {
	return cycle.ID == expected.ID &&
		cycle.StartsAt.Equal(expected.StartsAt) &&
		cycle.EndsAt.Equal(expected.EndsAt) &&
		cycle.Status == expected.Status &&
		cycle.SourceType == expected.SourceType &&
		equalStringPointers(cycle.SourceRef, expected.SourceRef) &&
		cycle.FinalUsageUsd == expected.FinalUsageUSD &&
		cycle.FinalManualQuotaResetCount == expected.FinalManualQuotaResetCount &&
		equalTimePointers(cycle.CompletedAt, expected.CompletedAt)
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalTimePointers(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
