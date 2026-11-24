//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBuildPaymentOrderProviderSnapshot_ExcludesSensitiveConfig(t *testing.T) {
	t.Parallel()

	sel := &payment.InstanceSelection{
		InstanceID:     "12",
		ProviderKey:    payment.TypeWxpay,
		SupportedTypes: "wxpay,wxpay_direct",
		PaymentMode:    "popup",
		Config: map[string]string{
			"privateKey": "secret",
			"apiV3Key":   "secret-v3",
			"appId":      "wx-app-id",
		},
	}

	snapshot := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{})
	require.Equal(t, map[string]any{
		"schema_version":       2,
		"provider_instance_id": "12",
		"provider_key":         payment.TypeWxpay,
		"payment_mode":         "popup",
		"merchant_app_id":      "wx-app-id",
		"currency":             "CNY",
	}, snapshot)
	require.NotContains(t, snapshot, "config")
	require.NotContains(t, snapshot, "privateKey")
	require.NotContains(t, snapshot, "apiV3Key")
	require.NotContains(t, snapshot, "supported_types")
	require.NotContains(t, snapshot, "instance_name")
	require.NotContains(t, snapshot, "merchant_id")
}

func TestCreateOrderInTx_WritesProviderSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("snapshot@example.com").
		SetPasswordHash("hash").
		SetUsername("snapshot-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("Primary Alipay").
		SetConfig(`{"secretKey":"do-not-copy"}`).
		SetSupportedTypes("alipay,alipay_direct").
		SetPaymentMode("redirect").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(
		ctx,
		CreateOrderRequest{
			UserID:      user.ID,
			PaymentType: payment.TypeAlipay,
			OrderType:   payment.OrderTypeBalance,
			ClientIP:    "127.0.0.1",
			SrcHost:     "app.example.com",
		},
		&User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
		},
		nil,
		false,
		&PaymentConfig{
			MaxPendingOrders: 3,
			OrderTimeoutMin:  30,
		},
		88,
		88,
		0,
		88,
		0,
		&payment.InstanceSelection{
			InstanceID:     strconv.FormatInt(instance.ID, 10),
			ProviderKey:    payment.TypeAlipay,
			SupportedTypes: "alipay,alipay_direct",
			PaymentMode:    "redirect",
			Config: map[string]string{
				"secretKey": "do-not-copy",
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(instance.ID, 10), valueOrEmpty(order.ProviderInstanceID))
	require.Equal(t, payment.TypeAlipay, valueOrEmpty(order.ProviderKey))
	require.Equal(t, float64(2), order.ProviderSnapshot["schema_version"])
	require.Equal(t, strconv.FormatInt(instance.ID, 10), order.ProviderSnapshot["provider_instance_id"])
	require.Equal(t, payment.TypeAlipay, order.ProviderSnapshot["provider_key"])
	require.Equal(t, "redirect", order.ProviderSnapshot["payment_mode"])
	require.NotContains(t, order.ProviderSnapshot, "config")
	require.NotContains(t, order.ProviderSnapshot, "secretKey")
	require.NotContains(t, order.ProviderSnapshot, "supported_types")
	require.NotContains(t, order.ProviderSnapshot, "instance_name")
}

func TestBuildPaymentOrderProviderSnapshot_UsesWxpayJSAPIAppIDForOpenIDOrders(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "88",
		ProviderKey: payment.TypeWxpay,
		Config: map[string]string{
			"appId":   "wx-open-app",
			"mpAppId": "wx-mp-app",
			"mchId":   "mch-88",
		},
		PaymentMode: "jsapi",
	}, CreateOrderRequest{OpenID: "openid-123"})

	require.Equal(t, "wx-mp-app", snapshot["merchant_app_id"])
	require.Equal(t, "mch-88", snapshot["merchant_id"])
	require.Equal(t, "CNY", snapshot["currency"])
}

func TestBuildPaymentOrderProviderSnapshot_IncludesAlipayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "21",
		ProviderKey: payment.TypeAlipay,
		Config: map[string]string{
			"appId":      "alipay-app-21",
			"privateKey": "secret",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{})

	require.Equal(t, "alipay-app-21", snapshot["merchant_app_id"])
	require.NotContains(t, snapshot, "privateKey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesEasyPayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "66",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid":  "easypay-merchant-66",
			"pkey": "secret",
		},
		PaymentMode: "popup",
	}, CreateOrderRequest{PaymentType: payment.TypeAlipay})

	require.Equal(t, "easypay-merchant-66", snapshot["merchant_id"])
	require.NotContains(t, snapshot, "pkey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesBepusdtNetwork(t *testing.T) {
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "67",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"easypayProtocol": "bepusdt_native",
			"bepusdtNetworks": "bep20",
		},
		PaymentMode: "popup",
	}, CreateOrderRequest{PaymentType: "usdt", PaymentNetwork: "bep20"})

	require.Equal(t, "bepusdt_native", snapshot["protocol"])
	require.Equal(t, "bep20", snapshot["payment_network"])
	require.Equal(t, "usdt.bep20", snapshot["upstream_trade_type"])
}

func TestBuildPaymentOrderProviderSnapshot_IncludesGmpayNetwork(t *testing.T) {
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "68",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid":             "1000",
			"easypayProtocol": "epusdt_gmpay",
			"gmpayNetworks":   "trc20",
			"secretKey":       "secret",
		},
		PaymentMode: "qrcode",
	}, CreateOrderRequest{PaymentType: "usdt", PaymentNetwork: "trc20"})

	require.Equal(t, "epusdt_gmpay", snapshot["protocol"])
	require.Equal(t, "1000", snapshot["merchant_id"])
	require.Equal(t, "trc20", snapshot["payment_network"])
	require.Equal(t, "tron", snapshot["upstream_network"])
	require.NotContains(t, snapshot, "upstream_trade_type")
	require.NotContains(t, snapshot, "secretKey")
}

