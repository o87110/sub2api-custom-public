package paymentchannels

import (
	"bytes"
	"context"
	"crypto/md5"
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

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	EasyPayProtocolConfigKey = "easypayProtocol"
	EasyPayAPITokenConfigKey = "apiToken"
	BepusdtNetworksConfigKey = "bepusdtNetworks"
	EasyPayProtocolRainbow   = "rainbow_epay"
	EasyPayProtocolBepusdt   = "bepusdt_native"
	BepusdtPaymentType       = "usdt"
	BepusdtFiat              = "CNY"
	maxBepusdtResponseSize   = 1 << 20
	bepusdtHTTPTimeout       = 10 * time.Second
)

type BepusdtNetwork struct {
	Code         string `json:"code"`
	UpstreamType string `json:"upstream_type"`
	DisplayName  string `json:"display_name"`
}

var bepusdtNetworkByCode = map[string]BepusdtNetwork{
	"trc20":   {Code: "trc20", UpstreamType: "usdt.trc20", DisplayName: "TRC20"},
	"bep20":   {Code: "bep20", UpstreamType: "usdt.bep20", DisplayName: "BEP20"},
	"polygon": {Code: "polygon", UpstreamType: "usdt.polygon", DisplayName: "Polygon"},
	"plasma":  {Code: "plasma", UpstreamType: "usdt.plasma", DisplayName: "Plasma"},
}

var bepusdtNetworkOrder = []string{"bep20", "trc20", "polygon", "plasma"}

func NormalizeEasyPayProtocol(raw string) (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(raw))
	if protocol == "" {
		return EasyPayProtocolRainbow, nil
	}
	switch protocol {
	case EasyPayProtocolRainbow, EasyPayProtocolBepusdt:
		return protocol, nil
	default:
		return "", fmt.Errorf("unsupported easypay protocol: %s", protocol)
	}
}

func IsBepusdtNativeConfig(config map[string]string) bool {
	protocol, err := NormalizeEasyPayProtocol(config[EasyPayProtocolConfigKey])
	return err == nil && protocol == EasyPayProtocolBepusdt
}

func BepusdtNetworkByCode(code string) (BepusdtNetwork, bool) {
	network, ok := bepusdtNetworkByCode[strings.ToLower(strings.TrimSpace(code))]
	return network, ok
}

func ParseBepusdtNetworks(raw string) ([]BepusdtNetwork, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]BepusdtNetwork, 0, len(parts))
	for _, part := range parts {
		code := strings.ToLower(strings.TrimSpace(part))
		if code == "" {
			continue
		}
		network, ok := BepusdtNetworkByCode(code)
		if !ok {
			return nil, fmt.Errorf("unsupported BEpusdt network: %s", code)
		}
		if _, exists := seen[code]; exists {
			return nil, fmt.Errorf("duplicate BEpusdt network: %s", code)
		}
		seen[code] = struct{}{}
		result = append(result, network)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one BEpusdt network is required")
	}
	sort.SliceStable(result, func(i, j int) bool {
		return networkOrderIndex(result[i].Code) < networkOrderIndex(result[j].Code)
	})
	return result, nil
}

func networkOrderIndex(code string) int {
	for index, value := range bepusdtNetworkOrder {
		if value == code {
			return index
		}
	}
	return len(bepusdtNetworkOrder)
}

func BepusdtNetworkOptions(config map[string]string) []NetworkOption {
	networks, err := ParseBepusdtNetworks(config[BepusdtNetworksConfigKey])
	if err != nil {
		return nil
	}
	options := make([]NetworkOption, 0, len(networks))
	for _, network := range networks {
		options = append(options, NetworkOption{Code: network.Code, DisplayName: network.DisplayName})
	}
	return options
}

func ValidateBepusdtPaymentNetwork(config map[string]string, paymentType, network string) error {
	if strings.TrimSpace(network) == "" {
		return fmt.Errorf("payment network is required for BEpusdt USDT")
	}
	if strings.ToLower(strings.TrimSpace(paymentType)) != BepusdtPaymentType {
		return fmt.Errorf("payment network is only supported for USDT")
	}
	if !IsBepusdtNativeConfig(config) {
		return fmt.Errorf("payment network requires BEpusdt native protocol")
	}
	configured, err := ParseBepusdtNetworks(config[BepusdtNetworksConfigKey])
	if err != nil {
		return err
	}
	code := strings.ToLower(strings.TrimSpace(network))
	for _, item := range configured {
		if item.Code == code {
			return nil
		}
	}
	return fmt.Errorf("BEpusdt network is not enabled: %s", code)
}

