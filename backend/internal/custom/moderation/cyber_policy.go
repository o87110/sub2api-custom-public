package moderation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	// ActionCyberPolicy records an in-scope upstream cyber policy event.
	ActionCyberPolicy = "cyber_policy"
	cyberPolicyMode   = "post_upstream"
	cyberPolicySource = "openai"
)

// CyberPolicyEvent contains the upstream rejection details needed by local risk control.
type CyberPolicyEvent struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	UpstreamMessage string
	UpstreamBody    string
	UpstreamStatus  int
	UpstreamInTok   int
	UpstreamOutTok  int
}

// Policy is the custom enforcement policy resolved for one cyber policy event.
type Policy struct {
	InGroupScope        bool
	ExcludeFromBanCount bool
}

// Record is the custom cyber policy audit record passed through the service adapter.
type Record struct {
	ID              int64
	RequestID       string
	UserID          *int64
	UserEmail       string
	APIKeyID        *int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Provider        string
	Model           string
	Mode            string
	Action          string
	Flagged         bool
	HighestCategory string
	HighestScore    float64
	Error           string
	ViolationCount  int
	AutoBanned      bool
	EmailSent       bool
	CreatedAt       time.Time
}

// PenaltyResult reports the state changes produced by the existing account enforcement service.
type PenaltyResult struct {
	ViolationCount int
	AutoBanned     bool
	JustBanned     bool
}

// CyberPolicyAdapter exposes only the official service capabilities required by custom orchestration.
type CyberPolicyAdapter interface {
	RiskControlEnabled(ctx context.Context) bool
	LoadPolicy(ctx context.Context, groupID *int64) (Policy, error)
	Redact(text string) string
	ApplyPenalty(ctx context.Context, record *Record) PenaltyResult
	CreateLog(ctx context.Context, record *Record) (int64, error)
	EmailAvailable() bool
	SendCyberPolicyNotice(ctx context.Context, record *Record) error
	SendAccountDisabledNotice(ctx context.Context, record *Record) error
	UpdateLogEmailSent(ctx context.Context, id int64) error
}

// CyberPolicyScopeAdapter resolves the cached runtime scope used by gateway enforcement.
type CyberPolicyScopeAdapter interface {
	LoadScope(ctx context.Context, groupID *int64) (riskControlEnabled bool, inGroupScope bool, err error)
}

// CyberPolicyGroupInScope fails open when the cached runtime policy is unavailable.
func CyberPolicyGroupInScope(ctx context.Context, groupID *int64, adapter CyberPolicyScopeAdapter) bool {
	if adapter == nil {
		return false
	}
	enabled, inScope, err := adapter.LoadScope(ctx, groupID)
	if err != nil {
		slog.Warn("content_moderation.cyber_scope_load_failed", "group_id", int64Value(groupID), "error", err)
		return false
	}
	return enabled && inScope
}

// RecordCyberPolicyEvent performs the custom audit, enforcement, and notification flow.
func RecordCyberPolicyEvent(ctx context.Context, event CyberPolicyEvent, adapter CyberPolicyAdapter) {
	if adapter == nil || !adapter.RiskControlEnabled(ctx) {
		return
	}

	policy, err := adapter.LoadPolicy(ctx, event.GroupID)
	if err != nil {
		slog.Warn("content_moderation.cyber_load_config_failed", "error", err)
		// Configuration failures still preserve an audit record but fail open for enforcement.
		policy = Policy{}
	}

	record := buildCyberPolicyRecord(event, policy, adapter.Redact)
	penalty := PenaltyResult{}
	if policy.InGroupScope && !policy.ExcludeFromBanCount {
		penalty = adapter.ApplyPenalty(ctx, record)
		record.ViolationCount = penalty.ViolationCount
		record.AutoBanned = penalty.AutoBanned
	}

	logID, persistErr := adapter.CreateLog(ctx, record)
	if persistErr != nil {
		slog.Warn("content_moderation.cyber_create_log_failed", "user_id", event.UserID, "error", persistErr)
	} else {
		record.ID = logID
	}
	if !policy.InGroupScope || !adapter.EmailAvailable() || strings.TrimSpace(record.UserEmail) == "" {
		return
	}

	emailSent := false
	if err := adapter.SendCyberPolicyNotice(ctx, record); err != nil {
		slog.Warn("content_moderation.cyber_email_failed", "user_id", event.UserID, "error", err)
	} else {
		emailSent = true
	}
	if penalty.JustBanned {
		if err := adapter.SendAccountDisabledNotice(ctx, record); err != nil {
			slog.Warn("content_moderation.cyber_ban_email_failed", "user_id", event.UserID, "error", err)
		} else {
			emailSent = true
		}
	}
	record.EmailSent = emailSent
	if persistErr == nil && emailSent {
		if err := adapter.UpdateLogEmailSent(ctx, record.ID); err != nil {
			slog.Warn("content_moderation.cyber_update_email_sent_failed", "log_id", record.ID, "error", err)
		}
	}
}

func buildCyberPolicyRecord(event CyberPolicyEvent, policy Policy, redact func(string) string) *Record {
	errBody := strings.TrimSpace(event.UpstreamMessage)
	if body := strings.TrimSpace(event.UpstreamBody); body != "" {
		errBody = strings.TrimSpace(errBody + "\n" + body)
	}
	if event.UpstreamInTok > 0 || event.UpstreamOutTok > 0 {
		errBody = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errBody, event.UpstreamInTok, event.UpstreamOutTok)
	}
	if redact != nil {
		errBody = redact(errBody)
	}

	action := ActionCyberPolicy
	if !policy.InGroupScope {
		action = ActionCyberPolicyOutOfScope
	}
	return &Record{
		RequestID:       event.RequestID,
		UserID:          positiveInt64Ptr(event.UserID),
		UserEmail:       event.UserEmail,
		APIKeyID:        positiveInt64Ptr(event.APIKeyID),
		APIKeyName:      event.APIKeyName,
		GroupID:         cloneInt64Ptr(event.GroupID),
		GroupName:       event.GroupName,
		Endpoint:        event.Endpoint,
		Provider:        cyberPolicySource,
		Model:           event.Model,
		Mode:            cyberPolicyMode,
		Action:          action,
		Flagged:         true,
		HighestCategory: ActionCyberPolicy,
		HighestScore:    1.0,
		Error:           trimRunes(errBody, MaxErrorExcerptRunes),
		CreatedAt:       time.Now(),
	}
}

func positiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
