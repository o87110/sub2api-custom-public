package moderation

import "fmt"

const (
	MinAPIAuditBanThreshold = 1
	MaxAPIAuditBanThreshold = 1000

	BanCountSourceTotal    = "total"
	BanCountSourceAPIAudit = "api_audit"
)

// BanEvaluation describes the count and threshold that should be presented for
// the current hit. The existing total rule takes precedence when both rules
// reach their thresholds on the same request.
type BanEvaluation struct {
	Count     int
	Threshold int
	Reached   bool
	Source    string
}

func ValidateAPIAuditBanThreshold(threshold int) error {
	if threshold < MinAPIAuditBanThreshold || threshold > MaxAPIAuditBanThreshold {
		return fmt.Errorf("API audit ban threshold must be between %d and %d", MinAPIAuditBanThreshold, MaxAPIAuditBanThreshold)
	}
	return nil
}

// IsAPIAuditViolation identifies a flagged result that belongs to the API
// moderation enforcement stream. Upstream cyber-policy blocks are included:
// they are an API request rejection and must honor the same dedicated ban
// threshold when that rule is enabled.
func IsAPIAuditViolation(action string, flagged bool) bool {
	if !flagged {
		return false
	}
	return action == "allow" || action == "block" || action == "cyber_policy"
}

// EvaluateBanRules keeps the existing total rule authoritative while allowing
// the API-only rule to ban earlier. When neither rule is reached, the existing
// total count remains the value shown in logs and notifications.
func EvaluateBanRules(
	totalCount int,
	totalThreshold int,
	apiAuditCount int,
	apiAuditThreshold int,
	apiAuditRuleActive bool,
) BanEvaluation {
	total := BanEvaluation{
		Count:     totalCount,
		Threshold: totalThreshold,
		Reached:   totalThreshold > 0 && totalCount >= totalThreshold,
		Source:    BanCountSourceTotal,
	}
	if total.Reached || !apiAuditRuleActive {
		return total
	}
	if apiAuditThreshold > 0 && apiAuditCount >= apiAuditThreshold {
		return BanEvaluation{
			Count:     apiAuditCount,
			Threshold: apiAuditThreshold,
			Reached:   true,
			Source:    BanCountSourceAPIAudit,
		}
	}
	return total
}
