package paymentchannels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

var ErrNoEnabledInstance = errors.New("no enabled payment provider instance")

const (
	StrategyRoundRobin  = "round-robin"
	StrategyLeastAmount = "least-amount"
)

type InstanceRecord struct {
	ID             int64
	ProviderKey    string
	ConfigRaw      string
	Config         map[string]string
	SupportedTypes string
	PaymentMode    string
	LimitsJSON     string
	Enabled        bool
}

type InstanceLoader interface {
	LoadEnabledInstances(ctx context.Context, providerKey string) ([]InstanceRecord, error)
	LoadInstance(ctx context.Context, instanceID int64) (*InstanceRecord, error)
	LoadDailyUsage(ctx context.Context, instanceIDs []string) (map[string]float64, error)
}

type InstanceSelectionRequest struct {
	ProviderKey      string
	PaymentType      string
	Strategy         string
	OrderAmount      float64
	WeChatJSAPIAppID string
}

type InstanceSelectionResult struct {
	Selection       *OrderSelection
	UsageLoadError  error
	LimitRejections []LimitRejection
}

type LimitRejection struct {
	InstanceID int64
	Reason     string
	DailyUsed  float64
	Limit      float64
}

type instanceCandidate struct {
	record    InstanceRecord
	dailyUsed float64
}

type InstanceCoordinator struct {
	counter atomic.Uint64
}

func NewInstanceCoordinator() *InstanceCoordinator {
	return &InstanceCoordinator{}
}

func (c *InstanceCoordinator) Select(ctx context.Context, loader InstanceLoader, request InstanceSelectionRequest) (*InstanceSelectionResult, error) {
	records, err := loader.LoadEnabledInstances(ctx, normalize(request.ProviderKey))
	if err != nil {
		return nil, err
	}
	records = matchingInstanceRecords(records, request.PaymentType, request.WeChatJSAPIAppID)
	if len(records) == 0 {
		return nil, fmt.Errorf("%w for payment type %s", ErrNoEnabledInstance, request.PaymentType)
	}

	ids := make([]string, len(records))
	for index, record := range records {
		ids[index] = strconv.FormatInt(record.ID, 10)
	}
	usage, usageErr := loader.LoadDailyUsage(ctx, ids)
	if usageErr != nil {
		usage = map[string]float64{}
	}
	candidates := make([]instanceCandidate, len(records))
	for index, record := range records {
		candidates[index] = instanceCandidate{record: record, dailyUsed: usage[strconv.FormatInt(record.ID, 10)]}
	}
	available, rejections := availableInstances(candidates, request.PaymentType, request.OrderAmount)
	if len(available) == 0 {
		if ShouldFailClosedOnNoAvailableInstance(request.ProviderKey) {
			return &InstanceSelectionResult{UsageLoadError: usageErr, LimitRejections: rejections}, nil
		}
		available = candidates
	}
	selected := c.pick(available, request.Strategy)
	return &InstanceSelectionResult{
		Selection:       orderSelection(selected.record),
		UsageLoadError:  usageErr,
		LimitRejections: rejections,
	}, nil
}

func (c *InstanceCoordinator) Revalidate(ctx context.Context, loader InstanceLoader, selection *OrderSelection, paymentType string, orderAmount float64) (bool, error) {
	if selection == nil {
		return false, nil
	}
	instanceID, err := strconv.ParseInt(strings.TrimSpace(selection.InstanceID), 10, 64)
	if err != nil || instanceID <= 0 {
		return false, nil
	}
	record, err := loader.LoadInstance(ctx, instanceID)
	if err != nil {
		return false, err
	}
	if record == nil || !SelectionMatches(revisionRecord(*record), SelectionSnapshot{
		ProviderKey: selection.ProviderKey,
		Revision:    selection.InstanceRevision,
	}, instanceSupportsType(record.SupportedTypes, paymentType)) {
		return false, nil
	}
	if orderAmount <= 0 {
		return true, nil
	}
	usage, err := loader.LoadDailyUsage(ctx, []string{selection.InstanceID})
	if err != nil {
		return false, err
	}
	available, _ := availableInstances([]instanceCandidate{{
		record:    *record,
		dailyUsed: usage[selection.InstanceID],
	}}, paymentType, orderAmount)
	return len(available) == 1, nil
}

