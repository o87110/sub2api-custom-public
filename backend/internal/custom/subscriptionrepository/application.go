package subscriptionrepository

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *Repository) RenewExistingTerm(
	ctx context.Context,
	subscriptionID int64,
	validityDays int,
	notes, sourceType string,
	sourceRef *string,
	manualBulkQuotaResetEnabled bool,
	assignmentSemantics bool,
	now, maxExpiresAt time.Time,
) error {
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		existing, err := r.GetByIDForUpdate(txCtx, subscriptionID)
		if err != nil {
			return fmt.Errorf("lock subscription for renewal: %w", err)
		}
		if assignmentSemantics && existing.Status == service.SubscriptionStatusSuspended {
			return nil
		}
		isExpired := !existing.ExpiresAt.After(now)
		if assignmentSemantics {
			isExpired = existing.Status == service.SubscriptionStatusExpired ||
				(existing.Status != service.SubscriptionStatusSuspended && !existing.ExpiresAt.After(now))
		}
		newExpiresAt := existing.ExpiresAt.AddDate(0, 0, validityDays)
		if isExpired {
			newExpiresAt = now.AddDate(0, 0, validityDays)
		}
		if newExpiresAt.After(maxExpiresAt) {
			newExpiresAt = maxExpiresAt
		}
		if assignmentSemantics && strings.TrimSpace(existing.Notes) == strings.TrimSpace(notes) {
			notes = ""
		}

		if isExpired {
			return subscriptionquota.RenewExpired(txCtx, client, subscriptionquota.RenewExpiredInput{
				SubscriptionID:              existing.ID,
				StartsAt:                    now,
				EndsAt:                      newExpiresAt,
				DailyStart:                  timezone.StartOfDay(now),
				PeriodicStart:               now,
				Status:                      service.SubscriptionStatusActive,
				Notes:                       appendNotes(existing.Notes, notes),
				SourceType:                  sourceType,
				SourceRef:                   sourceRef,
				ManualBulkQuotaResetEnabled: manualBulkQuotaResetEnabled,
			})
		}

		if err := subscriptionquota.AppendRenewalCycle(txCtx, client, existing.ID, existing.ExpiresAt, newExpiresAt, sourceType, sourceRef, manualBulkQuotaResetEnabled); err != nil {
			return fmt.Errorf("extend subscription: %w", err)
		}
		if existing.Status != service.SubscriptionStatusActive {
			if err := r.UpdateStatus(txCtx, existing.ID, service.SubscriptionStatusActive); err != nil {
				return fmt.Errorf("update subscription status: %w", err)
			}
		}
		if notes != "" {
			if err := r.UpdateNotes(txCtx, existing.ID, appendNotes(existing.Notes, notes)); err != nil {
				return fmt.Errorf("update subscription notes: %w", err)
			}
		}
		return nil
	})
	return translateCycleError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func (r *Repository) AdjustTerm(
	ctx context.Context,
	subscriptionID int64,
	days int,
	captureSnapshot, revokeIfExpired bool,
	now, maxExpiresAt time.Time,
	maxValidityDays int,
) (*service.UserSubscription, *subscriptionquota.TermSnapshot, error) {
	if days > maxValidityDays {
		days = maxValidityDays
	}
	if days < -maxValidityDays {
		days = -maxValidityDays
	}

	var snapshot *subscriptionquota.TermSnapshot
	var subject *service.UserSubscription
	wouldExpire := false
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		sub, err := r.GetByIDForUpdate(txCtx, subscriptionID)
		if err != nil {
			return err
		}
		subject = sub
		isExpired := !sub.ExpiresAt.After(now)
		if isExpired && days < 0 {
			return infraerrors.BadRequest("CANNOT_SHORTEN_EXPIRED", "cannot shorten an expired subscription")
		}
		newExpiresAt := sub.ExpiresAt.AddDate(0, 0, days)
		if isExpired {
			newExpiresAt = now.AddDate(0, 0, days)
		}
		if newExpiresAt.After(maxExpiresAt) {
			newExpiresAt = maxExpiresAt
		}

		if !newExpiresAt.After(now) {
			wouldExpire = true
			if !revokeIfExpired {
				return service.ErrAdjustWouldExpire
			}
			if captureSnapshot {
				snapshot, err = subscriptionquota.CaptureTermSnapshot(txCtx, client, subscriptionID)
				if err != nil {
					return err
				}
			}
			if err := r.Delete(txCtx, subscriptionID); err != nil {
				return err
			}
			if snapshot != nil {
				snapshot.Expected, err = subscriptionquota.CaptureExpectedTermState(txCtx, client, subscriptionID)
			}
			return err
		}

		if captureSnapshot {
			snapshot, err = subscriptionquota.CaptureTermSnapshot(txCtx, client, subscriptionID)
			if err != nil {
				return err
			}
		}
		if err := r.BaseUserSubscriptionRepository.ExtendExpiry(txCtx, subscriptionID, newExpiresAt); err != nil {
			return err
		}
		if err := subscriptionquota.AdjustTailCycle(txCtx, client, subscriptionID, newExpiresAt); err != nil {
			return err
		}
		if sub.Status == service.SubscriptionStatusExpired {
			if err := r.UpdateStatus(txCtx, subscriptionID, service.SubscriptionStatusActive); err != nil {
				return err
			}
		}
		if snapshot != nil {
			snapshot.Expected, err = subscriptionquota.CaptureExpectedTermState(txCtx, client, subscriptionID)
		}
		return err
	})
	if err != nil {
		return subject, snapshot, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
	}
	if wouldExpire {
		return subject, snapshot, service.ErrAdjustWouldExpire
	}
	adjusted, err := r.GetByID(ctx, subscriptionID)
	return adjusted, snapshot, err
}

