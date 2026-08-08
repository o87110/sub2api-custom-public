package subscriptionquota

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/ent/usersubscriptioncycle"
)

var ErrCycleStateNotFound = errors.New("subscription cycle state not found")

type RenewExpiredInput struct {
	SubscriptionID int64
	StartsAt       time.Time
	EndsAt         time.Time
	DailyStart     time.Time
	PeriodicStart  time.Time
	Status         string
	Notes          string
	SourceType     string
	SourceRef      *string
}

func InitializeCurrentCycle(
	ctx context.Context,
	client *dbent.Client,
	subscriptionID int64,
	startsAt, endsAt time.Time,
	cycleUsageUSD float64,
	manualResetCount int64,
	sourceType string,
	sourceRef *string,
) error {
	if _, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetCycleUsageUsd(cycleUsageUSD).
		SetManualQuotaResetCount(manualResetCount).
		SetCurrentCycleStartsAt(startsAt).
		SetCurrentCycleEndsAt(endsAt).
		Save(ctx); err != nil {
		return err
	}
	return CreateCurrentCycle(ctx, client, subscriptionID, startsAt, endsAt, sourceType, sourceRef)
}

func CreateCurrentCycle(ctx context.Context, client *dbent.Client, subscriptionID int64, startsAt, endsAt time.Time, sourceType string, sourceRef *string) error {
	_, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(subscriptionID).
		SetStartsAt(startsAt).
		SetEndsAt(endsAt).
		SetStatus(CycleStatusCurrent).
		SetSourceType(NormalizeSource(sourceType)).
		SetNillableSourceRef(sourceRef).
		Save(ctx)
	return err
}

func AppendRenewalCycle(ctx context.Context, client *dbent.Client, subscriptionID int64, startsAt, endsAt time.Time, sourceType string, sourceRef *string) error {
	if !endsAt.After(startsAt) {
		return nil
	}
	if _, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(ctx); err != nil {
		return err
	}
	if _, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(subscriptionID).
		SetStartsAt(startsAt).
		SetEndsAt(endsAt).
		SetStatus(CycleStatusPending).
		SetSourceType(NormalizeSource(sourceType)).
		SetNillableSourceRef(sourceRef).
		Save(ctx); err != nil {
		return err
	}
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetExpiresAt(endsAt).
		Save(ctx)
	return err
}

func RenewExpired(ctx context.Context, client *dbent.Client, input RenewExpiredInput) error {
	current, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(input.SubscriptionID)).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return err
	}

	if _, err := client.UserSubscriptionCycle.Update().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(input.SubscriptionID),
			usersubscriptioncycle.StatusEQ(CycleStatusCurrent),
		).
		SetStatus(CycleStatusCompleted).
		SetFinalUsageUsd(current.CycleUsageUsd).
		SetFinalManualQuotaResetCount(current.ManualQuotaResetCount).
		SetCompletedAt(input.StartsAt).
		Save(ctx); err != nil {
		return err
	}
	if _, err := client.UserSubscriptionCycle.Update().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(input.SubscriptionID),
			usersubscriptioncycle.StatusEQ(CycleStatusPending),
		).
		SetStatus(CycleStatusCompleted).
		SetCompletedAt(input.StartsAt).
		Save(ctx); err != nil {
		return err
	}

	if _, err := client.UserSubscription.UpdateOneID(input.SubscriptionID).
		SetStartsAt(input.StartsAt).
		SetExpiresAt(input.EndsAt).
		SetStatus(input.Status).
		SetDailyWindowStart(input.DailyStart).
		SetWeeklyWindowStart(input.PeriodicStart).
		SetMonthlyWindowStart(input.PeriodicStart).
		SetDailyUsageUsd(0).
		SetWeeklyUsageUsd(0).
		SetMonthlyUsageUsd(0).
		SetCycleUsageUsd(0).
		SetManualQuotaResetCount(0).
		SetCurrentCycleStartsAt(input.StartsAt).
		SetCurrentCycleEndsAt(input.EndsAt).
		SetNotes(input.Notes).
		Save(ctx); err != nil {
		return err
	}
	return CreateCurrentCycle(ctx, client, input.SubscriptionID, input.StartsAt, input.EndsAt, input.SourceType, input.SourceRef)
}

