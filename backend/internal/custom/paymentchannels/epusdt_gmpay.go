package paymentchannels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EPUSDT GMPay native protocol (EPUSDT v2.1.4, HMAC-SHA256 merchant API).
//
// Upstream facts verified against v2.1.4 source (src/middleware/check_sign.go,
// src/util/sign/sign.go, src/model/service/order_service.go, src/mq/worker.go):
//   - Request signature: lowercase hex HMAC-SHA256(canonical_params, secret_key).
//     canonical_params drops "signature", nil and empty-string values, formats
//     numbers with -1 precision, sorts keys in ASCII order and joins as k=v&.
//   - Notifications are JSON POST, sent only on payment success (status=2),
//     signed with the same rule; v2 callbacks additionally carry "network".
//   - The signing secret is pinned per order at creation time
//     (PinCallbackSigningKey), so after a merchant secret rotation historical
//     callbacks still arrive signed with the old key. Verification failures
//     must therefore be retried/reconciled, not silently dropped.
//   - The checkout counter may switch networks (/pay/switch-network) which
//     settles a child order: the callback then reports the parent
//     trade_id/order_id/amount but the child actual_amount/receive_address/
//     token/network. Fiat amount is unchanged.
//   - Checkout queries (/pay/check-status, /pay/checkout-counter-resp) are
//     public cashier endpoints without merchant auth; safety relies on trade_id
//     secrecy plus HTTPS to the configured apiBase.
const (
	EasyPayProtocolGmpay    = "epusdt_gmpay"
	GmpayNetworksConfigKey  = "gmpayNetworks"
	GmpaySecretKeyConfigKey = "secretKey"
	GmpayPIDConfigKey       = "pid"
	GmpayFiat               = "CNY"
	GmpayToken              = "usdt"
	maxGmpayResponseSize    = 1 << 20
	gmpayHTTPTimeout        = 10 * time.Second
	// GmpayCallbackVersion2 opts into the signed "network" field on callbacks.
	GmpayCallbackVersion2 = 2
)

// EPUSDT order statuses (status enums of CreateTransactionResponse).
const (
	GmpayStatusWaitPay      = 1 // 等待支付
	GmpayStatusPaid         = 2 // 支付成功
	GmpayStatusExpired      = 3 // 已过期
	GmpayStatusWaitSelect   = 4 // 等待选择支付网络/币种（本协议建单必带 token+network，不应出现）
	GmpayStatusConfirming   = 5 // 链上确认中
	GmpayStatusManualReview = 6 // 待人工复核
)

// GmpayNetwork maps a public network code to the GMPay upstream network
// identifier. The public codes intentionally match BepusdtNetwork codes so the
// shared network dialog and channel aggregation keep working when both native
// protocols are enabled side by side.
type GmpayNetwork struct {
	Code            string
	UpstreamNetwork string
	DisplayName     string
}

var gmpayNetworkByCode = map[string]GmpayNetwork{
	"bep20":   {Code: "bep20", UpstreamNetwork: "binance", DisplayName: "BEP20"},
	"trc20":   {Code: "trc20", UpstreamNetwork: "tron", DisplayName: "TRC20"},
	"polygon": {Code: "polygon", UpstreamNetwork: "polygon", DisplayName: "Polygon"},
}

func IsGmpayConfig(config map[string]string) bool {
	protocol, err := NormalizeEasyPayProtocol(config[EasyPayProtocolConfigKey])
	return err == nil && protocol == EasyPayProtocolGmpay
}

func GmpayNetworkByCode(code string) (GmpayNetwork, bool) {
	network, ok := gmpayNetworkByCode[strings.ToLower(strings.TrimSpace(code))]
	return network, ok
}

