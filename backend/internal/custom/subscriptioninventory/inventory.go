package subscriptioninventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	StateUntracked = "untracked"
	StateReserved  = "reserved"
	StateConsumed  = "consumed"
	StateReleased  = "released"
)

// QuantityPatch preserves the three states required by plan PATCH requests:
// omitted means unchanged, null means unlimited, and a positive integer means
// a new limited remaining quantity.
type QuantityPatch struct {
	Present bool
	Value   *int
}

func (p *QuantityPatch) UnmarshalJSON(data []byte) error {
	p.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		p.Value = nil
		return nil
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

func ValidateConfiguredQuantity(quantity *int) error {
	if quantity != nil && *quantity <= 0 {
		return infraerrors.BadRequest("PLAN_QUANTITY_INVALID", "remaining quantity must be a positive integer or null")
	}
	return nil
}

type AdminAvailability struct {
	RemainingQuantity     *int
	ForSale               bool
	InventoryAutoDelisted bool
}

// AdminPlanPatch keeps the official plan DTO at the bridge while allowing all
// fields to be committed atomically with inventory availability.
type AdminPlanPatch struct {
	GroupID           *int64
	Name              *string
	Description       *string
	Price             *float64
	OriginalPrice     *float64
	Currency          *string
	ValidityDays      *int
	ValidityUnit      *string
	Features          *string
	ProductName       *string
	ForSale           *bool
	RemainingQuantity QuantityPatch
	SortOrder         *int
}

// lockPlanForAdminUpdate acquires the plan row's write lock inside the
// caller-owned transaction before inventory availability is read. The no-op
// sort-order update is portable across the PostgreSQL runtime and SQLite tests.
func lockPlanForAdminUpdate(ctx context.Context, client *dbent.Client, planID int64) (*dbent.SubscriptionPlan, error) {
	updated, err := client.SubscriptionPlan.Update().
		Where(subscriptionplan.IDEQ(planID)).
		AddSortOrder(0).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock plan for inventory update: %w", err)
	}
	if updated == 0 {
		return nil, &dbent.NotFoundError{}
	}
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("load locked plan inventory: %w", err)
	}
	return plan, nil
}

// ListPlansForSale filters both explicit delisting and zero inventory so an
// inconsistent on-sale flag can never make a sold-out plan public again.
func ListPlansForSale(ctx context.Context, client *dbent.Client) ([]*dbent.SubscriptionPlan, error) {
	return client.SubscriptionPlan.Query().Where(
		subscriptionplan.ForSaleEQ(true),
		subscriptionplan.Or(
			subscriptionplan.RemainingQuantityIsNil(),
			subscriptionplan.RemainingQuantityGT(0),
		),
	).Order(subscriptionplan.BySortOrder()).All(ctx)
}

// UpdateAdminPlan serializes availability changes with order reservations and
// commits the entire PATCH atomically so non-inventory fields cannot be left in
// a partial state if inventory validation or persistence fails.
func UpdateAdminPlan(ctx context.Context, entClient *dbent.Client, planID int64, patch AdminPlanPatch) (*dbent.SubscriptionPlan, error) {
	client := entClient
	opCtx := ctx
	var tx *dbent.Tx
	if patch.RemainingQuantity.Present || patch.ForSale != nil {
		var err error
		tx, err = entClient.Tx(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin plan inventory update tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		client = tx.Client()
		opCtx = dbent.NewTxContext(ctx, tx)
	}

	update := client.SubscriptionPlan.UpdateOneID(planID)
	if tx != nil {
		current, err := lockPlanForAdminUpdate(opCtx, client, planID)
		if err != nil {
			return nil, planNotFoundOrError(err)
		}
		availability, err := ResolveAdminAvailability(
			AdminAvailability{
				RemainingQuantity:     current.RemainingQuantity,
				ForSale:               current.ForSale,
				InventoryAutoDelisted: current.InventoryAutoDelisted,
			},
			patch.RemainingQuantity,
			patch.ForSale,
		)
		if err != nil {
			return nil, err
		}
		if patch.RemainingQuantity.Present {
			if availability.RemainingQuantity == nil {
				update.ClearRemainingQuantity()
			} else {
				update.SetRemainingQuantity(*availability.RemainingQuantity)
			}
		}
		update.SetForSale(availability.ForSale).
			SetInventoryAutoDelisted(availability.InventoryAutoDelisted)
	}
	applyAdminPlanFields(update, patch)
	plan, err := update.Save(opCtx)
	if err != nil {
		return nil, planNotFoundOrError(err)
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit plan inventory update tx: %w", err)
		}
		plan.Unwrap()
	}
	return plan, nil
}

