package paymentchannels

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OrderSelection is the persistence-independent provider instance selected for
// an order. Official payment code maps it to and from its transport types.
type OrderSelection struct {
	InstanceID       string
	ProviderKey      string
	Config           map[string]string
	SupportedTypes   string
	PaymentMode      string
	InstanceRevision string
}

type OrderPreparationRequest struct {
	PaymentType              string
	ProviderKey              string
	OrderType                string
	Amount                   float64
	LimitAmount              float64
	GlobalFeeRate            float64
	ChannelSettings          ChannelSettings
	ResolvedFeeRate          *float64
	SubscriptionUSDToCNYRate float64
	IsWeChatBrowser          bool
	OpenID                   string
	PlanID                   int64
	SourceURL                string
	PaymentNetwork           string
}

type OrderSelectionRequest struct {
	PaymentType      string
	ProviderKey      string
	Strategy         string
	PayAmount        float64
	WeChatJSAPIAppID string
	PaymentNetwork   string
}

type OrderOAuth struct {
	AuthorizeURL string
	AppID        string
	Scope        string
	RedirectURL  string
}

type OrderOAuthRequest struct {
	PaymentType   string
	ProviderKey   string
	OrderType     string
	Amount        float64
	DisplayAmount float64
	PayAmount     float64
	FeeRate       float64
	PlanID        int64
	SourceURL     string
	AppID         string
}

type OrderPreparation struct {
	ProviderKey     string
	FeeRate         float64
	PayAmount       float64
	PayAmountString string
	Selection       *OrderSelection
	OAuth           *OrderOAuth
}

// OrderLoader is the official-side persistence/config/signing adapter used by
// the Custom order coordinator. Implementations must not make channel-policy
// decisions; they only load data, map DTOs, or perform the requested operation.
type OrderLoader interface {
	HasConfiguredSelection(ctx context.Context, paymentType, providerKey string) (bool, error)
	LoadMethodCurrency(ctx context.Context, paymentType, providerKey string) (string, error)
	CalculatePayAmount(limitAmount, feeRate float64, currency, orderType string, usdToCNYRate float64) (string, float64, error)
	SelectOrderInstance(ctx context.Context, request OrderSelectionRequest) (*OrderSelection, error)
	RevalidateOrderInstance(ctx context.Context, selection *OrderSelection, paymentType string, payAmount float64) (bool, error)
	ValidatePayAmountCurrency(payAmount string, selection *OrderSelection) error
	UsesOfficialWeChatVisibleMethod(ctx context.Context) bool
	LoadWeChatOAuthAppID(ctx context.Context) (string, error)
	BuildWeChatOAuth(ctx context.Context, request OrderOAuthRequest) (*OrderOAuth, error)
}

// OrderCoordinator owns the complete custom multi-channel preparation flow.
// The official PaymentService calls this once before persistence and receives a
// fully resolved selection, price and optional OAuth response.
type OrderCoordinator struct {
	loader OrderLoader
}

func NewOrderCoordinator(loader OrderLoader) *OrderCoordinator {
	return &OrderCoordinator{loader: loader}
}

