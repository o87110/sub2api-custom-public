package subscriptionrepository

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
)

// Repository decorates the upstream subscription repository with Custom cycle
// accounting. Transaction and application orchestration intentionally live in
// this package so upstream repository/service files remain thin integration
// points.
type Repository struct {
	service.BaseUserSubscriptionRepository
	client *dbent.Client
}

func New(client *dbent.Client, base service.BaseUserSubscriptionRepository) *Repository {
	return &Repository{BaseUserSubscriptionRepository: base, client: client}
}

var ProviderSet = wire.NewSet(
	New,
	wire.Bind(new(service.UserSubscriptionRepository), new(*Repository)),
)

func (r *Repository) Create(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}
	startsAt := sub.StartsAt
	if startsAt.IsZero() {
		startsAt = time.Now()
		sub.StartsAt = startsAt
	}
	cycleStartsAt := sub.CurrentCycleStartsAt
	if cycleStartsAt.IsZero() {
		cycleStartsAt = startsAt
	}
	cycleEndsAt := sub.CurrentCycleEndsAt
	if cycleEndsAt.IsZero() {
		cycleEndsAt = sub.ExpiresAt
	}

	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		if err := r.BaseUserSubscriptionRepository.Create(txCtx, sub); err != nil {
			return err
		}
		return subscriptionquota.InitializeCurrentCycle(
			txCtx,
			client,
			sub.ID,
			cycleStartsAt,
			cycleEndsAt,
			sub.CycleUsageUSD,
			sub.ManualQuotaResetCount,
			sub.CycleSourceType,
			sub.CycleSourceRef,
			sub.ManualBulkQuotaResetEnabled,
		)
	})
	if err == nil {
		sub.CurrentCycleStartsAt = cycleStartsAt
		sub.CurrentCycleEndsAt = cycleEndsAt
	}
	return translateCycleError(err, nil, service.ErrSubscriptionAlreadyExists)
}

func (r *Repository) AppendRenewalCycle(ctx context.Context, subscriptionID int64, startsAt, endsAt time.Time, sourceType string, sourceRef *string) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		return subscriptionquota.AppendRenewalCycle(txCtx, client, subscriptionID, startsAt, endsAt, sourceType, sourceRef, false)
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func (r *Repository) RenewExpiredWithCycle(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		dailyStart := sub.CurrentCycleStartsAt
		if sub.DailyWindowStart != nil {
			dailyStart = *sub.DailyWindowStart
		}
		periodicStart := sub.CurrentCycleStartsAt
		if sub.WeeklyWindowStart != nil {
			periodicStart = *sub.WeeklyWindowStart
		}
		return subscriptionquota.RenewExpired(txCtx, client, subscriptionquota.RenewExpiredInput{
			SubscriptionID:              sub.ID,
			StartsAt:                    sub.CurrentCycleStartsAt,
			EndsAt:                      sub.CurrentCycleEndsAt,
			DailyStart:                  dailyStart,
			PeriodicStart:               periodicStart,
			Status:                      sub.Status,
			Notes:                       sub.Notes,
			SourceType:                  sub.CycleSourceType,
			SourceRef:                   sub.CycleSourceRef,
			ManualBulkQuotaResetEnabled: sub.ManualBulkQuotaResetEnabled,
		})
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func (r *Repository) AdvanceCycle(ctx context.Context, subscriptionID int64, now, dailyStart time.Time) (bool, error) {
	advanced := false
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		var err error
		advanced, err = subscriptionquota.AdvanceCycle(txCtx, client, subscriptionID, now, dailyStart)
		return err
	})
	return advanced, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) CaptureTermSnapshot(ctx context.Context, subscriptionID int64) (*subscriptionquota.TermSnapshot, error) {
	var snapshot *subscriptionquota.TermSnapshot
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		var err error
		snapshot, err = subscriptionquota.CaptureTermSnapshot(txCtx, client, subscriptionID)
		return err
	})
	return snapshot, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) AdjustExpiryWithSnapshot(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) (*subscriptionquota.TermSnapshot, error) {
	var snapshot *subscriptionquota.TermSnapshot
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		var err error
		snapshot, err = subscriptionquota.CaptureTermSnapshot(txCtx, client, subscriptionID)
		if err != nil {
			return err
		}
		if err := r.BaseUserSubscriptionRepository.ExtendExpiry(txCtx, subscriptionID, newExpiresAt); err != nil {
			return err
		}
		if err := subscriptionquota.AdjustTailCycle(txCtx, client, subscriptionID, newExpiresAt); err != nil {
			return err
		}
		snapshot.Expected, err = subscriptionquota.CaptureExpectedTermState(txCtx, client, subscriptionID)
		return err
	})
	return snapshot, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) RestoreTermSnapshot(ctx context.Context, snapshot *subscriptionquota.TermSnapshot) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		return subscriptionquota.RestoreTermSnapshot(txCtx, client, snapshot)
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		if err := r.BaseUserSubscriptionRepository.ExtendExpiry(txCtx, subscriptionID, newExpiresAt); err != nil {
			return err
		}
		return subscriptionquota.AdjustTailCycle(txCtx, client, subscriptionID, newExpiresAt)
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		if err := r.BaseUserSubscriptionRepository.ResetUsageWindows(txCtx, id, resetDaily, resetWeekly, resetMonthly, dailyStart, periodicStart); err != nil {
			return err
		}
		return subscriptionquota.IncrementManualResetCount(txCtx, client, id)
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) ResetUsageWindowsIdempotent(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time, operation idempotencyexecution.Execution) (bool, error) {
	applied := false
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		claimed, err := subscriptionquota.ClaimResetOperation(txCtx, client, id, operation.OperationKeyHash, operation.ClaimedAt, operation.ExpiresAt)
		if err != nil || !claimed {
			return err
		}
		if err := r.BaseUserSubscriptionRepository.ResetUsageWindows(txCtx, id, resetDaily, resetWeekly, resetMonthly, dailyStart, periodicStart); err != nil {
			return err
		}
		if err := subscriptionquota.IncrementManualResetCount(txCtx, client, id); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		if err := r.BaseUserSubscriptionRepository.IncrementUsage(txCtx, id, costUSD); err != nil {
			return err
		}
		return subscriptionquota.IncrementCycleUsage(txCtx, client, id, costUSD)
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) withTx(ctx context.Context, fn func(context.Context, *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func translateCycleError(err error, notFound, conflict *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, subscriptionquota.ErrCycleStateNotFound) && notFound != nil {
		return notFound.WithCause(err)
	}
	if dbent.IsNotFound(err) && notFound != nil {
		return notFound.WithCause(err)
	}
	if dbent.IsConstraintError(err) && conflict != nil {
		return conflict.WithCause(err)
	}
	return err
}
