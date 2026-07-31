package paymentchannels

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type orderLoaderStub struct {
	configured      bool
	methodCurrency  string
	selection       *OrderSelection
	selectErr       error
	revalidateValid bool
	revalidateErr   error
	usesOfficial    bool
	expectedAppID   string
	oauth           *OrderOAuth
	calculated      []string
	oauthCalls      int
	appIDLoads      int
}

func (s *orderLoaderStub) HasConfiguredSelection(context.Context, string, string) (bool, error) {
	return s.configured, nil
}

func (s *orderLoaderStub) LoadMethodCurrency(context.Context, string, string) (string, error) {
	return s.methodCurrency, nil
}

func (s *orderLoaderStub) CalculatePayAmount(_ float64, _ float64, currency, _ string, _ float64) (string, float64, error) {
	s.calculated = append(s.calculated, currency)
	if currency == "USD" {
		return "12.34", 12.34, nil
	}
	return "88.00", 88, nil
}

func (s *orderLoaderStub) SelectOrderInstance(context.Context, OrderSelectionRequest) (*OrderSelection, error) {
	return s.selection, s.selectErr
}

func (s *orderLoaderStub) RevalidateOrderInstance(context.Context, *OrderSelection, string, float64) (bool, error) {
	return s.revalidateValid, s.revalidateErr
}

func (*orderLoaderStub) ValidatePayAmountCurrency(string, *OrderSelection) error { return nil }

func (s *orderLoaderStub) UsesOfficialWeChatVisibleMethod(context.Context) bool {
	return s.usesOfficial
}

func (s *orderLoaderStub) LoadWeChatOAuthAppID(context.Context) (string, error) {
	s.appIDLoads++
	return s.expectedAppID, nil
}

func (s *orderLoaderStub) BuildWeChatOAuth(context.Context, OrderOAuthRequest) (*OrderOAuth, error) {
	s.oauthCalls++
	return s.oauth, nil
}

func TestOrderCoordinatorRepricesForSelectedCurrency(t *testing.T) {
	loader := &orderLoaderStub{
		methodCurrency:  DefaultCurrency,
		revalidateValid: true,
		selection: &OrderSelection{
			InstanceID:  "1",
			ProviderKey: ProviderStripe,
			Config:      map[string]string{"currency": "usd"},
		},
	}
	result, err := NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType:   MethodStripe,
		ProviderKey:   ProviderStripe,
		OrderType:     OrderTypeBalance,
		LimitAmount:   10,
		GlobalFeeRate: 2,
	}, StrategyRoundRobin)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.PayAmountString != "12.34" || result.PayAmount != 12.34 {
		t.Fatalf("result = %+v", result)
	}
	if len(loader.calculated) != 2 || loader.calculated[0] != DefaultCurrency || loader.calculated[1] != "USD" {
		t.Fatalf("calculated currencies = %v", loader.calculated)
	}
}

func TestOrderCoordinatorRejectsInvalidExplicitProvider(t *testing.T) {
	loader := &orderLoaderStub{methodCurrency: DefaultCurrency, revalidateValid: true}
	_, err := NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType: MethodAlipay,
		ProviderKey: ProviderWxpay,
	}, StrategyRoundRobin)
	if infraerrors.Reason(err) != "INVALID_PAYMENT_PROVIDER_SELECTION" {
		t.Fatalf("reason = %q, err = %v", infraerrors.Reason(err), err)
	}
}

func TestOrderCoordinatorBuildsOAuthOnlyForOfficialWeChat(t *testing.T) {
	loader := &orderLoaderStub{
		methodCurrency:  DefaultCurrency,
		revalidateValid: true,
		expectedAppID:   "wx-app",
		oauth:           &OrderOAuth{AuthorizeURL: "/oauth", AppID: "wx-app"},
		selection: &OrderSelection{
			InstanceID:  "1",
			ProviderKey: ProviderWxpay,
			Config:      map[string]string{"mpAppId": "wx-app"},
		},
	}
	result, err := NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType:     MethodWxpay,
		ProviderKey:     ProviderWxpay,
		OrderType:       OrderTypeBalance,
		Amount:          20,
		LimitAmount:     20,
		IsWeChatBrowser: true,
	}, StrategyRoundRobin)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.OAuth == nil || loader.oauthCalls != 1 {
		t.Fatalf("oauth = %+v, calls = %d", result.OAuth, loader.oauthCalls)
	}

	loader.oauthCalls = 0
	loader.selection = &OrderSelection{InstanceID: "2", ProviderKey: ProviderEasyPay}
	result, err = NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType:     MethodWxpay,
		ProviderKey:     ProviderEasyPay,
		OrderType:       OrderTypeBalance,
		Amount:          20,
		LimitAmount:     20,
		IsWeChatBrowser: true,
	}, StrategyRoundRobin)
	if err != nil {
		t.Fatalf("EasyPay Prepare() error = %v", err)
	}
	if result.OAuth != nil || loader.oauthCalls != 0 {
		t.Fatalf("EasyPay OAuth = %+v, calls = %d", result.OAuth, loader.oauthCalls)
	}
}

func TestOrderCoordinatorLoadsOAuthAppIDAfterProviderAgnosticOfficialWeChatSelection(t *testing.T) {
	loader := &orderLoaderStub{
		methodCurrency:  DefaultCurrency,
		revalidateValid: true,
		usesOfficial:    false,
		expectedAppID:   "wx-app",
		oauth:           &OrderOAuth{AuthorizeURL: "/oauth", AppID: "wx-app"},
		selection: &OrderSelection{
			InstanceID:  "1",
			ProviderKey: ProviderWxpay,
			Config:      map[string]string{"mpAppId": "wx-app"},
		},
	}

	result, err := NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType:     MethodWxpay,
		OrderType:       OrderTypeBalance,
		Amount:          20,
		LimitAmount:     20,
		IsWeChatBrowser: true,
	}, StrategyRoundRobin)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.OAuth == nil || loader.oauthCalls != 1 {
		t.Fatalf("oauth = %+v, calls = %d", result.OAuth, loader.oauthCalls)
	}
	if loader.appIDLoads != 1 {
		t.Fatalf("LoadWeChatOAuthAppID() calls = %d, want 1", loader.appIDLoads)
	}
}

func TestOrderCoordinatorFailsClosedOnRevalidationError(t *testing.T) {
	loader := &orderLoaderStub{
		methodCurrency: DefaultCurrency,
		selection:      &OrderSelection{InstanceID: "1", ProviderKey: ProviderAlipay},
		revalidateErr:  errors.New("database unavailable"),
	}
	_, err := NewOrderCoordinator(loader).Prepare(context.Background(), OrderPreparationRequest{
		PaymentType: MethodAlipay,
		ProviderKey: ProviderAlipay,
		LimitAmount: 10,
	}, StrategyRoundRobin)
	if infraerrors.Reason(err) != "PAYMENT_GATEWAY_ERROR" {
		t.Fatalf("reason = %q, err = %v", infraerrors.Reason(err), err)
	}
}
