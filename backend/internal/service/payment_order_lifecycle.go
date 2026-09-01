package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/custom/paymentchannels"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptioninventory"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Cancel & Expire ---

// Cancel rate limit configuration constants.
const (
	rateLimitUnitDay           = "day"
	rateLimitUnitMinute        = "minute"
	rateLimitUnitHour          = "hour"
	rateLimitModeFixed         = "fixed"
	checkPaidResultAlreadyPaid = "already_paid"
	checkPaidResultCancelled   = "cancelled"
	checkPaidResultUnpaid      = "unpaid"
	checkPaidResultUnavailable = "unavailable"

	paymentOrderReconcileLimit = 20
)

type checkPaidOptions struct {
	cancelIfUnpaid bool
}

func (s *PaymentService) checkCancelRateLimit(ctx context.Context, userID int64, cfg *PaymentConfig) error {
	if !cfg.CancelRateLimitEnabled || cfg.CancelRateLimitMax <= 0 {
		return nil
	}
	windowStart := cancelRateLimitWindowStart(cfg)
	operator := fmt.Sprintf("user:%d", userID)
	count, err := s.entClient.PaymentAuditLog.Query().
		Where(
			paymentauditlog.ActionEQ("ORDER_CANCELLED"),
			paymentauditlog.OperatorEQ(operator),
			paymentauditlog.CreatedAtGTE(windowStart),
		).Count(ctx)
	if err != nil {
		slog.Error("check cancel rate limit failed", "userID", userID, "error", err)
		return nil // fail open
	}
	if count >= cfg.CancelRateLimitMax {
		return infraerrors.TooManyRequests("CANCEL_RATE_LIMITED", "cancel rate limited").
			WithMetadata(map[string]string{
				"max":    strconv.Itoa(cfg.CancelRateLimitMax),
				"window": strconv.Itoa(cfg.CancelRateLimitWindow),
				"unit":   cfg.CancelRateLimitUnit,
			})
	}
	return nil
}

func cancelRateLimitWindowStart(cfg *PaymentConfig) time.Time {
	now := time.Now()
	w := cfg.CancelRateLimitWindow
	if w <= 0 {
		w = 1
	}
	unit := cfg.CancelRateLimitUnit
	if unit == "" {
		unit = rateLimitUnitDay
	}
	if cfg.CancelRateLimitMode == rateLimitModeFixed {
		switch unit {
		case rateLimitUnitMinute:
			t := now.Truncate(time.Minute)
			return t.Add(-time.Duration(w-1) * time.Minute)
		case rateLimitUnitDay:
			y, m, d := now.Date()
			t := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			return t.AddDate(0, 0, -(w - 1))
		default: // hour
			t := now.Truncate(time.Hour)
			return t.Add(-time.Duration(w-1) * time.Hour)
		}
	}
	// rolling window
	switch unit {
	case rateLimitUnitMinute:
		return now.Add(-time.Duration(w) * time.Minute)
	case rateLimitUnitDay:
		return now.AddDate(0, 0, -w)
	default: // hour
		return now.Add(-time.Duration(w) * time.Hour)
	}
}

