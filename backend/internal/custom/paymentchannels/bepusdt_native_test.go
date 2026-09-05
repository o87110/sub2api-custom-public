package paymentchannels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseBepusdtNetworks(t *testing.T) {
	networks, err := ParseBepusdtNetworks("plasma, bep20, trc20")
	if err != nil {
		t.Fatalf("ParseBepusdtNetworks() error = %v", err)
	}
	if got := networks[0].Code; got != "bep20" {
		t.Fatalf("first network = %q, want bep20", got)
	}
	if got := networks[1].UpstreamType; got != "usdt.trc20" {
		t.Fatalf("second network upstream = %q, want usdt.trc20", got)
	}
	if _, err := ParseBepusdtNetworks("bep20,bep20"); err == nil {
		t.Fatal("duplicate network should fail")
	}
	if _, err := ParseBepusdtNetworks("bsc"); err == nil {
		t.Fatal("unknown network should fail")
	}
}

func TestProviderCatalogBuildsBepusdtNetworkOptions(t *testing.T) {
	options := (ProviderCatalog{}).BuildOptions([]ProviderRecord{{
		ID:             1,
		ProviderKey:    ProviderEasyPay,
		SupportedTypes: BepusdtPaymentType,
		Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt,
			BepusdtNetworksConfigKey: "trc20,bep20,polygon,plasma",
		},
	}}, 0, nil, false)
	if len(options) != 1 || options[0].ID != "easypay_usdt" {
		t.Fatalf("options = %+v", options)
	}
	if len(options[0].NetworkOptions) != 4 || options[0].NetworkOptions[0].Code != "bep20" {
		t.Fatalf("network options = %+v", options[0].NetworkOptions)
	}
}

func TestProviderCatalogUnionsBepusdtNetworkOptionsAcrossInstances(t *testing.T) {
	options := (ProviderCatalog{}).BuildOptions([]ProviderRecord{
		{ID: 1, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt, BepusdtNetworksConfigKey: "bep20",
		}},
		{ID: 2, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt, BepusdtNetworksConfigKey: "trc20",
		}},
	}, 0, nil, false)
	if len(options) != 1 {
		t.Fatalf("options = %+v", options)
	}
	if got := options[0].NetworkOptions; len(got) != 2 || got[0].Code != "bep20" || got[1].Code != "trc20" {
		t.Fatalf("network options = %+v, want BEP20 and TRC20", got)
	}
	if !options[0].Available {
		t.Fatal("channel should remain available when different instances cover different networks")
	}
}

func TestInstanceCoordinatorFiltersBepusdtNetwork(t *testing.T) {
	loader := &stubInstanceLoader{records: []InstanceRecord{
		{ID: 1, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt, BepusdtNetworksConfigKey: "trc20",
		}, Enabled: true},
		{ID: 2, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt, BepusdtNetworksConfigKey: "bep20",
		}, Enabled: true},
	}}
	result, err := NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		PaymentType: BepusdtPaymentType, PaymentNetwork: "bep20", Strategy: StrategyRoundRobin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection == nil || result.Selection.InstanceID != "2" {
		t.Fatalf("selection = %+v", result.Selection)
	}
}

type stubInstanceLoader struct{ records []InstanceRecord }

func (s *stubInstanceLoader) LoadEnabledInstances(context.Context, string) ([]InstanceRecord, error) {
	return s.records, nil
}
func (s *stubInstanceLoader) LoadInstance(context.Context, int64) (*InstanceRecord, error) {
	return nil, nil
}
func (s *stubInstanceLoader) LoadDailyUsage(context.Context, []string) (map[string]float64, error) {
	return map[string]float64{}, nil
}

func TestBepusdtClientCreateAndInfo(t *testing.T) {
	const token = "secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var values map[string]any
		if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if r.URL.Path == "/api/v1/order/create-transaction" {
			if values["trade_type"] != "usdt.bep20" {
				t.Errorf("trade_type = %v", values["trade_type"])
			}
			if values["signature"] != BepusdtSign(values, token) {
				t.Errorf("signature mismatch")
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"trade-1","order_id":"order-1","amount":"12.34","status":1,"payment_url":"https://pay.example/1"}}`))
			return
		}
		if r.URL.Path == "/api/v1/pay/info" {
			if _, ok := values["signature"]; ok {
				t.Errorf("info request should not contain signature")
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"trade-1","money":"12.34","status":2}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, err := NewBepusdtClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateTransaction(context.Background(), BepusdtCreateRequest{OrderID: "order-1", NotifyURL: "https://sub.example/notify", RedirectURL: "https://sub.example/result", Amount: 12.34, Fiat: "CNY", TradeType: "usdt.bep20", Name: "test", Timeout: 1800})
	if err != nil {
		t.Fatal(err)
	}
	if created.TradeID != "trade-1" || created.PayURL == "" {
		t.Fatalf("created = %+v", created)
	}
	info, err := client.Info(context.Background(), created.TradeID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != 2 || info.Money != 12.34 {
		t.Fatalf("info = %+v", info)
	}
}

func TestParseBepusdtNotification(t *testing.T) {
	token := "token"
	values := map[string]any{
		"trade_id": "trade-1", "order_id": "order-1", "amount": 12.34,
		"actual_amount": "4.25", "status": 2,
	}
	values["signature"] = BepusdtSign(values, token)
	raw, _ := json.Marshal(values)
	notification, err := ParseBepusdtNotification(string(raw), token)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Status != 2 || notification.OrderID != "order-1" {
		t.Fatalf("notification = %+v", notification)
	}
	if _, err := ParseBepusdtNotification(strings.Replace(string(raw), "order-1", "tampered", 1), token); err == nil {
		t.Fatal("tampered notification should fail")
	}
}