func applyAdminPlanFields(update *dbent.SubscriptionPlanUpdateOne, patch AdminPlanPatch) {
	if patch.GroupID != nil {
		update.SetGroupID(*patch.GroupID)
	}
	if patch.Name != nil {
		update.SetName(*patch.Name)
	}
	if patch.Description != nil {
		update.SetDescription(*patch.Description)
	}
	if patch.Price != nil {
		update.SetPrice(*patch.Price)
	}
	if patch.OriginalPrice != nil {
		update.SetOriginalPrice(*patch.OriginalPrice)
	}
	if patch.Currency != nil {
		update.SetCurrency(*patch.Currency)
	}
	if patch.ValidityDays != nil {
		update.SetValidityDays(*patch.ValidityDays)
	}
	if patch.ValidityUnit != nil {
		update.SetValidityUnit(*patch.ValidityUnit)
	}
	if patch.Features != nil {
		update.SetFeatures(*patch.Features)
	}
	if patch.ProductName != nil {
		update.SetProductName(*patch.ProductName)
	}
	if patch.SortOrder != nil {
		update.SetSortOrder(*patch.SortOrder)
	}
}

func planNotFoundOrError(err error) error {
	if dbent.IsNotFound(err) {
		return infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return err
}

// ResolveAdminAvailability applies an inventory patch without overriding an
// explicit manual sale-state choice. Replenishing an automatically delisted
// plan restores it; replenishing a manually delisted plan does not.
func ResolveAdminAvailability(
	current AdminAvailability,
	quantity QuantityPatch,
	requestedForSale *bool,
) (AdminAvailability, error) {
	result := current
	if quantity.Present {
		if err := ValidateConfiguredQuantity(quantity.Value); err != nil {
			return AdminAvailability{}, err
		}
		result.RemainingQuantity = quantity.Value
		if current.InventoryAutoDelisted && requestedForSale == nil {
			result.ForSale = true
			result.InventoryAutoDelisted = false
		}
	}
	if requestedForSale != nil {
		result.ForSale = *requestedForSale
		result.InventoryAutoDelisted = false
	}
	if result.ForSale && result.RemainingQuantity != nil && *result.RemainingQuantity == 0 {
		return AdminAvailability{}, infraerrors.Conflict("PLAN_SOLD_OUT", "sold-out plan cannot be put on sale")
	}
	return result, nil
}

// ReserveForOrder atomically reserves one limited unit. Unlimited plans return
// StateUntracked. requireForSale is true for new orders and false when a paid,
// previously released order is reacquiring its unit during fulfillment.
func ReserveForOrder(ctx context.Context, client *dbent.Client, planID int64, requireForSale bool) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		if requireForSale {
			reserved, err := client.SubscriptionPlan.Update().
				Where(
					subscriptionplan.IDEQ(planID),
					subscriptionplan.ForSaleEQ(true),
					subscriptionplan.RemainingQuantityEQ(1),
				).
				SetRemainingQuantity(0).
				SetForSale(false).
				SetInventoryAutoDelisted(true).
				Save(ctx)
			if err != nil {
				return "", fmt.Errorf("reserve final plan inventory: %w", err)
			}
			if reserved > 0 {
				return StateReserved, nil
			}
		} else {
			reserved, err := reserveFinalUnitForPaidOrder(ctx, client, planID)
			if err != nil {
				return "", err
			}
			if reserved {
				return StateReserved, nil
			}
		}

		update := client.SubscriptionPlan.Update().Where(
			subscriptionplan.IDEQ(planID),
			subscriptionplan.RemainingQuantityGT(1),
		)
		if requireForSale {
			update.Where(subscriptionplan.ForSaleEQ(true))
		}
		reserved, err := update.
			AddRemainingQuantity(-1).
			Save(ctx)
		if err != nil {
			return "", fmt.Errorf("reserve plan inventory: %w", err)
		}
		if reserved > 0 {
			return StateReserved, nil
		}

		query := client.SubscriptionPlan.Query().Where(subscriptionplan.IDEQ(planID))
		if requireForSale {
			query.Where(subscriptionplan.ForSaleEQ(true))
		}
		plan, err := query.Only(ctx)
		if dbent.IsNotFound(err) {
			return "", soldOutError()
		}
		if err != nil {
			return "", fmt.Errorf("load plan inventory: %w", err)
		}
		if plan.RemainingQuantity == nil {
			return StateUntracked, nil
		}
		if *plan.RemainingQuantity <= 0 {
			return "", soldOutError()
		}
		// A concurrent reservation moved a multi-unit plan to its last unit
		// between the conditional updates. Retry so this caller can claim it.
	}
	return "", infraerrors.Conflict("PLAN_INVENTORY_BUSY", "plan inventory changed concurrently; retry the order")
}

