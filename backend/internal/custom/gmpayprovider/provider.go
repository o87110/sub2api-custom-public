// Package gmpayprovider holds the EPUSDT GMPay provider orchestration for the
// EasyPay bridge. The official EasyPay provider keeps only a one-line
// delegation per method (paymentchannels.IsGmpayConfig → the functions here),
// so protocol behavior — request assembly, status mapping, notification
// parsing and confirmation — lives in Custom instead of the official bridge.
package gmpayprovider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	paymentchannels "github.com/Wei-Shaw/sub2api/internal/custom/paymentchannels"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// clientFromConfig builds a GMPay client from an instance config projection.
func clientFromConfig(apiBase string, config map[string]string) (*paymentchannels.GmpayClient, error) {
	return paymentchannels.NewGmpayClient(
		apiBase,
		config[paymentchannels.GmpayPIDConfigKey],
		config[paymentchannels.GmpaySecretKeyConfigKey],
	)
}

// resolveURLs mirrors the EasyPay request-first URL fallback.
func resolveURLs(config map[string]string, req payment.CreatePaymentRequest) (string, string) {
	notifyURL := req.NotifyURL
	if notifyURL == "" {
		notifyURL = config["notifyUrl"]
	}
	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = config["returnUrl"]
	}
	return notifyURL, returnURL
}

// CreatePayment creates an EPUSDT GMPay native order with a concrete
// token+network (never a status=4 placeholder). The QRCode carries the
// receiving address for in-site QR display; CryptoAmount carries the actual
// USDT amount payable. The upstream expiration is governed by its own order
// settings (GMPay v2.1.4 has no per-request timeout field).
func CreatePayment(ctx context.Context, apiBase string, config map[string]string, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if err := paymentchannels.ValidateGmpayPaymentNetwork(config, req.PaymentType, req.PaymentNetwork); err != nil {
		return nil, err
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(req.Amount), 64)
	if err != nil || amount <= 0 {
		return nil, fmt.Errorf("invalid GMPay payment amount")
	}
	client, err := clientFromConfig(apiBase, config)
	if err != nil {
		return nil, err
	}
	network, _ := paymentchannels.GmpayNetworkByCode(req.PaymentNetwork)
	notifyURL, returnURL := resolveURLs(config, req)
	response, err := client.CreateTransaction(ctx, paymentchannels.GmpayCreateRequest{
		OrderID: req.OrderID, NotifyURL: notifyURL, RedirectURL: returnURL,
		Amount: amount, Fiat: paymentchannels.GmpayFiat,
		Token: paymentchannels.GmpayToken, Network: network.UpstreamNetwork,
		Name: req.Subject,
	})
	if err != nil {
		return nil, fmt.Errorf("gmpay create: %w", err)
	}
	return &payment.CreatePaymentResponse{
		TradeNo:      response.TradeID,
		PayURL:       response.PaymentURL,
		QRCode:       response.ReceiveAddress,
		CryptoAmount: response.ActualAmount,
	}, nil
}

// GmpayManualReviewMetadataKey marks a query result whose upstream order is
// awaiting human review (status 6): the local lifecycle must not expire or
// cancel such an order, because the on-chain payment may already be settled.
const GmpayManualReviewMetadataKey = "gmpay_manual_review"

// GmpayQueryPaidMetadataKey marks a paid result that came from the checkout
// projection rather than a verified callback. When a child order settles a
// switched-network payment, the upstream only flips the parent's status and
// pay_by_sub_id — the parent projection keeps the originally selected
// network, amount and address. A query therefore proves that *some* payment
// settled the order but cannot prove the paid network matches the snapshot,
// so reconciliation must NOT auto-fulfill on it; the order waits for the
// signed callback (which reports the settled child's network) or a human.
const GmpayQueryPaidMetadataKey = "gmpay_query_paid"