func (s *PaymentService) CancelOrder(ctx context.Context, orderID, userID int64) (string, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return "", infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != userID {
		return "", infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	if o.Status != OrderStatusPending {
		if paymentOrderIsPaidLike(o.Status) {
			return "", infraerrors.Conflict("ORDER_ALREADY_PAID", "order has already been paid and cannot be cancelled")
		}
		return "", infraerrors.Conflict("ORDER_STATUS_CHANGED", "order status changed while cancelling")
	}
	outcome, err := s.cancelCore(ctx, o, OrderStatusCancelled, fmt.Sprintf("user:%d", userID), "user cancelled order")
	return normalizeCancelOutcome(outcome, err)
}

func (s *PaymentService) AdminCancelOrder(ctx context.Context, orderID int64) (string, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return "", infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusPending {
		if paymentOrderIsPaidLike(o.Status) {
			return "", infraerrors.Conflict("ORDER_ALREADY_PAID", "order has already been paid and cannot be cancelled")
		}
		return "", infraerrors.Conflict("ORDER_STATUS_CHANGED", "order status changed while cancelling")
	}
	outcome, err := s.cancelCore(ctx, o, OrderStatusCancelled, "admin", "admin cancelled order")
	return normalizeCancelOutcome(outcome, err)
}

func (s *PaymentService) cancelCore(ctx context.Context, o *dbent.PaymentOrder, fs, op, ad string) (string, error) {
	if o.PaymentTradeNo != "" || o.PaymentType != "" {
		switch s.checkPaid(ctx, o) {
		case checkPaidResultAlreadyPaid:
			return checkPaidResultAlreadyPaid, nil
		case checkPaidResultUnavailable:
			return checkPaidResultUnavailable, nil
		}
	}
	c, err := subscriptioninventory.TransitionPendingOrderAndRelease(ctx, s.entClient, o.ID, fs)
	if err != nil {
		return "", fmt.Errorf("cancel order and release inventory: %w", err)
	}
	if c == 0 {
		current, reloadErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if reloadErr != nil {
			return "", fmt.Errorf("reload order after state transition race: %w", reloadErr)
		}
		if paymentOrderIsPaidLike(current.Status) {
			return checkPaidResultAlreadyPaid, nil
		}
		return "", infraerrors.Conflict("ORDER_STATUS_CHANGED", "order status changed while cancelling")
	}
	auditAction := "ORDER_CANCELLED"
	if fs == OrderStatusExpired {
		auditAction = "ORDER_EXPIRED"
	}
	s.writeAuditLog(ctx, o.ID, auditAction, op, map[string]any{"detail": ad})
	return checkPaidResultCancelled, nil
}

func normalizeCancelOutcome(outcome string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	switch outcome {
	case checkPaidResultAlreadyPaid:
		return "", infraerrors.Conflict("ORDER_ALREADY_PAID", "order has already been paid and cannot be cancelled")
	case checkPaidResultUnavailable:
		return "", infraerrors.ServiceUnavailable("PAYMENT_STATUS_UNAVAILABLE", "payment provider status is temporarily unavailable")
	case checkPaidResultCancelled:
		return outcome, nil
	default:
		return "", infraerrors.Conflict("ORDER_STATUS_CHANGED", "order status changed while cancelling")
	}
}

func paymentOrderIsPaidLike(status string) bool {
	switch status {
	case OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted,
		OrderStatusRefundRequested, OrderStatusRefunding, OrderStatusRefundPending,
		OrderStatusPartiallyRefunded, OrderStatusRefunded, OrderStatusRefundFailed:
		return true
	default:
		return false
	}
}

func (s *PaymentService) checkPaid(ctx context.Context, o *dbent.PaymentOrder) string {
	return s.checkPaidWithOptions(ctx, o, checkPaidOptions{cancelIfUnpaid: true})
}

func (s *PaymentService) reconcilePaid(ctx context.Context, o *dbent.PaymentOrder) string {
	return s.checkPaidWithOptions(ctx, o, checkPaidOptions{})
}

func (s *PaymentService) checkPaidWithOptions(ctx context.Context, o *dbent.PaymentOrder, opts checkPaidOptions) string {
	prov, err := s.getOrderProvider(ctx, o)
	if err != nil {
		slog.Warn("load payment provider for status check failed", "orderID", o.ID, "error", err)
		return checkPaidResultUnavailable
	}
	queryRef := paymentOrderQueryReference(o, prov)
	if queryRef == "" {
		slog.Warn("payment status query reference is missing", "orderID", o.ID)
		return checkPaidResultUnavailable
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.QueryOrder(ctx, queryRef)
	finishProviderCall()
	if err != nil {
		slog.Warn("query upstream failed", "orderID", o.ID, "error", err)
		return checkPaidResultUnavailable
	}
	if resp == nil {
		slog.Warn("query upstream returned empty response", "orderID", o.ID, "queryRef", queryRef)
		return checkPaidResultUnavailable
	}
	if resp.Status != payment.ProviderStatusPaid && resp.Status != payment.ProviderStatusPending && resp.Status != payment.ProviderStatusFailed {
		slog.Warn("query upstream returned unsupported status", "orderID", o.ID, "queryRef", queryRef, "status", resp.Status)
		return checkPaidResultUnavailable
	}
	if resp.Status == payment.ProviderStatusPaid {
		if !isValidProviderAmount(resp.Amount) {
			s.writeAuditLog(ctx, o.ID, "PAYMENT_INVALID_AMOUNT", prov.ProviderKey(), map[string]any{
				"expected": o.PayAmount,
				"paid":     resp.Amount,
				"tradeNo":  resp.TradeNo,
				"queryRef": queryRef,
			})
			slog.Warn("query upstream returned invalid paid amount", "orderID", o.ID, "queryRef", queryRef, "paid", resp.Amount)
			retriedResp, retryOK := requeryPaidOrderOnce(ctx, prov, queryRef)
			if !retryOK {
				return checkPaidResultUnavailable
			}
			resp = retriedResp
		}
		notificationTradeNo := o.PaymentTradeNo
		if upstreamTradeNo := strings.TrimSpace(resp.TradeNo); paymentOrderShouldPersistUpstreamTradeNo(queryRef, upstreamTradeNo, notificationTradeNo) {
			if _, updateErr := s.entClient.PaymentOrder.Update().
				Where(paymentorder.IDEQ(o.ID)).
				SetPaymentTradeNo(upstreamTradeNo).
				Save(ctx); updateErr != nil {
				slog.Error("persist upstream trade no during checkPaid failed", "orderID", o.ID, "tradeNo", upstreamTradeNo, "error", updateErr)
			} else {
				o.PaymentTradeNo = upstreamTradeNo
			}
			notificationTradeNo = upstreamTradeNo
		}
		if err := s.HandlePaymentNotification(ctx, &payment.PaymentNotification{TradeNo: notificationTradeNo, OrderID: o.OutTradeNo, Amount: resp.Amount, Status: payment.ProviderStatusSuccess, Metadata: resp.Metadata}, prov.ProviderKey()); err != nil {
			slog.Error("fulfillment failed during checkPaid", "orderID", o.ID, "error", err)
			// Still return already_paid — order was paid, fulfillment can be retried
		}
		return checkPaidResultAlreadyPaid
	}
	if resp.Status == payment.ProviderStatusFailed {
		// A terminal upstream failure already proves that no payment was made.
		// Do not issue a second close/cancel request: providers commonly reject
		// cancellation after the trade has already been closed.
		return checkPaidResultUnpaid
	}
	if !opts.cancelIfUnpaid {
		return checkPaidResultUnpaid
	}
	if cp, ok := prov.(payment.CancelableProvider); ok {
		finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
		cancelErr := cp.CancelPayment(ctx, queryRef)
		finishProviderCall()
		if cancelErr != nil {
			slog.Warn("cancel upstream payment failed", "orderID", o.ID, "queryRef", queryRef, "error", cancelErr)
			return checkPaidResultUnavailable
		}
	}
	return checkPaidResultUnpaid
}

func requeryPaidOrderOnce(ctx context.Context, prov payment.Provider, queryRef string) (*payment.QueryOrderResponse, bool) {
	if prov == nil || strings.TrimSpace(queryRef) == "" {
		return nil, false
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.QueryOrder(ctx, queryRef)
	finishProviderCall()
	if err != nil {
		slog.Warn("query upstream retry failed", "queryRef", queryRef, "error", err)
		return nil, false
	}
	if resp == nil || resp.Status != payment.ProviderStatusPaid || !isValidProviderAmount(resp.Amount) {
		return nil, false
	}
	return resp, true
}

func paymentOrderQueryReference(order *dbent.PaymentOrder, prov payment.Provider) string {
	if order == nil {
		return ""
	}

	providerKey := ""
	if prov != nil {
		providerKey = strings.TrimSpace(prov.ProviderKey())
	}
	if providerKey == "" {
		if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
			providerKey = strings.TrimSpace(snapshot.ProviderKey)
		}
	}
	if providerKey == "" {
		providerKey = strings.TrimSpace(psStringValue(order.ProviderKey))
	}
	if providerKey == "" {
		providerKey = strings.TrimSpace(order.PaymentType)
	}

	switch payment.GetBasePaymentType(providerKey) {
	case payment.TypeEasyPay:
		if snapshot := psOrderProviderSnapshot(order); snapshot != nil && snapshot.Protocol == paymentchannels.EasyPayProtocolBepusdt {
			if tradeNo := strings.TrimSpace(order.PaymentTradeNo); tradeNo != "" {
				return tradeNo
			}
		}
		return strings.TrimSpace(order.OutTradeNo)
	case payment.TypeAlipay, payment.TypeWxpay:
		return strings.TrimSpace(order.OutTradeNo)
	default:
		if tradeNo := strings.TrimSpace(order.PaymentTradeNo); tradeNo != "" {
			return tradeNo
		}
		return strings.TrimSpace(order.OutTradeNo)
	}
}

func paymentOrderShouldPersistUpstreamTradeNo(queryRef, upstreamTradeNo, currentTradeNo string) bool {
	upstreamTradeNo = strings.TrimSpace(upstreamTradeNo)
	if upstreamTradeNo == "" {
		return false
	}
	if strings.EqualFold(upstreamTradeNo, strings.TrimSpace(currentTradeNo)) {
		return false
	}
	if strings.EqualFold(upstreamTradeNo, strings.TrimSpace(queryRef)) {
		return false
	}
	return true
}

// VerifyOrderByOutTradeNo actively queries the upstream provider to check
// if a payment was made, and processes it if so. This handles the case where
// the provider's notify callback was missed (e.g. EasyPay popup mode).
func (s *PaymentService) VerifyOrderByOutTradeNo(ctx context.Context, outTradeNo string, userID int64) (*dbent.PaymentOrder, error) {
	outTradeNo, err := normalizeOrderLookupOutTradeNo(outTradeNo)
	if err != nil {
		return nil, err
	}
	o, err := s.entClient.PaymentOrder.Query().
		Where(paymentorder.OutTradeNo(outTradeNo)).
		Only(ctx)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != userID {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission for this order")
	}
	// Only verify orders that are still pending or recently expired
	if o.Status == OrderStatusPending || o.Status == OrderStatusExpired {
		result := s.reconcilePaid(ctx, o)
		if result == checkPaidResultAlreadyPaid {
			// Reload order to get updated status
			o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
			if err != nil {
				return nil, fmt.Errorf("reload order: %w", err)
			}
		}
	}
	return o, nil
}

// ReconcilePendingPaymentOrders actively checks pending and recently terminal
// payment orders across all configured providers so missed notifications do not
// leave paid orders stuck in PENDING, EXPIRED, or CANCELLED.
func (s *PaymentService) ReconcilePendingPaymentOrders(ctx context.Context) (int, error) {
	now := time.Now()
	orders, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.ExpiresAtGT(now),
		).
		Order(dbent.Asc(paymentorder.FieldCreatedAt)).
		Limit(paymentOrderReconcileLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query pending payment orders: %w", err)
	}

	lateOrders, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.StatusIn(OrderStatusExpired, OrderStatusCancelled),
			paymentorder.UpdatedAtGTE(now.Add(-paymentGraceMinutes*time.Minute)),
		).
		Order(dbent.Asc(paymentorder.FieldUpdatedAt)).
		Limit(paymentOrderReconcileLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query recently terminal payment orders: %w", err)
	}

	fulfillmentOrders, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.Or(
				paymentorder.StatusEQ(OrderStatusPaid),
				paymentorder.And(
					paymentorder.StatusEQ(OrderStatusRecharging),
					paymentorder.UpdatedAtLTE(now.Add(-paymentFulfillmentLeaseDuration)),
				),
			),
		).
		Order(dbent.Asc(paymentorder.FieldUpdatedAt)).
		Limit(paymentOrderReconcileLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query payment orders awaiting fulfillment: %w", err)
	}

	seen := make(map[int64]struct{}, len(orders)+len(lateOrders))
	allOrders := make([]*dbent.PaymentOrder, 0, len(orders)+len(lateOrders))
	for _, order := range append(orders, lateOrders...) {
		if _, ok := seen[order.ID]; ok {
			continue
		}
		seen[order.ID] = struct{}{}
		allOrders = append(allOrders, order)
	}

	recovered := 0
	for _, order := range allOrders {
		result := s.reconcilePaid(ctx, order)
		switch result {
		case checkPaidResultAlreadyPaid:
			recovered++
		case checkPaidResultUnavailable:
			slog.Warn("payment order reconciliation deferred", "orderID", order.ID, "status", order.Status)
		}
	}
	for _, order := range fulfillmentOrders {
		if err := s.executeFulfillment(ctx, order.ID); err != nil {
			slog.Warn("payment order fulfillment retry failed", "orderID", order.ID, "status", order.Status, "error", err)
			continue
		}
		current, err := s.entClient.PaymentOrder.Get(ctx, order.ID)
		if err == nil && current.Status == OrderStatusCompleted {
			recovered++
		}
	}
	return recovered, nil
}