func reserveFinalUnitForPaidOrder(ctx context.Context, client *dbent.Client, planID int64) (bool, error) {
	reserved, err := client.SubscriptionPlan.Update().
		Where(
			subscriptionplan.IDEQ(planID),
			subscriptionplan.ForSaleEQ(true),
			subscriptionplan.RemainingQuantityEQ(1),
		).
		SetRemainingQuantity(0).
		SetForSale(false).
		SetInventoryAutoDelisted(true).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("reacquire final on-sale plan inventory: %w", err)
	}
	if reserved > 0 {
		return true, nil
	}
	reserved, err = client.SubscriptionPlan.Update().
		Where(
			subscriptionplan.IDEQ(planID),
			subscriptionplan.ForSaleEQ(false),
			subscriptionplan.RemainingQuantityEQ(1),
		).
		SetRemainingQuantity(0).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("reacquire final delisted plan inventory: %w", err)
	}
	return reserved > 0, nil
}

// ReleaseReservation changes reserved to released exactly once and returns the
// unit when the plan is still limited. The caller owns the surrounding DB tx.
func ReleaseReservation(ctx context.Context, client *dbent.Client, orderID int64) error {
	order, err := client.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("load inventory reservation: %w", err)
	}
	if order.PlanInventoryState != StateReserved {
		return nil
	}
	if order.PlanID == nil {
		return fmt.Errorf("reserved order %d has no plan", orderID)
	}
	claimed, err := client.PaymentOrder.Update().
		Where(
			paymentorder.IDEQ(orderID),
			paymentorder.PlanInventoryStateEQ(StateReserved),
		).
		SetPlanInventoryState(StateReleased).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("release inventory reservation: %w", err)
	}
	if claimed == 0 {
		return nil
	}

	restored, err := client.SubscriptionPlan.Update().
		Where(
			subscriptionplan.IDEQ(*order.PlanID),
			subscriptionplan.RemainingQuantityEQ(0),
			subscriptionplan.InventoryAutoDelistedEQ(true),
		).
		SetRemainingQuantity(1).
		SetForSale(true).
		SetInventoryAutoDelisted(false).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("restore automatically delisted plan inventory: %w", err)
	}
	if restored > 0 {
		return nil
	}
	if _, err := client.SubscriptionPlan.Update().
		Where(
			subscriptionplan.IDEQ(*order.PlanID),
			subscriptionplan.RemainingQuantityNotNil(),
		).
		AddRemainingQuantity(1).
		Save(ctx); err != nil {
		return fmt.Errorf("return plan inventory: %w", err)
	}
	return nil
}

// TransitionPendingOrderAndRelease changes a pending order terminal state and
// releases its reservation in the same transaction. Repeated calls are no-ops.
func TransitionPendingOrderAndRelease(ctx context.Context, entClient *dbent.Client, orderID int64, nextStatus string) (int, error) {
	tx, err := entClient.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin order inventory release tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	updated, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(orderID), paymentorder.StatusEQ("PENDING")).
		SetStatus(nextStatus).
		Save(txCtx)
	if err != nil {
		return 0, fmt.Errorf("update pending order status: %w", err)
	}
	if updated > 0 {
		if err := ReleaseReservation(txCtx, tx.Client(), orderID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit order inventory release tx: %w", err)
	}
	return updated, nil
}

// ConsumeForFulfillment commits a reservation in the same transaction as the
// subscription assignment. Released late-paid orders reacquire a unit first.
func ConsumeForFulfillment(ctx context.Context, client *dbent.Client, orderID int64) error {
	order, err := client.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("load fulfillment inventory: %w", err)
	}
	switch order.PlanInventoryState {
	case StateUntracked, StateConsumed:
		return nil
	case StateReserved:
		return setConsumed(ctx, client, orderID, StateReserved, order.UpdatedAt)
	case StateReleased:
		if order.PaidAt == nil {
			return infraerrors.BadRequest("ORDER_NOT_PAID", "released inventory cannot be consumed by an unpaid order")
		}
		if order.PlanID == nil {
			return fmt.Errorf("released order %d has no plan", orderID)
		}
		if _, err := ReserveForOrder(ctx, client, *order.PlanID, false); err != nil {
			return err
		}
		return setConsumed(ctx, client, orderID, StateReleased, order.UpdatedAt)
	default:
		return fmt.Errorf("order %d has invalid plan inventory state %q", orderID, order.PlanInventoryState)
	}
}

func setConsumed(ctx context.Context, client *dbent.Client, orderID int64, from string, preservedUpdatedAt time.Time) error {
	updated, err := client.PaymentOrder.Update().
		Where(
			paymentorder.IDEQ(orderID),
			paymentorder.PlanInventoryStateEQ(from),
		).
		SetPlanInventoryState(StateConsumed).
		SetUpdatedAt(preservedUpdatedAt).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("consume plan inventory: %w", err)
	}
	if updated == 0 {
		return infraerrors.Conflict("PLAN_INVENTORY_STATE_CHANGED", "plan inventory was processed concurrently")
	}
	return nil
}

func soldOutError() error {
	return infraerrors.Conflict("PLAN_SOLD_OUT", "subscription plan is sold out")
}