func ParseGmpayNetworks(raw string) ([]GmpayNetwork, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]GmpayNetwork, 0, len(parts))
	for _, part := range parts {
		code := strings.ToLower(strings.TrimSpace(part))
		if code == "" {
			continue
		}
		network, ok := GmpayNetworkByCode(code)
		if !ok {
			return nil, fmt.Errorf("unsupported GMPay network: %s", code)
		}
		if _, exists := seen[code]; exists {
			return nil, fmt.Errorf("duplicate GMPay network: %s", code)
		}
		seen[code] = struct{}{}
		result = append(result, network)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one GMPay network is required")
	}
	// Reuse the shared public network ordering (bep20, trc20, polygon, ...).
	sort.SliceStable(result, func(i, j int) bool {
		return networkOrderIndex(result[i].Code) < networkOrderIndex(result[j].Code)
	})
	return result, nil
}

func GmpayNetworkOptions(config map[string]string) []NetworkOption {
	networks, err := ParseGmpayNetworks(config[GmpayNetworksConfigKey])
	if err != nil {
		return nil
	}
	options := make([]NetworkOption, 0, len(networks))
	for _, network := range networks {
		options = append(options, NetworkOption{Code: network.Code, DisplayName: network.DisplayName})
	}
	return options
}

// ValidateGmpayRequiredConfig checks the mandatory GMPay instance keys:
// merchant pid, secret key and the callback URLs.
func ValidateGmpayRequiredConfig(config map[string]string) error {
	for _, k := range []string{GmpayPIDConfigKey, GmpaySecretKeyConfigKey, "notifyUrl", "returnUrl"} {
		if strings.TrimSpace(config[k]) == "" {
			return fmt.Errorf("easypay config missing required key: %s", k)
		}
	}
	return nil
}

// GmpayValidateAPIBase enforces an HTTPS gateway for production instances
// (plain HTTP remains reachable only from non-exported test construction).
func GmpayValidateAPIBase(apiBase string) error {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("GMPay api base must use HTTPS")
	}
	return nil
}

func ValidateGmpayPaymentNetwork(config map[string]string, paymentType, network string) error {
	if strings.TrimSpace(network) == "" {
		return fmt.Errorf("payment network is required for GMPay USDT")
	}
	if strings.ToLower(strings.TrimSpace(paymentType)) != BepusdtPaymentType {
		return fmt.Errorf("payment network is only supported for USDT")
	}
	if !IsGmpayConfig(config) {
		return fmt.Errorf("payment network requires GMPay native protocol")
	}
	configured, err := ParseGmpayNetworks(config[GmpayNetworksConfigKey])
	if err != nil {
		return err
	}
	code := strings.ToLower(strings.TrimSpace(network))
	for _, item := range configured {
		if item.Code == code {
			return nil
		}
	}
	return fmt.Errorf("GMPay network is not enabled: %s", code)
}

// ValidateGmpayProviderMode enforces the GMPay protocol constraints when an
// easypay instance selects the epusdt_gmpay protocol: usdt only, no refunds,
// qrcode or popup payment mode, a valid network whitelist and an HTTPS api
// base (plain HTTP is only reachable through non-exported test construction).
func ValidateGmpayProviderMode(config map[string]string, supportedTypes, paymentMode string, refundEnabled bool) error {
	if !IsGmpayConfig(config) {
		return nil
	}
	mode := strings.TrimSpace(paymentMode)
	if mode != "qrcode" && mode != "popup" {
		return fmt.Errorf("GMPay mode requires qrcode or popup payment mode")
	}
	if refundEnabled {
		return fmt.Errorf("GMPay mode does not support refunds")
	}
	if _, err := ParseGmpayNetworks(config[GmpayNetworksConfigKey]); err != nil {
		return err
	}
	types := strings.Split(supportedTypes, ",")
	if len(types) != 1 || strings.ToLower(strings.TrimSpace(types[0])) != BepusdtPaymentType {
		return fmt.Errorf("GMPay mode supports only usdt")
	}
	if base := strings.TrimSpace(config["apiBase"]); base != "" {
		if err := GmpayValidateAPIBase(base); err != nil {
			return err
		}
	}
	return nil
}

type GmpayCreateRequest struct {
	OrderID     string
	NotifyURL   string
	RedirectURL string
	Amount      float64
	Fiat        string
	Token       string
	Network     string // GMPay upstream network: binance, tron, polygon
	Name        string
}