// QueryOrder reconciles an order through the public cashier checkout
// projection — the only order query GMPay exposes; there is no
// merchant-authenticated lookup endpoint. See GmpayQueryPaidMetadataKey for
// why a paid projection is flagged instead of being trusted for fulfillment.
// Status 6 (manual review) stays pending and is flagged so expiry/cancel
// hold and the hold persists locally across later query outages.
func QueryOrder(ctx context.Context, apiBase string, config map[string]string, tradeNo string) (*payment.QueryOrderResponse, error) {
	client, err := clientFromConfig(apiBase, config)
	if err != nil {
		return nil, err
	}
	checkout, err := client.QueryCheckout(ctx, tradeNo)
	if err != nil {
		return nil, fmt.Errorf("gmpay query: %w", err)
	}
	var status string
	switch checkout.Status {
	case paymentchannels.GmpayStatusPaid:
		status = payment.ProviderStatusPaid
	case paymentchannels.GmpayStatusExpired:
		status = payment.ProviderStatusFailed
	case paymentchannels.GmpayStatusWaitSelect,
		paymentchannels.GmpayStatusWaitPay,
		paymentchannels.GmpayStatusConfirming,
		paymentchannels.GmpayStatusManualReview:
		status = payment.ProviderStatusPending
	default:
		return nil, fmt.Errorf("gmpay query returned unsupported status")
	}
	var metadata map[string]string
	switch {
	case status == payment.ProviderStatusPaid:
		metadata = map[string]string{GmpayQueryPaidMetadataKey: "true"}
	case checkout.Status == paymentchannels.GmpayStatusManualReview:
		metadata = map[string]string{GmpayManualReviewMetadataKey: "true"}
	}
	return &payment.QueryOrderResponse{TradeNo: checkout.TradeID, Status: status, Amount: checkout.Amount, Metadata: metadata}, nil
}

// ConfirmNotification re-checks a verified success notification against the
// upstream order before fulfillment. The checkout projection has no order_id
// field; the locally stored trade_id binds it to this order. The fiat amount
// is compared exactly after canonical -1 precision formatting (no float
// tolerance).
func ConfirmNotification(ctx context.Context, apiBase string, config map[string]string, n *payment.PaymentNotification) error {
	if n == nil || n.Status != payment.NotificationStatusSuccess {
		return nil
	}
	client, err := clientFromConfig(apiBase, config)
	if err != nil {
		return err
	}
	checkout, err := client.QueryCheckout(ctx, n.TradeNo)
	if err != nil {
		return fmt.Errorf("gmpay confirm: %w", err)
	}
	if checkout.Status != paymentchannels.GmpayStatusPaid || checkout.TradeID != n.TradeNo || !paymentchannels.GmpayAmountEquals(checkout.Amount, n.Amount) {
		return fmt.Errorf("gmpay confirm: trade, status, or amount mismatch")
	}
	return nil
}

// VerifyNotification verifies and parses a GMPay JSON callback. The full
// callback projection is exposed as metadata so the snapshot validation can
// check pid/network binding before fulfillment.
func VerifyNotification(config map[string]string, rawBody string) (*payment.PaymentNotification, error) {
	notification, err := paymentchannels.ParseGmpayNotification(rawBody, config[paymentchannels.GmpaySecretKeyConfigKey])
	if err != nil {
		return nil, err
	}
	status := payment.ProviderStatusFailed
	if notification.Status == paymentchannels.GmpayStatusPaid {
		status = payment.ProviderStatusSuccess
	}
	return &payment.PaymentNotification{
		TradeNo: notification.TradeID, OrderID: notification.OrderID,
		Amount: notification.Amount, Status: status, RawData: rawBody,
		Metadata: map[string]string{
			"protocol":             paymentchannels.EasyPayProtocolGmpay,
			"pid":                  notification.PID,
			"network":              notification.Network,
			"actual_amount":        strconv.FormatFloat(notification.ActualAmount, 'f', -1, 64),
			"receive_address":      notification.ReceiveAddress,
			"block_transaction_id": notification.BlockTransactionID,
		},
	}, nil
}