func ValidateBepusdtProviderMode(config map[string]string, supportedTypes, paymentMode string, refundEnabled bool) error {
	if !IsBepusdtNativeConfig(config) {
		return nil
	}
	if strings.TrimSpace(paymentMode) != "popup" {
		return fmt.Errorf("BEpusdt native mode requires popup payment mode")
	}
	if refundEnabled {
		return fmt.Errorf("BEpusdt native mode does not support refunds")
	}
	if _, err := ParseBepusdtNetworks(config[BepusdtNetworksConfigKey]); err != nil {
		return err
	}
	types := strings.Split(supportedTypes, ",")
	if len(types) != 1 || strings.ToLower(strings.TrimSpace(types[0])) != BepusdtPaymentType {
		return fmt.Errorf("BEpusdt native mode supports only usdt")
	}
	return nil
}

func ValidatePaymentNetworkSelection(paymentType, network string, selection *OrderSelection) error {
	network = strings.ToLower(strings.TrimSpace(network))
	paymentType = strings.ToLower(strings.TrimSpace(paymentType))
	if network != "" && paymentType != BepusdtPaymentType {
		return infraerrors.BadRequest("INVALID_PAYMENT_NETWORK", "payment network is only supported for USDT")
	}
	if selection == nil {
		return nil
	}
	if normalize(selection.ProviderKey) != ProviderEasyPay {
		if network != "" {
			return infraerrors.BadRequest("INVALID_PAYMENT_NETWORK", "payment network requires BEpusdt native protocol")
		}
		return nil
	}
	if !IsBepusdtNativeConfig(selection.Config) {
		if network != "" {
			return infraerrors.BadRequest("INVALID_PAYMENT_NETWORK", "payment network requires BEpusdt native protocol")
		}
		return nil
	}
	if err := ValidateBepusdtPaymentNetwork(selection.Config, paymentType, network); err != nil {
		return infraerrors.BadRequest("INVALID_PAYMENT_NETWORK", err.Error())
	}
	return nil
}

type BepusdtCreateRequest struct {
	OrderID     string
	NotifyURL   string
	RedirectURL string
	Amount      float64
	Fiat        string
	TradeType   string
	Name        string
	Timeout     int64
}

type BepusdtCreateResponse struct {
	TradeID string
	OrderID string
	Amount  float64
	Status  int
	PayURL  string
}

type BepusdtInfoResponse struct {
	TradeID      string
	OrderID      string
	Money        float64
	ActualAmount string
	Status       int
}

type BepusdtClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewBepusdtClient(baseURL, token string) (*BepusdtClient, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid BEpusdt api base")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("BEpusdt api token is required")
	}
	return &BepusdtClient{baseURL: base, token: token, httpClient: &http.Client{Timeout: bepusdtHTTPTimeout}}, nil
}

