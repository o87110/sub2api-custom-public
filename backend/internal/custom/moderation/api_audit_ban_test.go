//go:build unit

package moderation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsAPIAuditViolation(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		flagged bool
		want    bool
	}{
		{name: "observe hit", action: "allow", flagged: true, want: true},
		{name: "pre block hit", action: "block", flagged: true, want: true},
		{name: "non hit", action: "allow", flagged: false, want: false},
		{name: "keyword", action: "keyword_block", flagged: true, want: false},
		{name: "hash", action: "hash_block", flagged: true, want: false},
		{name: "cyber policy", action: "cyber_policy", flagged: true, want: true},
		{name: "out of scope", action: "cyber_policy_out_of_scope", flagged: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsAPIAuditViolation(tt.action, tt.flagged))
		})
	}
}

func TestEvaluateBanRules(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		totalMax  int
		api       int
		apiMax    int
		apiActive bool
		want      BanEvaluation
	}{
		{
			name:      "total rule keeps precedence",
			total:     3,
			totalMax:  3,
			api:       2,
			apiMax:    2,
			apiActive: true,
			want:      BanEvaluation{Count: 3, Threshold: 3, Reached: true, Source: BanCountSourceTotal},
		},
		{
			name:      "api rule can ban earlier",
			total:     2,
			totalMax:  5,
			api:       2,
			apiMax:    2,
			apiActive: true,
			want:      BanEvaluation{Count: 2, Threshold: 2, Reached: true, Source: BanCountSourceAPIAudit},
		},
		{
			name:      "inactive API rule preserves total progress",
			total:     2,
			totalMax:  5,
			api:       2,
			apiMax:    2,
			apiActive: false,
			want:      BanEvaluation{Count: 2, Threshold: 5, Reached: false, Source: BanCountSourceTotal},
		},
		{
			name:      "unreached API rule preserves total progress",
			total:     2,
			totalMax:  5,
			api:       1,
			apiMax:    2,
			apiActive: true,
			want:      BanEvaluation{Count: 2, Threshold: 5, Reached: false, Source: BanCountSourceTotal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, EvaluateBanRules(tt.total, tt.totalMax, tt.api, tt.apiMax, tt.apiActive))
		})
	}
}

func TestValidateAPIAuditBanThreshold(t *testing.T) {
	require.NoError(t, ValidateAPIAuditBanThreshold(1))
	require.NoError(t, ValidateAPIAuditBanThreshold(1000))
	require.Error(t, ValidateAPIAuditBanThreshold(0))
	require.Error(t, ValidateAPIAuditBanThreshold(1001))
}