type GmpayCreateResponse struct {
	TradeID        string
	OrderID        string
	Amount         float64
	Currency       string
	ActualAmount   float64
	ReceiveAddress string
	Token          string
	Status         int
	ExpirationTime int64
	PaymentURL     string
}

// GmpayCheckoutResponse is the /pay/checkout-counter-resp projection used for
// order reconciliation. The upstream payload has no order_id field; callers
// bind it through the locally stored trade_id.
type GmpayCheckoutResponse struct {
	TradeID        string
	Amount         float64
	ActualAmount   float64
	Token          string
	Currency       string
	ReceiveAddress string
	Network        string
	Status         int
	ExpirationTime int64
	PaymentType    string
	IsSelected     bool
}

type GmpayClient struct {
	baseURL    string
	pid        string
	secretKey  string
	httpClient *http.Client
}

func NewGmpayClient(baseURL, pid, secretKey string) (*GmpayClient, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid GMPay api base")
	}
	pid = strings.TrimSpace(pid)
	if pid == "" {
		return nil, fmt.Errorf("GMPay merchant pid is required")
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, fmt.Errorf("GMPay secret key is required")
	}
	return &GmpayClient{baseURL: base, pid: pid, secretKey: secretKey, httpClient: &http.Client{Timeout: gmpayHTTPTimeout}}, nil
}

// CreateTransaction calls POST /payments/gmpay/v1/order/create-transaction.
// The upstream replays an existing order when the same order_id is submitted
// with identical parameters, and answers 10002 (order ID conflict) otherwise;
// both cases surface through the returned error so callers can distinguish
// them via GmpayErrorCode.
func (c *GmpayClient) CreateTransaction(ctx context.Context, request GmpayCreateRequest) (*GmpayCreateResponse, error) {
	values := map[string]any{
		"pid":              c.pid,
		"order_id":         strings.TrimSpace(request.OrderID),
		"currency":         strings.ToLower(strings.TrimSpace(request.Fiat)),
		"token":            strings.ToLower(strings.TrimSpace(request.Token)),
		"network":          strings.ToLower(strings.TrimSpace(request.Network)),
		"amount":           request.Amount,
		"notify_url":       strings.TrimSpace(request.NotifyURL),
		"callback_version": GmpayCallbackVersion2,
	}
	if redirect := strings.TrimSpace(request.RedirectURL); redirect != "" {
		values["redirect_url"] = redirect
	}
	if name := strings.TrimSpace(request.Name); name != "" {
		values["name"] = name
	}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Data       struct {
			TradeID        string  `json:"trade_id"`
			OrderID        string  `json:"order_id"`
			Amount         float64 `json:"amount"`
			Currency       string  `json:"currency"`
			ActualAmount   float64 `json:"actual_amount"`
			ReceiveAddress string  `json:"receive_address"`
			Token          string  `json:"token"`
			Status         int     `json:"status"`
			ExpirationTime int64   `json:"expiration_time"`
			PaymentURL     string  `json:"payment_url"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, "/payments/gmpay/v1/order/create-transaction", values, &response); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(response.Data.TradeID) == "" {
		return nil, fmt.Errorf("GMPay create failed: %s", c.safeMessage(response.StatusCode, response.Message))
	}
	if amount := response.Data.Amount; math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return nil, fmt.Errorf("GMPay create returned invalid amount")
	}
	return &GmpayCreateResponse{
		TradeID:        response.Data.TradeID,
		OrderID:        response.Data.OrderID,
		Amount:         response.Data.Amount,
		Currency:       response.Data.Currency,
		ActualAmount:   response.Data.ActualAmount,
		ReceiveAddress: response.Data.ReceiveAddress,
		Token:          response.Data.Token,
		Status:         response.Data.Status,
		ExpirationTime: response.Data.ExpirationTime,
		PaymentURL:     response.Data.PaymentURL,
	}, nil
}

// QueryCheckout calls GET /pay/checkout-counter-resp/{trade_id}, the public
// cashier projection with the full order state. It carries no merchant
// signature: the trade_id is a server-side secret between sub2api and the
// configured upstream.
func (c *GmpayClient) QueryCheckout(ctx context.Context, tradeID string) (*GmpayCheckoutResponse, error) {
	tradeID = strings.TrimSpace(tradeID)
	if tradeID == "" {
		return nil, fmt.Errorf("GMPay query requires trade_id")
	}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Data       struct {
			TradeID        string  `json:"trade_id"`
			Amount         float64 `json:"amount"`
			ActualAmount   float64 `json:"actual_amount"`
			Token          string  `json:"token"`
			Currency       string  `json:"currency"`
			ReceiveAddress string  `json:"receive_address"`
			Network        string  `json:"network"`
			Status         int     `json:"status"`
			ExpirationTime int64   `json:"expiration_time"`
			PaymentType    string  `json:"payment_type"`
			IsSelected     bool    `json:"is_selected"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, "/pay/checkout-counter-resp/"+url.PathEscape(tradeID), &response); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(response.Data.TradeID) == "" {
		return nil, fmt.Errorf("GMPay query failed: %s", c.safeMessage(response.StatusCode, response.Message))
	}
	return &GmpayCheckoutResponse{
		TradeID:        response.Data.TradeID,
		Amount:         response.Data.Amount,
		ActualAmount:   response.Data.ActualAmount,
		Token:          response.Data.Token,
		Currency:       response.Data.Currency,
		ReceiveAddress: response.Data.ReceiveAddress,
		Network:        response.Data.Network,
		Status:         response.Data.Status,
		ExpirationTime: response.Data.ExpirationTime,
		PaymentType:    response.Data.PaymentType,
		IsSelected:     response.Data.IsSelected,
	}, nil
}

