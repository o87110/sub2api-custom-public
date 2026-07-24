package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type cyberPolicyAdapterStub struct {
	enabled        bool
	policy         Policy
	loadErr        error
	penalty        PenaltyResult
	createErr      error
	createdID      int64
	emailAvailable bool
	cyberEmailErr  error
	banEmailErr    error
	updateErr      error
	penaltyCalls   int
	createCalls    int
	cyberCalls     int
	banCalls       int
	updateCalls    int
	lastRecord     *Record
	calls          []string
}

func (s *cyberPolicyAdapterStub) RiskControlEnabled(context.Context) bool { return s.enabled }
func (s *cyberPolicyAdapterStub) LoadPolicy(context.Context, *int64) (Policy, error) {
	return s.policy, s.loadErr
}
func (s *cyberPolicyAdapterStub) Redact(text string) string {
	return strings.ReplaceAll(text, "secret-token", "[redacted]")
}
func (s *cyberPolicyAdapterStub) ApplyPenalty(_ context.Context, record *Record) PenaltyResult {
	s.penaltyCalls++
	s.calls = append(s.calls, "penalty")
	s.lastRecord = record
	return s.penalty
}
func (s *cyberPolicyAdapterStub) CreateLog(_ context.Context, record *Record) (int64, error) {
	s.createCalls++
	s.calls = append(s.calls, "create")
	s.lastRecord = record
	return s.createdID, s.createErr
}
func (s *cyberPolicyAdapterStub) EmailAvailable() bool { return s.emailAvailable }
func (s *cyberPolicyAdapterStub) SendCyberPolicyNotice(_ context.Context, record *Record) error {
	s.cyberCalls++
	s.calls = append(s.calls, "cyber_email")
	s.lastRecord = record
	return s.cyberEmailErr
}
func (s *cyberPolicyAdapterStub) SendAccountDisabledNotice(_ context.Context, record *Record) error {
	s.banCalls++
	s.calls = append(s.calls, "ban_email")
	s.lastRecord = record
	return s.banEmailErr
}
func (s *cyberPolicyAdapterStub) UpdateLogEmailSent(context.Context, int64) error {
	s.updateCalls++
	s.calls = append(s.calls, "update_email_sent")
	return s.updateErr
}

func TestRecordCyberPolicyEventDisabled(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 1}, adapter)

	require.Zero(t, adapter.createCalls)
	require.Zero(t, adapter.penaltyCalls)
}

type cyberPolicyScopeAdapterStub struct {
	enabled bool
	inScope bool
	err     error
}

func (s cyberPolicyScopeAdapterStub) LoadScope(context.Context, *int64) (bool, bool, error) {
	return s.enabled, s.inScope, s.err
}

func TestCyberPolicyGroupInScope(t *testing.T) {
	require.True(t, CyberPolicyGroupInScope(context.Background(), nil, cyberPolicyScopeAdapterStub{
		enabled: true,
		inScope: true,
	}))
	require.False(t, CyberPolicyGroupInScope(context.Background(), nil, cyberPolicyScopeAdapterStub{
		enabled: true,
		inScope: false,
	}))
	require.False(t, CyberPolicyGroupInScope(context.Background(), nil, cyberPolicyScopeAdapterStub{
		enabled: true,
		inScope: true,
		err:     errors.New("settings unavailable"),
	}))
}

func TestRecordCyberPolicyEventOutOfScopeOnlyPersistsAudit(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:        true,
		createdID:      42,
		emailAvailable: true,
	}
	event := CyberPolicyEvent{
		UserID:          7,
		UserEmail:       "user@example.com",
		UpstreamMessage: "secret-token",
	}

	RecordCyberPolicyEvent(context.Background(), event, adapter)

	require.Equal(t, 1, adapter.createCalls)
	require.Zero(t, adapter.penaltyCalls)
	require.Zero(t, adapter.cyberCalls)
	require.Zero(t, adapter.banCalls)
	require.Equal(t, ActionCyberPolicyOutOfScope, adapter.lastRecord.Action)
	require.Contains(t, adapter.lastRecord.Error, "[redacted]")
}

func TestRecordCyberPolicyEventConfigFailureFailsOpen(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:   true,
		loadErr:   errors.New("settings unavailable"),
		createdID: 1,
	}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 7}, adapter)

	require.Equal(t, 1, adapter.createCalls)
	require.Zero(t, adapter.penaltyCalls)
	require.Equal(t, ActionCyberPolicyOutOfScope, adapter.lastRecord.Action)
}

func TestRecordCyberPolicyEventInScopeExcludedFromBanCount(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:        true,
		policy:         Policy{InGroupScope: true, ExcludeFromBanCount: true},
		createdID:      9,
		emailAvailable: true,
	}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 7, UserEmail: "user@example.com"}, adapter)

	require.Zero(t, adapter.penaltyCalls)
	require.Equal(t, 1, adapter.cyberCalls)
	require.Zero(t, adapter.banCalls)
	require.Equal(t, 1, adapter.updateCalls)
	require.Equal(t, ActionCyberPolicy, adapter.lastRecord.Action)
}

func TestRecordCyberPolicyEventAppliesPenaltyAndSendsBothEmails(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:        true,
		policy:         Policy{InGroupScope: true},
		penalty:        PenaltyResult{ViolationCount: 3, AutoBanned: true, JustBanned: true},
		createdID:      11,
		emailAvailable: true,
	}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 7, UserEmail: "user@example.com"}, adapter)

	require.Equal(t, 1, adapter.penaltyCalls)
	require.Equal(t, 1, adapter.cyberCalls)
	require.Equal(t, 1, adapter.banCalls)
	require.Equal(t, 1, adapter.updateCalls)
	require.Equal(t, 3, adapter.lastRecord.ViolationCount)
	require.True(t, adapter.lastRecord.AutoBanned)
	require.True(t, adapter.lastRecord.EmailSent)
	require.Equal(t, []string{"penalty", "create", "cyber_email", "ban_email", "update_email_sent"}, adapter.calls)
}

func TestRecordCyberPolicyEventDoesNotUpdateMissingLog(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:        true,
		policy:         Policy{InGroupScope: true},
		createErr:      errors.New("database unavailable"),
		emailAvailable: true,
	}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 7, UserEmail: "user@example.com"}, adapter)

	require.Equal(t, 1, adapter.cyberCalls)
	require.Zero(t, adapter.updateCalls)
	require.Equal(t, []string{"penalty", "create", "cyber_email"}, adapter.calls)
}

func TestRecordCyberPolicyEventDoesNotMarkFailedEmails(t *testing.T) {
	adapter := &cyberPolicyAdapterStub{
		enabled:        true,
		policy:         Policy{InGroupScope: true},
		createdID:      12,
		emailAvailable: true,
		cyberEmailErr:  errors.New("smtp unavailable"),
	}

	RecordCyberPolicyEvent(context.Background(), CyberPolicyEvent{UserID: 7, UserEmail: "user@example.com"}, adapter)

	require.Zero(t, adapter.updateCalls)
	require.False(t, adapter.lastRecord.EmailSent)
}