// ReconcilePendingWxpayOrders is kept as a compatibility wrapper for callers
// from older builds; reconciliation is now intentionally provider-agnostic.
func (s *PaymentService) ReconcilePendingWxpayOrders(ctx context.Context) (int, error) {
	return s.ReconcilePendingPaymentOrders(ctx)
}

// VerifyOrderPublic returns the currently persisted public order state without
// triggering any upstream reconciliation. Signed resume-token recovery is the
// only public recovery path allowed to query upstream state.
func (s *PaymentService) VerifyOrderPublic(ctx context.Context, outTradeNo string) (*dbent.PaymentOrder, error) {
	outTradeNo, err := normalizeOrderLookupOutTradeNo(outTradeNo)
	if err != nil {
		return nil, err
	}
	o, err := s.entClient.PaymentOrder.Query().
		Where(paymentorder.OutTradeNo(outTradeNo)).
		Only(ctx)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	return o, nil
}

func normalizeOrderLookupOutTradeNo(raw string) (string, error) {
	outTradeNo := strings.TrimSpace(raw)
	if outTradeNo == "" {
		return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is required")
	}
	if len(outTradeNo) > 64 {
		return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is invalid")
	}
	for _, ch := range outTradeNo {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '_' || ch == '-':
		default:
			return "", infraerrors.BadRequest("INVALID_OUT_TRADE_NO", "out_trade_no is invalid")
		}
	}
	return outTradeNo, nil
}