func AdvanceCycle(ctx context.Context, client *dbent.Client, subscriptionID int64, now, dailyStart time.Time) (bool, error) {
	sub, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return false, err
	}
	if !NeedsAdvance(sub.CurrentCycleEndsAt, sub.ExpiresAt, now) {
		return false, nil
	}

	currentCycle, err := client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(subscriptionID),
			usersubscriptioncycle.StatusEQ(CycleStatusCurrent),
		).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: current cycle", ErrCycleStateNotFound)
	}
	if _, err := currentCycle.Update().
		SetStatus(CycleStatusCompleted).
		SetFinalUsageUsd(sub.CycleUsageUsd).
		SetFinalManualQuotaResetCount(sub.ManualQuotaResetCount).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return false, err
	}

	pending, err := client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(subscriptionID),
			usersubscriptioncycle.StatusEQ(CycleStatusPending),
		).
		Order(dbent.Asc(usersubscriptioncycle.FieldStartsAt)).
		ForUpdate().
		All(ctx)
	if err != nil {
		return false, err
	}
	var nextCycle *dbent.UserSubscriptionCycle
	for _, cycle := range pending {
		if !cycle.EndsAt.After(now) {
			if _, err := cycle.Update().
				SetStatus(CycleStatusCompleted).
				SetCompletedAt(now).
				Save(ctx); err != nil {
				return false, err
			}
			continue
		}
		nextCycle = cycle
		break
	}
	if nextCycle == nil {
		return false, fmt.Errorf("%w: pending cycle", ErrCycleStateNotFound)
	}
	if _, err := nextCycle.Update().
		SetStatus(CycleStatusCurrent).
		Save(ctx); err != nil {
		return false, err
	}
	if _, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetCurrentCycleStartsAt(nextCycle.StartsAt).
		SetCurrentCycleEndsAt(nextCycle.EndsAt).
		SetCycleUsageUsd(0).
		SetManualQuotaResetCount(0).
		SetDailyUsageUsd(0).
		SetWeeklyUsageUsd(0).
		SetMonthlyUsageUsd(0).
		SetDailyWindowStart(dailyStart).
		SetWeeklyWindowStart(now).
		SetMonthlyWindowStart(now).
		Save(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func AdjustTailCycle(ctx context.Context, client *dbent.Client, subscriptionID int64, newExpiresAt time.Time) error {
	if _, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(ctx); err != nil {
		return err
	}
	cycles, err := client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(subscriptionID),
			usersubscriptioncycle.StatusIn(CycleStatusCurrent, CycleStatusPending),
		).
		Order(dbent.Asc(usersubscriptioncycle.FieldStartsAt)).
		ForUpdate().
		All(ctx)
	if err != nil {
		return err
	}
	var tail *dbent.UserSubscriptionCycle
	for _, cycle := range cycles {
		if !cycle.StartsAt.Before(newExpiresAt) {
			if _, err := cycle.Update().SetStatus(CycleStatusCancelled).Save(ctx); err != nil {
				return err
			}
			continue
		}
		tail = cycle
	}
	if tail == nil {
		// Migration 196 archives already-expired subscriptions as completed.
		// Re-extending one of those subscriptions must restore its latest cycle
		// instead of making the upstream admin adjustment fail as "not found".
		completed, completedErr := client.UserSubscriptionCycle.Query().
			Where(
				usersubscriptioncycle.SubscriptionIDEQ(subscriptionID),
				usersubscriptioncycle.StatusEQ(CycleStatusCompleted),
			).
			Order(dbent.Desc(usersubscriptioncycle.FieldEndsAt)).
			ForUpdate().
			First(ctx)
		if completedErr != nil {
			if !dbent.IsNotFound(completedErr) {
				return completedErr
			}
			return fmt.Errorf("%w: tail cycle", ErrCycleStateNotFound)
		}
		if !completed.StartsAt.Before(newExpiresAt) {
			return fmt.Errorf("%w: completed tail cycle", ErrCycleStateNotFound)
		}
		if _, err := completed.Update().
			SetEndsAt(newExpiresAt).
			SetStatus(CycleStatusCurrent).
			SetFinalUsageUsd(0).
			SetFinalManualQuotaResetCount(0).
			ClearCompletedAt().
			Save(ctx); err != nil {
			return err
		}
		_, err = client.UserSubscription.UpdateOneID(subscriptionID).
			SetCurrentCycleStartsAt(completed.StartsAt).
			SetCurrentCycleEndsAt(newExpiresAt).
			Save(ctx)
		return err
	}
	if _, err := tail.Update().SetEndsAt(newExpiresAt).Save(ctx); err != nil {
		return err
	}
	if tail.Status == CycleStatusCurrent {
		_, err = client.UserSubscription.UpdateOneID(subscriptionID).
			SetCurrentCycleEndsAt(newExpiresAt).
			Save(ctx)
	}
	return err
}

func IncrementCycleUsage(ctx context.Context, client *dbent.Client, subscriptionID int64, costUSD float64) error {
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		AddCycleUsageUsd(costUSD).
		Save(ctx)
	return err
}

func IncrementCycleUsageTx(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	driver := entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})
	client := dbent.NewClient(dbent.Driver(driver))
	_, err := client.UserSubscription.Update().
		Where(usersubscription.IDEQ(subscriptionID)).
		AddCycleUsageUsd(costUSD).
		Save(ctx)
	return err
}

func IncrementManualResetCount(ctx context.Context, client *dbent.Client, subscriptionID int64) error {
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		AddManualQuotaResetCount(1).
		Save(ctx)
	return err
}
