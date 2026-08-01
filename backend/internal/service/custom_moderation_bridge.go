package service

import (
	"context"
	"time"

	custommoderation "github.com/Wei-Shaw/sub2api/internal/custom/moderation"
)

type contentModerationViolationCounter interface {
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error)
}

// AttachCustomViolationCounter installs the custom read port before the server starts.
func AttachCustomViolationCounter(s *ContentModerationService, counter custommoderation.ViolationCounter) {
	if s == nil {
		return
	}
	s.violationCounter = counter
}

func (s *ContentModerationService) countFlaggedByUserSince(
	ctx context.Context,
	userID int64,
	since time.Time,
	excludeCyberPolicy bool,
) (int, error) {
	if s != nil && s.violationCounter != nil {
		return s.violationCounter.CountFlaggedByUserSince(ctx, userID, since, excludeCyberPolicy)
	}
	return s.repo.CountFlaggedByUserSince(ctx, userID, since, excludeCyberPolicy)
}

const (
	ContentModerationActionCyberPolicyOutOfScope = custommoderation.ActionCyberPolicyOutOfScope
	contentModerationKeywordContextSeparator     = custommoderation.KeywordContextSeparator
	contentModerationKeywordExcerptRunes         = custommoderation.MaxKeywordExcerptRunes
)

type APIAuditScope = custommoderation.APIAuditScope

func defaultContentModerationAPIAuditScope() *APIAuditScope {
	return custommoderation.DefaultAPIAuditScope()
}

func normalizeContentModerationAPIAuditScope(scope *APIAuditScope) *APIAuditScope {
	return custommoderation.NormalizeAPIAuditScope(scope)
}

func (cfg *ContentModerationConfig) includesAPIAuditGroup(groupID *int64) bool {
	return cfg != nil && cfg.includesGroup(groupID) && cfg.APIAuditScope.Includes(groupID)
}

func validateContentModerationAPIAuditScope(cfg *ContentModerationConfig, requireNonEmpty bool) error {
	if cfg == nil {
		return nil
	}
	return custommoderation.ValidateAPIAuditScope(
		cfg.AllGroups,
		cfg.GroupIDs,
		cfg.APIAuditScope,
		requireNonEmpty,
	)
}

func buildContentModerationKeywordExcerptFromRedacted(redacted, keyword string, maxRunes int) string {
	return custommoderation.BuildKeywordExcerptFromRedacted(redacted, keyword, maxRunes)
}

func applyCustomContentModerationKeywordExcerpt(log *ContentModerationLog, text, keyword string) {
	if log == nil {
		return
	}
	log.MatchedKeyword = keyword
	log.InputExcerpt = buildContentModerationKeywordExcerptFromRedacted(
		redactContentModerationSecrets(text),
		keyword,
		contentModerationKeywordExcerptRunes,
	)
}

// CyberPolicyGroupInScope reports whether local cyber_policy enforcement should
// apply to the current group. Configuration failures fail open for local
// enforcement so an unavailable audit setting cannot trigger a stale local ban.
func (s *ContentModerationService) CyberPolicyGroupInScope(ctx context.Context, groupID *int64) bool {
	if s == nil {
		return false
	}
	return custommoderation.CyberPolicyGroupInScope(
		ctx,
		groupID,
		&contentModerationCyberPolicyAdapter{service: s},
	)
}

// tryRecordCustomCyberPolicyEvent delegates the active cyber-policy flow to the custom package.
// It always handles the event; the upstream body remains in RecordCyberPolicyEvent for comparison.
func (s *ContentModerationService) tryRecordCustomCyberPolicyEvent(ctx context.Context, in CyberPolicyRecordInput) bool {
	if s == nil || s.repo == nil {
		return true
	}
	adapter := &contentModerationCyberPolicyAdapter{service: s}
	custommoderation.RecordCyberPolicyEvent(ctx, toCustomCyberPolicyEvent(in), adapter)
	return true
}

type contentModerationCyberPolicyAdapter struct {
	service *ContentModerationService
	config  *ContentModerationConfig
}

func (a *contentModerationCyberPolicyAdapter) RiskControlEnabled(ctx context.Context) bool {
	return a != nil && a.service != nil && a.service.isRiskControlEnabled(ctx)
}