func (r *Repository) RestoreTermSnapshotExact(ctx context.Context, snapshot *subscriptionquota.TermSnapshot) (*service.UserSubscription, error) {
	if snapshot == nil {
		return nil, infraerrors.InternalServer("SUBSCRIPTION_TERM_SNAPSHOT_UNAVAILABLE", "subscription term snapshot is unavailable")
	}
	if err := r.RestoreTermSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, snapshot.SubscriptionID)
}

func (r *Repository) ResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool, operation *idempotencyexecution.Execution, now time.Time) (*service.UserSubscription, error) {
	if !resetDaily && !resetWeekly && !resetMonthly {
		return nil, service.ErrInvalidInput
	}
	var result *service.UserSubscription
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		if _, err := subscriptionquota.AdvanceCycle(txCtx, client, subscriptionID, now, timezone.StartOfDay(now)); err != nil {
			return err
		}
		if operation != nil && operation.OperationKeyHash != "" {
			claimed, err := subscriptionquota.ClaimResetOperation(txCtx, client, subscriptionID, operation.OperationKeyHash, operation.ClaimedAt, operation.ExpiresAt)
			if err != nil {
				return err
			}
			if !claimed {
				result, err = r.GetByID(txCtx, subscriptionID)
				return err
			}
		}
		if err := r.BaseUserSubscriptionRepository.ResetUsageWindows(txCtx, subscriptionID, resetDaily, resetWeekly, resetMonthly, timezone.StartOfDay(now), now); err != nil {
			return err
		}
		if err := subscriptionquota.IncrementManualResetCount(txCtx, client, subscriptionID); err != nil {
			return err
		}
		var err error
		result, err = r.GetByID(txCtx, subscriptionID)
		return err
	})
	return result, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *Repository) EnsureWindowMaintenance(ctx context.Context, sub *service.UserSubscription, now time.Time) (*service.UserSubscription, error) {
	if sub == nil {
		return nil, service.ErrSubscriptionNilInput
	}
	advanced, err := r.AdvanceCycle(ctx, sub.ID, now, timezone.StartOfDay(now))
	if err != nil {
		return nil, err
	}
	if !advanced {
		return sub, nil
	}
	return r.GetByID(ctx, sub.ID)
}

func appendNotes(existing, added string) string {
	if added == "" {
		return existing
	}
	if existing == "" {
		return added
	}
	return existing + "\n" + added
}
