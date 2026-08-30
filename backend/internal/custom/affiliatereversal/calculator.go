package affiliatereversal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

const moneyEpsilon = 0.00000001

type targetSnapshot struct {
	LedgerID        int64
	OrderID         int64
	InviterID       int64
	InviteeID       int64
	Amount          float64
	FrozenUntil     *time.Time
	CreatedAt       time.Time
	LedgerUpdatedAt time.Time
}

type accountSnapshot struct {
	InviterID          int64
	Email              string
	Username           string
	AffQuota           float64
	AffFrozenQuota     float64
	AffHistoryQuota    float64
	Balance            float64
	TotalRecharged     float64
	AffiliateUpdatedAt time.Time
	UserUpdatedAt      time.Time
}

type orderPlan struct {
	Target               targetSnapshot
	FrozenDeducted       float64
	AvailableDeducted    float64
	BalanceDeducted      float64
	BalanceBefore        float64
	BalanceAfter         float64
	AffQuotaBefore       float64
	AffQuotaAfter        float64
	AffFrozenBefore      float64
	AffFrozenAfter       float64
	AffHistoryBefore     float64
	AffHistoryAfter      float64
	TotalRechargedBefore float64
	TotalRechargedAfter  float64
}

type calculatedPlan struct {
	Preview       Preview
	Orders        []orderPlan
	FinalAccounts map[int64]accountSnapshot
}

