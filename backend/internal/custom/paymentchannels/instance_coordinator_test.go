package paymentchannels

import (
	"context"
	"errors"
	"testing"
)

type instanceLoaderStub struct {
	records  []InstanceRecord
	byID     map[int64]*InstanceRecord
	usage    map[string]float64
	usageErr error
}

func (s instanceLoaderStub) LoadEnabledInstances(context.Context, string) ([]InstanceRecord, error) {
	return s.records, nil
}

func (s instanceLoaderStub) LoadInstance(_ context.Context, id int64) (*InstanceRecord, error) {
	return s.byID[id], nil
}

func (s instanceLoaderStub) LoadDailyUsage(context.Context, []string) (map[string]float64, error) {
	return s.usage, s.usageErr
}

func TestInstanceCoordinatorSelectsMatchingLeastUsedInstance(t *testing.T) {
	loader := instanceLoaderStub{
		records: []InstanceRecord{
			{ID: 1, ProviderKey: ProviderEasyPay, SupportedTypes: MethodAlipay, Enabled: true},
			{ID: 2, ProviderKey: ProviderEasyPay, SupportedTypes: MethodAlipay, Enabled: true},
			{ID: 3, ProviderKey: ProviderWxpay, SupportedTypes: MethodWxpay, Enabled: true},
		},
		usage: map[string]float64{"1": 20, "2": 5, "3": 0},
	}
	result, err := NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		ProviderKey: ProviderEasyPay,
		PaymentType: MethodAlipay,
		Strategy:    StrategyLeastAmount,
		OrderAmount: 10,
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selection == nil || result.Selection.InstanceID != "2" {
		t.Fatalf("selection = %+v", result.Selection)
	}
}

func TestInstanceCoordinatorExplicitSelectionFailsClosedOnLimits(t *testing.T) {
	loader := instanceLoaderStub{
		records: []InstanceRecord{{
			ID:             1,
			ProviderKey:    ProviderAlipay,
			SupportedTypes: MethodAlipay,
			LimitsJSON:     `{"alipay":{"singleMax":5}}`,
			Enabled:        true,
		}},
		usage: map[string]float64{"1": 0},
	}
	result, err := NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		ProviderKey: ProviderAlipay,
		PaymentType: MethodAlipay,
		OrderAmount: 10,
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selection != nil || len(result.LimitRejections) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestInstanceCoordinatorLegacySelectionFallsBackOnLimitsAndUsageFailure(t *testing.T) {
	loader := instanceLoaderStub{
		records: []InstanceRecord{{
			ID:             1,
			ProviderKey:    ProviderAlipay,
			SupportedTypes: MethodAlipay,
			LimitsJSON:     `{"alipay":{"singleMax":5}}`,
			Enabled:        true,
		}},
		usageErr: errors.New("usage unavailable"),
	}
	result, err := NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		PaymentType: MethodAlipay,
		OrderAmount: 10,
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if result.Selection == nil || result.UsageLoadError == nil {
		t.Fatalf("result = %+v", result)
	}
}

func TestInstanceCoordinatorRevalidatesRevisionAndLimits(t *testing.T) {
	record := InstanceRecord{
		ID:             7,
		ProviderKey:    ProviderAlipay,
		ConfigRaw:      `{"appId":"x"}`,
		Config:         map[string]string{"appId": "x"},
		SupportedTypes: MethodAlipay,
		LimitsJSON:     `{"alipay":{"dailyLimit":100}}`,
		Enabled:        true,
	}
	selection := orderSelection(record)
	loader := instanceLoaderStub{
		byID:  map[int64]*InstanceRecord{7: &record},
		usage: map[string]float64{"7": 95},
	}
	valid, err := NewInstanceCoordinator().Revalidate(context.Background(), loader, selection, MethodAlipay, 10)
	if err != nil {
		t.Fatalf("Revalidate() error = %v", err)
	}
	if valid {
		t.Fatal("expected limit revalidation to fail")
	}
	record.Enabled = false
	valid, err = NewInstanceCoordinator().Revalidate(context.Background(), loader, selection, MethodAlipay, 0)
	if err != nil || valid {
		t.Fatalf("disabled revalidation = %v, %v", valid, err)
	}
}