func TestValidateProviderSnapshotMetadata_GmpayNetworkBinding(t *testing.T) {
	gmpayOrder := func() *dbent.PaymentOrder {
		return &dbent.PaymentOrder{
			PaymentType: "usdt",
			ProviderSnapshot: map[string]any{
				"schema_version":   2,
				"provider_key":     payment.TypeEasyPay,
				"merchant_id":      "1000",
				"protocol":         "epusdt_gmpay",
				"payment_network":  "trc20",
				"upstream_network": "tron",
			},
		}
	}

	t.Run("matching network and pid pass", func(t *testing.T) {
		err := validateProviderSnapshotMetadata(gmpayOrder(), payment.TypeEasyPay, map[string]string{
			"pid": "1000", "network": "tron",
		})
		require.NoError(t, err)
	})

	t.Run("pid mismatch fails", func(t *testing.T) {
		err := validateProviderSnapshotMetadata(gmpayOrder(), payment.TypeEasyPay, map[string]string{
			"pid": "9999", "network": "tron",
		})
		require.ErrorContains(t, err, "pid mismatch")
	})

	t.Run("cashier network switch fails for manual review", func(t *testing.T) {
		err := validateProviderSnapshotMetadata(gmpayOrder(), payment.TypeEasyPay, map[string]string{
			"pid": "1000", "network": "binance",
		})
		require.ErrorContains(t, err, "gmpay network mismatch")
	})

	// Orders are always created with callback_version=2, so a verified
	// callback without the network field is malformed and must be rejected.
	t.Run("callback without network field fails", func(t *testing.T) {
		err := validateProviderSnapshotMetadata(gmpayOrder(), payment.TypeEasyPay, map[string]string{
			"pid": "1000",
		})
		require.ErrorContains(t, err, "missing network")
	})
}

func TestValidateGmpayPaymentBinding(t *testing.T) {
	gmpayOrder := func(tradeNo string, payAmount float64) *dbent.PaymentOrder {
		return &dbent.PaymentOrder{
			PaymentType:    "usdt",
			PaymentTradeNo: tradeNo,
			PayAmount:      payAmount,
			ProviderSnapshot: map[string]any{
				"schema_version":   2,
				"provider_key":     payment.TypeEasyPay,
				"protocol":         "epusdt_gmpay",
				"payment_network":  "trc20",
				"upstream_network": "tron",
			},
		}
	}

	t.Run("matching trade and exact amount pass", func(t *testing.T) {
		require.NoError(t, validateGmpayPaymentBinding(gmpayOrder("trade-1", 100), "trade-1", 100))
		// Canonical -1 formatting: upstream precision must not fail a match.
		require.NoError(t, validateGmpayPaymentBinding(gmpayOrder("trade-1", 100), "trade-1", 100.0000))
	})

	t.Run("trade mismatch fails", func(t *testing.T) {
		err := validateGmpayPaymentBinding(gmpayOrder("trade-1", 100), "trade-other", 100)
		require.ErrorContains(t, err, "trade_no mismatch")
	})

	t.Run("amount mismatch fails exact comparison", func(t *testing.T) {
		err := validateGmpayPaymentBinding(gmpayOrder("trade-1", 100), "trade-1", 99.99)
		require.ErrorContains(t, err, "amount mismatch")
	})

	t.Run("empty stored trade is rejected", func(t *testing.T) {
		// GMPay orders always persist the creation trade_id; an empty stored
		// value means the binding was lost and must not fulfill.
		err := validateGmpayPaymentBinding(gmpayOrder("", 100), "trade-1", 100)
		require.ErrorContains(t, err, "trade_no mismatch")
	})

	t.Run("empty notified trade is rejected", func(t *testing.T) {
		err := validateGmpayPaymentBinding(gmpayOrder("trade-1", 100), "", 100)
		require.ErrorContains(t, err, "trade_no mismatch")
	})

	t.Run("trade comparison is case-sensitive", func(t *testing.T) {
		err := validateGmpayPaymentBinding(gmpayOrder("Trade-1", 100), "trade-1", 100)
		require.ErrorContains(t, err, "trade_no mismatch")
	})

	t.Run("non-gmpay orders skip strict binding", func(t *testing.T) {
		order := &dbent.PaymentOrder{
			PaymentType:    "usdt",
			PaymentTradeNo: "trade-1",
			PayAmount:      100,
			ProviderSnapshot: map[string]any{
				"schema_version": 2,
				"provider_key":   payment.TypeEasyPay,
				"protocol":       "bepusdt_native",
			},
		}
		require.NoError(t, validateGmpayPaymentBinding(order, "trade-other", 99.99))
	})
}

func TestBuildPaymentOrderProviderSnapshot_IncludesProviderCurrency(t *testing.T) {
	t.Parallel()

	stripeSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "77",
		ProviderKey: payment.TypeStripe,
		Config: map[string]string{
			"currency": "hkd",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "HKD", stripeSnapshot["currency"])

	airwallexSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "78",
		ProviderKey: payment.TypeAirwallex,
		Config: map[string]string{
			"currency":  "usd",
			"accountId": "acct-78",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "USD", airwallexSnapshot["currency"])
	require.Equal(t, "acct-78", airwallexSnapshot["merchant_id"])
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