func (c *BepusdtClient) CreateTransaction(ctx context.Context, request BepusdtCreateRequest) (*BepusdtCreateResponse, error) {
	values := map[string]any{
		"order_id": request.OrderID, "notify_url": request.NotifyURL, "redirect_url": request.RedirectURL,
		"amount": request.Amount, "fiat": request.Fiat, "trade_type": request.TradeType,
		"name": request.Name, "timeout": request.Timeout,
	}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Data       struct {
			TradeID string `json:"trade_id"`
			OrderID string `json:"order_id"`
			Amount  string `json:"amount"`
			Status  int    `json:"status"`
			PayURL  string `json:"payment_url"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "/api/v1/order/create-transaction", values, &response, true); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(response.Data.TradeID) == "" || strings.TrimSpace(response.Data.PayURL) == "" {
		return nil, fmt.Errorf("BEpusdt create failed: %s", c.safeMessage(response.Message))
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(response.Data.Amount), 64)
	if err != nil {
		return nil, fmt.Errorf("BEpusdt create returned invalid amount")
	}
	return &BepusdtCreateResponse{TradeID: response.Data.TradeID, OrderID: response.Data.OrderID, Amount: amount, Status: response.Data.Status, PayURL: response.Data.PayURL}, nil
}

func (c *BepusdtClient) Info(ctx context.Context, tradeID string) (*BepusdtInfoResponse, error) {
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Data       struct {
			TradeID      string `json:"trade_id"`
			OrderID      string `json:"order_id"`
			Money        string `json:"money"`
			ActualAmount string `json:"actual_amount"`
			Status       int    `json:"status"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "/api/v1/pay/info", map[string]any{"trade_id": tradeID}, &response, false); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("BEpusdt info failed: %s", c.safeMessage(response.Message))
	}
	money, err := strconv.ParseFloat(strings.TrimSpace(response.Data.Money), 64)
	if err != nil || math.IsNaN(money) || math.IsInf(money, 0) {
		return nil, fmt.Errorf("BEpusdt info returned invalid amount")
	}
	return &BepusdtInfoResponse{TradeID: response.Data.TradeID, OrderID: response.Data.OrderID, Money: money, ActualAmount: response.Data.ActualAmount, Status: response.Data.Status}, nil
}

func (c *BepusdtClient) Cancel(ctx context.Context, tradeID string) error {
	values := map[string]any{"trade_id": tradeID}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
	}
	if err := c.doJSON(ctx, "/api/v1/order/cancel-transaction", values, &response, true); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("BEpusdt cancel failed: %s", c.safeMessage(response.Message))
	}
	return nil
}

func (c *BepusdtClient) doJSON(ctx context.Context, path string, values map[string]any, target any, sign bool) error {
	if sign {
		values["signature"] = BepusdtSign(values, c.token)
	}
	body, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("BEpusdt request encode failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("BEpusdt request create failed")
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: bepusdtHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("BEpusdt request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBepusdtResponseSize))
	if err != nil {
		return fmt.Errorf("BEpusdt response read failed")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("BEpusdt HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("BEpusdt response parse failed")
	}
	return nil
}

func BepusdtSign(values map[string]any, token string) string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "signature" || value == nil || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(fmt.Sprintf("%v", values[key]))
	}
	digest := md5.Sum([]byte(builder.String() + token))
	return hex.EncodeToString(digest[:])
}

type BepusdtNotification struct {
	TradeID string
	OrderID string
	Amount  float64
	Status  int
}

func ParseBepusdtNotification(raw, token string) (*BepusdtNotification, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	values := make(map[string]any)
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("BEpusdt notification parse failed")
	}
	signature, _ := values["signature"].(string)
	if strings.TrimSpace(signature) == "" || !strings.EqualFold(BepusdtSign(values, token), strings.TrimSpace(signature)) {
		return nil, fmt.Errorf("invalid BEpusdt notification signature")
	}
	tradeID, _ := values["trade_id"].(string)
	orderID, _ := values["order_id"].(string)
	amount, err := bepusdtNumber(values["amount"])
	if err != nil {
		return nil, fmt.Errorf("BEpusdt notification amount is invalid")
	}
	status, err := bepusdtInt(values["status"])
	if err != nil {
		return nil, fmt.Errorf("BEpusdt notification status is invalid")
	}
	if strings.TrimSpace(tradeID) == "" || strings.TrimSpace(orderID) == "" || amount <= 0 {
		return nil, fmt.Errorf("BEpusdt notification is incomplete")
	}
	return &BepusdtNotification{TradeID: strings.TrimSpace(tradeID), OrderID: strings.TrimSpace(orderID), Amount: amount, Status: status}, nil
}

func bepusdtNumber(value any) (float64, error) {
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

func bepusdtInt(value any) (int, error) {
	number, err := bepusdtNumber(value)
	if err != nil || math.Trunc(number) != number {
		return 0, fmt.Errorf("invalid integer")
	}
	return int(number), nil
}

func safeBepusdtMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 256 {
		return message[:256] + "..."
	}
	return message
}

func (c *BepusdtClient) safeMessage(message string) string {
	if c != nil && c.token != "" {
		message = strings.ReplaceAll(message, c.token, "[redacted]")
	}
	return safeBepusdtMessage(message)
}