func (s *PaymentService) ExpireTimedOutOrders(ctx context.Context) (int, error) {
	now := time.Now()
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.StatusEQ(OrderStatusPending), paymentorder.ExpiresAtLTE(now)).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query expired: %w", err)
	}
	n := 0
	for _, o := range orders {
		// Check upstream payment status before expiring — the user may have
		// paid just before timeout and the webhook hasn't arrived yet.
		outcome, err := s.cancelCore(ctx, o, OrderStatusExpired, "system", "order expired")
		if err != nil {
			slog.Warn("defer order expiry after state transition error", "orderID", o.ID, "error", err)
			continue
		}
		if outcome == checkPaidResultAlreadyPaid {
			slog.Info("order was paid during expiry", "orderID", o.ID)
			continue
		}
		if outcome == checkPaidResultUnavailable {
			slog.Warn("defer order expiry because payment status is unavailable", "orderID", o.ID)
			continue
		}
		if outcome == checkPaidResultCancelled {
			n++
		}
	}
	return n, nil
}

// getOrderProvider creates a provider using the order's original instance config.
// Falls back to registry lookup if instance ID is missing (legacy orders).
func (s *PaymentService) getOrderProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("load order provider instance: %w", err)
	}
	if inst != nil {
		return s.createProviderFromInstance(ctx, inst)
	}
	if !paymentOrderAllowsRegistryFallback(o) {
		return nil, fmt.Errorf("order %d provider instance is unresolved", o.ID)
	}
	providerKey := paymentOrderFallbackProviderKey(s.registry, o)
	if providerKey == "" {
		return nil, fmt.Errorf("order %d provider fallback key is missing", o.ID)
	}
	if !s.webhookRegistryFallbackAllowed(ctx, providerKey) {
		return nil, fmt.Errorf("order %d provider fallback is ambiguous for %s", o.ID, providerKey)
	}
	s.EnsureProviders(ctx)
	return s.registry.GetProvider(o.PaymentType)
}