func (a *contentModerationCyberPolicyAdapter) LoadScope(ctx context.Context, groupID *int64) (bool, bool, error) {
	snapshot, err := a.service.loadRuntimeSnapshot(ctx)
	if err != nil {
		return false, false, err
	}
	if snapshot.config == nil {
		return snapshot.riskControlEnabled, false, nil
	}
	return snapshot.riskControlEnabled, snapshot.config.includesGroup(groupID), nil
}

func (a *contentModerationCyberPolicyAdapter) LoadPolicy(ctx context.Context, groupID *int64) (custommoderation.Policy, error) {
	cfg, err := a.service.loadConfig(ctx)
	if err != nil {
		return custommoderation.Policy{}, err
	}
	a.config = cfg
	return custommoderation.Policy{
		InGroupScope:        cfg.includesGroup(groupID),
		ExcludeFromBanCount: cfg.CyberPolicyExcludeFromBanCount,
	}, nil
}

func (a *contentModerationCyberPolicyAdapter) Redact(text string) string {
	return redactContentModerationSecrets(text)
}

func (a *contentModerationCyberPolicyAdapter) ApplyPenalty(ctx context.Context, record *custommoderation.Record) custommoderation.PenaltyResult {
	log := toServiceContentModerationLog(record)
	justBanned := a.service.applyFlaggedAccountSideEffects(ctx, a.config, log)
	return custommoderation.PenaltyResult{
		ViolationCount: log.ViolationCount,
		AutoBanned:     log.AutoBanned,
		JustBanned:     justBanned,
	}
}

func (a *contentModerationCyberPolicyAdapter) CreateLog(ctx context.Context, record *custommoderation.Record) (int64, error) {
	log := toServiceContentModerationLog(record)
	if err := a.service.repo.CreateLog(ctx, log); err != nil {
		return 0, err
	}
	record.ID = log.ID
	record.CreatedAt = log.CreatedAt
	return log.ID, nil
}

func (a *contentModerationCyberPolicyAdapter) EmailAvailable() bool {
	return a != nil && a.service != nil && a.service.emailService != nil
}

func (a *contentModerationCyberPolicyAdapter) SendCyberPolicyNotice(ctx context.Context, record *custommoderation.Record) error {
	return a.service.sendCyberPolicyEmail(ctx, toServiceContentModerationLog(record))
}

func (a *contentModerationCyberPolicyAdapter) SendAccountDisabledNotice(ctx context.Context, record *custommoderation.Record) error {
	return a.service.sendAccountDisabledEmail(ctx, a.config, toServiceContentModerationLog(record))
}

func (a *contentModerationCyberPolicyAdapter) UpdateLogEmailSent(ctx context.Context, id int64) error {
	return a.service.repo.UpdateLogEmailSent(ctx, id, true)
}

func toCustomCyberPolicyEvent(in CyberPolicyRecordInput) custommoderation.CyberPolicyEvent {
	return custommoderation.CyberPolicyEvent{
		RequestID:       in.RequestID,
		UserID:          in.UserID,
		UserEmail:       in.UserEmail,
		APIKeyID:        in.APIKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Model:           in.Model,
		UpstreamMessage: in.UpstreamMessage,
		UpstreamBody:    in.UpstreamBody,
		UpstreamStatus:  in.UpstreamStatus,
		UpstreamInTok:   in.UpstreamInTok,
		UpstreamOutTok:  in.UpstreamOutTok,
	}
}

func toServiceContentModerationLog(record *custommoderation.Record) *ContentModerationLog {
	if record == nil {
		return nil
	}
	return &ContentModerationLog{
		ID:              record.ID,
		RequestID:       record.RequestID,
		UserID:          cloneInt64Ptr(record.UserID),
		UserEmail:       record.UserEmail,
		APIKeyID:        cloneInt64Ptr(record.APIKeyID),
		APIKeyName:      record.APIKeyName,
		GroupID:         cloneInt64Ptr(record.GroupID),
		GroupName:       record.GroupName,
		Endpoint:        record.Endpoint,
		Provider:        record.Provider,
		Model:           record.Model,
		Mode:            record.Mode,
		Action:          record.Action,
		Flagged:         record.Flagged,
		HighestCategory: record.HighestCategory,
		HighestScore:    record.HighestScore,
		Error:           record.Error,
		ViolationCount:  record.ViolationCount,
		AutoBanned:      record.AutoBanned,
		EmailSent:       record.EmailSent,
		CreatedAt:       record.CreatedAt,
	}
}