func (c *GmpayClient) postJSON(ctx context.Context, path string, values map[string]any, target any) error {
	values["signature"] = GmpaySign(values, c.secretKey)
	body, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("GMPay request encode failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("GMPay request create failed")
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, target)
}

func (c *GmpayClient) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("GMPay request create failed")
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, target)
}

func (c *GmpayClient) do(req *http.Request, target any) error {
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: gmpayHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GMPay request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxGmpayResponseSize))
	if err != nil {
		return fmt.Errorf("GMPay response read failed")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// The upstream carries stable business errno (e.g. 10002 order ID
		// conflict) inside the JSON body with an HTTP 4xx status; surface the
		// code instead of swallowing it behind a bare HTTP error.
		var business struct {
			StatusCode int    `json:"status_code"`
			Message    string `json:"message"`
		}
		if json.Unmarshal(responseBody, &business) == nil && business.StatusCode != 0 {
			return fmt.Errorf("GMPay failed: %s", c.safeMessage(business.StatusCode, business.Message))
		}
		return fmt.Errorf("GMPay HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("GMPay response parse failed")
	}
	return nil
}

// GmpaySign builds the lowercase hex HMAC-SHA256 signature over the canonical
// parameter string, mirroring the upstream rule exactly: drop "signature",
// nil and empty-string values, format numbers with -1 precision, sort keys in
// ASCII order, join as k=v&..., HMAC key = secret_key.
func GmpaySign(values map[string]any, secretKey string) string {
	canonical := gmpayCanonicalParams(values)
	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func gmpayCanonicalParams(values map[string]any) string {
	items := make([]string, 0, len(values))
	for key, value := range values {
		if key == "signature" || value == nil {
			continue
		}
		var field string
		switch typed := value.(type) {
		case string:
			field = typed
		case float64:
			field = strconv.FormatFloat(typed, 'f', -1, 64)
		case int:
			field = strconv.Itoa(typed)
		case int64:
			field = strconv.FormatInt(typed, 10)
		case json.Number:
			number, err := typed.Float64()
			if err != nil {
				continue
			}
			field = strconv.FormatFloat(number, 'f', -1, 64)
		default:
			continue
		}
		if field == "" {
			continue
		}
		items = append(items, key+"="+field)
	}
	sort.Strings(items)
	return strings.Join(items, "&")
}

type GmpayNotification struct {
	PID                string
	TradeID            string
	OrderID            string
	Amount             float64
	ActualAmount       float64
	ReceiveAddress     string
	Token              string
	Network            string
	BlockTransactionID string
	Status             int
}

// ParseGmpayNotification verifies and parses a GMPay JSON callback. The
// signature comparison is constant-time, matching the upstream verifier.
// GmpayAmountEquals compares fiat amounts exactly after canonical -1
// precision formatting: upstream rounds to its configured precision, so both
// sides normalize to the same shortest decimal representation for a genuine
// match, and any real difference stays visible.
func GmpayAmountEquals(a, b float64) bool {
	return strconv.FormatFloat(a, 'f', -1, 64) == strconv.FormatFloat(b, 'f', -1, 64)
}

func ParseGmpayNotification(raw, secretKey string) (*GmpayNotification, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	values := make(map[string]any)
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("GMPay notification parse failed")
	}
	signature, _ := values["signature"].(string)
	signature = strings.TrimSpace(signature)
	if signature == "" || subtle.ConstantTimeCompare([]byte(GmpaySign(values, secretKey)), []byte(signature)) != 1 {
		return nil, fmt.Errorf("invalid GMPay notification signature")
	}
	pid, _ := values["pid"].(string)
	tradeID, _ := values["trade_id"].(string)
	orderID, _ := values["order_id"].(string)
	receiveAddress, _ := values["receive_address"].(string)
	token, _ := values["token"].(string)
	network, _ := values["network"].(string)
	blockTransactionID, _ := values["block_transaction_id"].(string)
	amount, err := gmpayNumber(values["amount"])
	if err != nil {
		return nil, fmt.Errorf("GMPay notification amount is invalid")
	}
	actualAmount, err := gmpayNumber(values["actual_amount"])
	if err != nil {
		return nil, fmt.Errorf("GMPay notification actual_amount is invalid")
	}
	status, err := gmpayInt(values["status"])
	if err != nil {
		return nil, fmt.Errorf("GMPay notification status is invalid")
	}
	if strings.TrimSpace(pid) == "" || strings.TrimSpace(tradeID) == "" || strings.TrimSpace(orderID) == "" || amount <= 0 || actualAmount <= 0 {
		return nil, fmt.Errorf("GMPay notification is incomplete")
	}
	// Orders are always created with token=usdt; a callback naming any other
	// token does not belong to this integration.
	if token = strings.TrimSpace(token); token == "" || !strings.EqualFold(token, GmpayToken) {
		return nil, fmt.Errorf("GMPay notification token must be %s", GmpayToken)
	}
	return &GmpayNotification{
		PID:                strings.TrimSpace(pid),
		TradeID:            strings.TrimSpace(tradeID),
		OrderID:            strings.TrimSpace(orderID),
		Amount:             amount,
		ActualAmount:       actualAmount,
		ReceiveAddress:     strings.TrimSpace(receiveAddress),
		Token:              strings.TrimSpace(token),
		Network:            strings.TrimSpace(network),
		BlockTransactionID: strings.TrimSpace(blockTransactionID),
		Status:             status,
	}, nil
}

func gmpayNumber(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Float64()
	case float64:
		return typed, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("invalid number")
	}
}

func gmpayInt(value any) (int, error) {
	number, err := gmpayNumber(value)
	if err != nil || math.Trunc(number) != number {
		return 0, fmt.Errorf("invalid integer")
	}
	return int(number), nil
}

func (c *GmpayClient) safeMessage(statusCode int, message string) string {
	if c != nil && c.secretKey != "" {
		message = strings.ReplaceAll(message, c.secretKey, "[redacted]")
	}
	if c != nil && c.pid != "" {
		message = strings.ReplaceAll(message, c.pid, "[redacted]")
	}
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 256 {
		return message[:256] + "..."
	}
	if message == "" {
		message = "<empty>"
	}
	if statusCode > 0 {
		return fmt.Sprintf("code=%d: %s", statusCode, message)
	}
	return message
}