func calculatePlan(targets []targetSnapshot, accounts map[int64]accountSnapshot) (*calculatedPlan, error) {
	if len(targets) == 0 {
		return nil, ErrInvalidOrders
	}
	sortedTargets := append([]targetSnapshot(nil), targets...)
	sort.Slice(sortedTargets, func(i, j int) bool {
		if sortedTargets[i].InviterID != sortedTargets[j].InviterID {
			return sortedTargets[i].InviterID < sortedTargets[j].InviterID
		}
		if !sortedTargets[i].CreatedAt.Equal(sortedTargets[j].CreatedAt) {
			return sortedTargets[i].CreatedAt.Before(sortedTargets[j].CreatedAt)
		}
		return sortedTargets[i].LedgerID < sortedTargets[j].LedgerID
	})

	state := make(map[int64]accountSnapshot, len(accounts))
	for id, account := range accounts {
		state[id] = account
	}
	plan := &calculatedPlan{
		Preview:       Preview{Orders: make([]PreviewOrder, 0, len(sortedTargets))},
		Orders:        make([]orderPlan, 0, len(sortedTargets)),
		FinalAccounts: state,
	}
	impactByInviter := make(map[int64]*InviterImpact, len(accounts))

	for _, target := range sortedTargets {
		account, ok := state[target.InviterID]
		if !ok {
			return nil, ErrInvariant
		}
		amount := roundMoney(target.Amount)
		if amount <= 0 || account.AffHistoryQuota+moneyEpsilon < amount {
			return nil, ErrInvariant
		}
		item := orderPlan{
			Target:               target,
			BalanceBefore:        account.Balance,
			AffQuotaBefore:       account.AffQuota,
			AffFrozenBefore:      account.AffFrozenQuota,
			AffHistoryBefore:     account.AffHistoryQuota,
			TotalRechargedBefore: account.TotalRecharged,
		}
		bucket := "available"
		if target.FrozenUntil != nil {
			bucket = "frozen"
			if account.AffFrozenQuota+moneyEpsilon < amount {
				return nil, ErrInvariant
			}
			item.FrozenDeducted = amount
			account.AffFrozenQuota = roundMoney(account.AffFrozenQuota - amount)
		} else {
			item.AvailableDeducted = math.Min(amount, math.Max(account.AffQuota, 0))
			item.AvailableDeducted = roundMoney(item.AvailableDeducted)
			item.BalanceDeducted = roundMoney(amount - item.AvailableDeducted)
			account.AffQuota = roundMoney(account.AffQuota - item.AvailableDeducted)
			if account.TotalRecharged+moneyEpsilon < item.BalanceDeducted {
				return nil, ErrInvariant
			}
			account.Balance = roundMoney(account.Balance - item.BalanceDeducted)
			account.TotalRecharged = roundMoney(account.TotalRecharged - item.BalanceDeducted)
		}
		account.AffHistoryQuota = roundMoney(account.AffHistoryQuota - amount)
		item.BalanceAfter = account.Balance
		item.AffQuotaAfter = account.AffQuota
		item.AffFrozenAfter = account.AffFrozenQuota
		item.AffHistoryAfter = account.AffHistoryQuota
		item.TotalRechargedAfter = account.TotalRecharged
		state[target.InviterID] = account
		plan.Orders = append(plan.Orders, item)
		plan.Preview.Orders = append(plan.Preview.Orders, PreviewOrder{
			OrderID:         target.OrderID,
			LedgerID:        target.LedgerID,
			InviterID:       target.InviterID,
			InviteeID:       target.InviteeID,
			RebateAmount:    amount,
			QuotaBucket:     bucket,
			FrozenDeducted:  item.FrozenDeducted,
			QuotaDeducted:   item.AvailableDeducted,
			BalanceDeducted: item.BalanceDeducted,
		})

		impact := impactByInviter[target.InviterID]
		if impact == nil {
			initial := accounts[target.InviterID]
			impact = &InviterImpact{
				InviterID:            target.InviterID,
				InviterEmail:         initial.Email,
				InviterUsername:      initial.Username,
				BalanceBefore:        initial.Balance,
				HistoryQuotaBefore:   initial.AffHistoryQuota,
				TotalRechargedBefore: initial.TotalRecharged,
			}
			impactByInviter[target.InviterID] = impact
		}
		impact.OrderCount++
		impact.TotalRebateAmount = roundMoney(impact.TotalRebateAmount + amount)
		impact.FrozenQuotaDeducted = roundMoney(impact.FrozenQuotaDeducted + item.FrozenDeducted)
		impact.AvailableQuotaDeducted = roundMoney(impact.AvailableQuotaDeducted + item.AvailableDeducted)
		impact.BalanceDeducted = roundMoney(impact.BalanceDeducted + item.BalanceDeducted)
	}

	inviterIDs := make([]int64, 0, len(impactByInviter))
	for inviterID := range impactByInviter {
		inviterIDs = append(inviterIDs, inviterID)
	}
	sort.Slice(inviterIDs, func(i, j int) bool { return inviterIDs[i] < inviterIDs[j] })
	for _, inviterID := range inviterIDs {
		impact := impactByInviter[inviterID]
		final := state[inviterID]
		impact.BalanceAfter = final.Balance
		impact.HistoryQuotaAfter = final.AffHistoryQuota
		impact.TotalRechargedAfter = final.TotalRecharged
		impact.WillBeNegative = final.Balance < -moneyEpsilon
		plan.Preview.Inviters = append(plan.Preview.Inviters, *impact)
		plan.Preview.TotalRebateAmount = roundMoney(plan.Preview.TotalRebateAmount + impact.TotalRebateAmount)
		plan.Preview.TotalBalanceDeducted = roundMoney(plan.Preview.TotalBalanceDeducted + impact.BalanceDeducted)
		if impact.WillBeNegative {
			plan.Preview.NegativeBalanceUsers++
		}
	}
	plan.Preview.OrderCount = len(sortedTargets)
	plan.Preview.HasNegativeBalance = plan.Preview.NegativeBalanceUsers > 0
	return plan, nil
}

func previewToken(targets []targetSnapshot, accounts map[int64]accountSnapshot) (string, error) {
	targetCopy := append([]targetSnapshot(nil), targets...)
	sort.Slice(targetCopy, func(i, j int) bool { return targetCopy[i].OrderID < targetCopy[j].OrderID })
	accountIDs := make([]int64, 0, len(accounts))
	for id := range accounts {
		accountIDs = append(accountIDs, id)
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
	accountCopy := make([]accountSnapshot, 0, len(accountIDs))
	for _, id := range accountIDs {
		accountCopy = append(accountCopy, accounts[id])
	}
	payload := struct {
		Targets  []targetSnapshot  `json:"targets"`
		Accounts []accountSnapshot `json:"accounts"`
	}{Targets: targetCopy, Accounts: accountCopy}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal affiliate reversal preview: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func roundMoney(value float64) float64 {
	return math.Round(value*100000000) / 100000000
}