func (c *InstanceCoordinator) pick(candidates []instanceCandidate, strategy string) instanceCandidate {
	if strategy == StrategyLeastAmount && len(candidates) > 1 {
		best := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.dailyUsed < best.dailyUsed {
				best = candidate
			}
		}
		return best
	}
	index := c.counter.Add(1) % uint64(len(candidates))
	return candidates[index]
}

func matchingInstanceRecords(records []InstanceRecord, paymentType, expectedAppID string) []InstanceRecord {
	matched := make([]InstanceRecord, 0, len(records))
	for _, record := range records {
		if normalizeVisibleMethod(paymentType) == MethodStripe {
			if normalize(record.ProviderKey) == ProviderStripe {
				matched = append(matched, record)
			}
			continue
		}
		if !instanceSupportsType(record.SupportedTypes, paymentType) {
			continue
		}
		if strings.TrimSpace(expectedAppID) != "" && normalizeVisibleMethod(paymentType) == MethodWxpay && normalize(record.ProviderKey) == ProviderWxpay && resolveWeChatJSAPIAppID(record.Config) != strings.TrimSpace(expectedAppID) {
			continue
		}
		matched = append(matched, record)
	}
	return matched
}

func availableInstances(candidates []instanceCandidate, paymentType string, orderAmount float64) ([]instanceCandidate, []LimitRejection) {
	available := make([]instanceCandidate, 0, len(candidates))
	rejections := make([]LimitRejection, 0)
	for _, candidate := range candidates {
		limits := instanceLimits(candidate.record, paymentType)
		rejection := LimitRejection{InstanceID: candidate.record.ID, DailyUsed: candidate.dailyUsed}
		switch {
		case limits.SingleMin > 0 && orderAmount < limits.SingleMin:
			rejection.Reason, rejection.Limit = "single_min", limits.SingleMin
		case limits.SingleMax > 0 && orderAmount > limits.SingleMax:
			rejection.Reason, rejection.Limit = "single_max", limits.SingleMax
		case limits.DailyLimit > 0 && candidate.dailyUsed+orderAmount > limits.DailyLimit:
			rejection.Reason, rejection.Limit = "daily_limit", limits.DailyLimit
		default:
			available = append(available, candidate)
			continue
		}
		rejections = append(rejections, rejection)
	}
	return available, rejections
}

func instanceLimits(record InstanceRecord, paymentType string) Limits {
	if strings.TrimSpace(record.LimitsJSON) == "" {
		return Limits{}
	}
	limits := make(map[string]Limits)
	if err := json.Unmarshal([]byte(record.LimitsJSON), &limits); err != nil {
		return Limits{}
	}
	lookup := normalizeVisibleMethod(paymentType)
	if normalize(record.ProviderKey) == ProviderStripe {
		lookup = ProviderStripe
	}
	if value, ok := limits[lookup]; ok {
		return value
	}
	if lookup == MethodAlipay {
		return limits["alipay_direct"]
	}
	if lookup == MethodWxpay {
		return limits["wxpay_direct"]
	}
	return Limits{}
}

func instanceSupportsType(supportedTypes, paymentType string) bool {
	if strings.TrimSpace(supportedTypes) == "" {
		return true
	}
	target := normalizeVisibleMethod(paymentType)
	for _, supported := range strings.Split(supportedTypes, ",") {
		if normalizeVisibleMethod(supported) == target {
			return true
		}
	}
	return false
}

func orderSelection(record InstanceRecord) *OrderSelection {
	config := make(map[string]string, len(record.Config)+1)
	for key, value := range record.Config {
		config[key] = value
	}
	if record.PaymentMode != "" {
		config["paymentMode"] = record.PaymentMode
	}
	return &OrderSelection{
		InstanceID:       strconv.FormatInt(record.ID, 10),
		ProviderKey:      record.ProviderKey,
		Config:           config,
		SupportedTypes:   record.SupportedTypes,
		PaymentMode:      record.PaymentMode,
		InstanceRevision: InstanceRevision(revisionRecord(record)),
	}
}

func revisionRecord(record InstanceRecord) RevisionRecord {
	return RevisionRecord{
		ProviderKey:    record.ProviderKey,
		Config:         record.ConfigRaw,
		SupportedTypes: record.SupportedTypes,
		PaymentMode:    record.PaymentMode,
		Limits:         record.LimitsJSON,
		Enabled:        record.Enabled,
	}
}