func paymentOrderAllowsRegistryFallback(order *dbent.PaymentOrder) bool {
	if order == nil {
		return false
	}
	if psOrderProviderSnapshot(order) != nil {
		return false
	}
	if strings.TrimSpace(psStringValue(order.ProviderInstanceID)) != "" {
		return false
	}
	if strings.TrimSpace(psStringValue(order.ProviderKey)) != "" {
		return false
	}
	return true
}

func paymentOrderFallbackProviderKey(registry *payment.Registry, order *dbent.PaymentOrder) string {
	if order == nil {
		return ""
	}
	if registry != nil {
		if key := strings.TrimSpace(registry.GetProviderKey(payment.PaymentType(order.PaymentType))); key != "" {
			return key
		}
	}
	return strings.TrimSpace(payment.GetBasePaymentType(strings.TrimSpace(order.PaymentType)))
}

func (s *PaymentService) createProviderFromInstance(ctx context.Context, inst *dbent.PaymentProviderInstance) (payment.Provider, error) {
	if inst == nil {
		return nil, fmt.Errorf("payment provider instance is missing")
	}

	cfg, err := s.loadBalancer.GetInstanceConfig(ctx, int64(inst.ID))
	if err != nil {
		return nil, fmt.Errorf("load provider instance config: %w", err)
	}
	if inst.PaymentMode != "" {
		cfg["paymentMode"] = inst.PaymentMode
	}

	instID := strconv.FormatInt(int64(inst.ID), 10)
	prov, err := createPaymentProviderFromInstance(inst.ProviderKey, instID, cfg)
	if err != nil {
		return nil, fmt.Errorf("create provider from instance: %w", err)
	}
	return prov, nil
}

func psStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
