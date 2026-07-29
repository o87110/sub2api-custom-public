package groupaccess

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const MinimumBalanceNotMetReason = "GROUP_MINIMUM_BALANCE_NOT_MET"

// BalanceRequirement is the authoritative evaluation result for a group's
// minimum-balance gate. BalanceGap is the distance to equality; eligibility
// still requires the current balance to be strictly greater than the minimum.
type BalanceRequirement struct {
	MinimumBalance float64
	CurrentBalance float64
	UsableBalance  float64
	BalanceGap     float64
	Eligible       bool
}

// EvaluateMinimumBalance applies the strict group balance rule without
// considering users.frozen_balance. A non-positive minimum disables the gate.
func EvaluateMinimumBalance(currentBalance, minimumBalance float64) BalanceRequirement {
	requirement := BalanceRequirement{
		MinimumBalance: minimumBalance,
		CurrentBalance: currentBalance,
		Eligible:       minimumBalance <= 0 || currentBalance > minimumBalance,
	}
	requirement.UsableBalance = math.Max(currentBalance-minimumBalance, 0)
	requirement.BalanceGap = math.Max(minimumBalance-currentBalance, 0)
	return requirement
}

// CheckMinimumBalance returns the stable public API error for an unmet group
// balance requirement.
func CheckMinimumBalance(groupID int64, groupName string, currentBalance, minimumBalance float64) error {
	requirement := EvaluateMinimumBalance(currentBalance, minimumBalance)
	if requirement.Eligible {
		return nil
	}

	message := minimumBalanceErrorMessage(requirement)
	return infraerrors.Forbidden(MinimumBalanceNotMetReason, message).
		WithMetadata(map[string]string{
			"group_id":         strconv.FormatInt(groupID, 10),
			"group_name":       groupName,
			"minimum_balance":  formatBalanceNumber(requirement.MinimumBalance),
			"current_balance":  formatBalanceNumber(requirement.CurrentBalance),
			"usable_balance":   formatBalanceNumber(requirement.UsableBalance),
			"balance_gap":      formatBalanceNumber(requirement.BalanceGap),
			"balance_eligible": "false",
		})
}

func minimumBalanceErrorMessage(requirement BalanceRequirement) string {
	minimum := FormatUSD(requirement.MinimumBalance)
	current := FormatUSD(requirement.CurrentBalance)
	if requirement.CurrentBalance == requirement.MinimumBalance {
		return fmt.Sprintf(
			"Group requires an available balance greater than %s; current balance is %s. Increase the balance above %s to continue.",
			minimum,
			current,
			minimum,
		)
	}
	return fmt.Sprintf(
		"Group requires an available balance greater than %s; current balance is %s. Add more than %s to continue.",
		minimum,
		current,
		FormatUSD(requirement.BalanceGap),
	)
}

// FormatUSD keeps the usual two decimal places while preserving meaningful
// sub-cent values up to the database's eight-decimal precision.
func FormatUSD(value float64) string {
	return "$" + formatBalanceNumber(value)
}

func formatBalanceNumber(value float64) string {
	if value == 0 {
		return "0.00"
	}
	if math.Abs(value) >= 0.01 {
		return strconv.FormatFloat(value, 'f', 2, 64)
	}
	raw := strconv.FormatFloat(value, 'f', 8, 64)
	raw = strings.TrimRight(raw, "0")
	if strings.HasSuffix(raw, ".") {
		raw += "00"
		return raw
	}
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		decimals := len(raw) - dot - 1
		if decimals == 1 {
			raw += "0"
		}
		return raw
	}
	return raw + ".00"
}