func (c *OrderCoordinator) Prepare(ctx context.Context, request OrderPreparationRequest, strategy string) (*OrderPreparation, error) {
	providerKey := (OrderPolicy{}).NormalizeProviderKey(request.ProviderKey)
	if providerKey != "" {
		configured := false
		if !IsValidSelection(request.PaymentType, providerKey) {
			var err error
			configured, err = c.loader.HasConfiguredSelection(ctx, request.PaymentType, providerKey)
			if err != nil {
				return nil, err
			}
		}
		if err := (OrderPolicy{}).ValidateSelection(request.PaymentType, providerKey, configured); err != nil {
			return nil, err
		}
	}

	feeRate, err := (OrderPolicy{}).ResolveFeeRate(
		request.PaymentType,
		providerKey,
		request.GlobalFeeRate,
		request.ChannelSettings,
		request.ResolvedFeeRate,
	)
	if err != nil {
		return nil, err
	}
	methodCurrency, err := c.loader.LoadMethodCurrency(ctx, request.PaymentType, providerKey)
	if err != nil {
		return nil, err
	}
	payAmountString, payAmount, err := c.loader.CalculatePayAmount(
		request.LimitAmount,
		feeRate,
		methodCurrency,
		request.OrderType,
		request.SubscriptionUSDToCNYRate,
	)
	if err != nil {
		return nil, err
	}

	expectedAppID, err := c.selectionWeChatAppID(ctx, request, providerKey)
	if err != nil {
		return nil, err
	}
	selection, err := c.loader.SelectOrderInstance(ctx, OrderSelectionRequest{
		PaymentType:      request.PaymentType,
		ProviderKey:      providerKey,
		Strategy:         strategy,
		PayAmount:        payAmount,
		WeChatJSAPIAppID: expectedAppID,
		PaymentNetwork:   request.PaymentNetwork,
	})
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_GATEWAY_ERROR", "method_not_configured").
			WithMetadata(map[string]string{"payment_type": request.PaymentType, "provider_key": providerKey})
	}
	if selection == nil {
		return nil, infraerrors.TooManyRequests("NO_AVAILABLE_INSTANCE", "no_available_instance")
	}
	valid, err := c.loader.RevalidateOrderInstance(ctx, selection, request.PaymentType, payAmount)
	if err := (OrderPolicy{}).RevalidationError(request.PaymentType, providerKey, valid, err); err != nil {
		return nil, err
	}
	if expectedAppID == "" && needsWeChatCompatibility(request) && normalize(selection.ProviderKey) == ProviderWxpay {
		expectedAppID, err = c.loader.LoadWeChatOAuthAppID(ctx)
		if err != nil {
			return nil, err
		}
	}
	if err := validateWeChatSelection(request, selection, expectedAppID); err != nil {
		return nil, err
	}
	if err := ValidatePaymentNetworkSelection(request.PaymentType, request.PaymentNetwork, selection); err != nil {
		return nil, err
	}

	selectedCurrency := selectionCurrency(selection)
	if selectedCurrency != methodCurrency {
		payAmountString, payAmount, err = c.loader.CalculatePayAmount(
			request.LimitAmount,
			feeRate,
			selectedCurrency,
			request.OrderType,
			request.SubscriptionUSDToCNYRate,
		)
		if err != nil {
			return nil, err
		}
	}
	if err := c.loader.ValidatePayAmountCurrency(payAmountString, selection); err != nil {
		return nil, err
	}

	result := &OrderPreparation{
		ProviderKey:     providerKey,
		FeeRate:         feeRate,
		PayAmount:       payAmount,
		PayAmountString: payAmountString,
		Selection:       selection,
	}
	if needsWeChatOAuth(request, selection) {
		result.OAuth, err = c.loader.BuildWeChatOAuth(ctx, OrderOAuthRequest{
			PaymentType:   request.PaymentType,
			ProviderKey:   providerKey,
			OrderType:     request.OrderType,
			Amount:        request.Amount,
			DisplayAmount: request.LimitAmount,
			PayAmount:     payAmount,
			FeeRate:       feeRate,
			PlanID:        request.PlanID,
			SourceURL:     request.SourceURL,
			AppID:         expectedAppID,
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (c *OrderCoordinator) RevalidateBeforeProvider(ctx context.Context, paymentType, providerKey string, selection *OrderSelection) error {
	valid, err := c.loader.RevalidateOrderInstance(ctx, selection, paymentType, 0)
	return (OrderPolicy{}).RevalidationError(paymentType, providerKey, valid, err)
}

func (c *OrderCoordinator) selectionWeChatAppID(ctx context.Context, request OrderPreparationRequest, providerKey string) (string, error) {
	if !needsWeChatCompatibility(request) || !(OrderPolicy{}).UsesOfficialWeChat(providerKey, "") {
		return "", nil
	}
	if providerKey == "" && !c.loader.UsesOfficialWeChatVisibleMethod(ctx) {
		return "", nil
	}
	return c.loader.LoadWeChatOAuthAppID(ctx)
}

func needsWeChatCompatibility(request OrderPreparationRequest) bool {
	return normalizeVisibleMethod(request.PaymentType) == MethodWxpay &&
		(request.IsWeChatBrowser || strings.TrimSpace(request.OpenID) != "")
}

func needsWeChatOAuth(request OrderPreparationRequest, selection *OrderSelection) bool {
	return selection != nil &&
		(OrderPolicy{}).UsesOfficialWeChat(request.ProviderKey, selection.ProviderKey) &&
		strings.TrimSpace(request.OpenID) == "" &&
		request.IsWeChatBrowser &&
		normalizeVisibleMethod(request.PaymentType) == MethodWxpay
}

func validateWeChatSelection(request OrderPreparationRequest, selection *OrderSelection, expectedAppID string) error {
	if selection == nil || normalize(selection.ProviderKey) != ProviderWxpay || !needsWeChatCompatibility(request) {
		return nil
	}
	selectedAppID := resolveWeChatJSAPIAppID(selection.Config)
	if selectedAppID == "" || selectedAppID != strings.TrimSpace(expectedAppID) {
		return infraerrors.TooManyRequests("NO_AVAILABLE_INSTANCE", "selected payment instance is not compatible with the current WeChat OAuth app")
	}
	return nil
}

func selectionCurrency(selection *OrderSelection) string {
	if selection == nil {
		return DefaultCurrency
	}
	switch normalize(selection.ProviderKey) {
	case ProviderStripe, ProviderAirwallex:
		if currency := strings.ToUpper(strings.TrimSpace(selection.Config["currency"])); currency != "" {
			return currency
		}
	}
	return DefaultCurrency
}

func resolveWeChatJSAPIAppID(config map[string]string) string {
	if appID := strings.TrimSpace(config["mpAppId"]); appID != "" {
		return appID
	}
	return strings.TrimSpace(config["appId"])
}
