//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	custommoderation "github.com/Wei-Shaw/sub2api/internal/custom/moderation"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type customViolationCounterStub struct {
	count              int
	err                error
	calls              int
	excludeCyberPolicy bool
	apiAuditCount      int
	apiAuditErr        error
	apiAuditCalls      int
}

func (s *customViolationCounterStub) CountFlaggedByUserSince(
	_ context.Context,
	_ int64,
	_ time.Time,
	excludeCyberPolicy bool,
) (int, error) {
	s.calls++
	s.excludeCyberPolicy = excludeCyberPolicy
	return s.count, s.err
}

func (s *customViolationCounterStub) CountAPIAuditFlaggedByUserSince(
	_ context.Context,
	_ int64,
	_ time.Time,
) (int, error) {
	s.apiAuditCalls++
	return s.apiAuditCount, s.apiAuditErr
}

func TestCustomCyberPolicyPenaltyUsesUserBanThresholdOverride(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 1}
	repo := &contentModerationTestRepo{}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	service := NewContentModerationService(nil, repo, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(service, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 1
	cfg.UserBanThresholds = []UserBanThresholdOverride{{
		UserID:       userID,
		BanThreshold: 3,
	}}
	adapter := &contentModerationCyberPolicyAdapter{service: service, config: cfg}

	result := adapter.ApplyPenalty(context.Background(), &custommoderation.Record{
		UserID:  &userID,
		Flagged: true,
	})
	require.Equal(t, 2, result.ViolationCount)
	require.False(t, result.AutoBanned)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Equal(t, 3, adapter.config.BanThreshold)

	counter.count = 2
	result = adapter.ApplyPenalty(context.Background(), &custommoderation.Record{
		UserID:  &userID,
		Flagged: true,
	})
	require.Equal(t, 3, result.ViolationCount)
	require.True(t, result.AutoBanned)
	require.True(t, result.JustBanned)
	require.Equal(t, StatusDisabled, userRepo.user.Status)
}

func TestCustomCyberPolicyExcludedNoticeUsesEffectiveThresholdWithoutCounting(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 7}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	service := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(service, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 10
	cfg.CyberPolicyExcludeFromBanCount = true
	cfg.UserBanThresholds = []UserBanThresholdOverride{{UserID: userID, BanThreshold: 25}}
	adapter := &contentModerationCyberPolicyAdapter{service: service, config: cfg}
	record := &custommoderation.Record{UserID: &userID, UserEmail: "user@example.com", Model: "gpt-test"}

	log := toServiceContentModerationLog(record)
	effective := effectiveContentModerationConfigForUser(adapter.config, record.UserID)
	variables := contentModerationEmailVariables(log, effective)
	require.Equal(t, "25", variables["ban_threshold"])
	require.Zero(t, counter.calls, "排除计数时通知不得查询违规次数")
	require.Equal(t, StatusActive, userRepo.user.Status, "通知阈值解析不得触发处罚")
	require.Equal(t, 10, cfg.BanThreshold, "通知阈值派生不得改写基础配置")
	body := buildCyberPolicyNoticeEmailBody("Sub2API", log, effective)
	require.Contains(t, body, "封禁触发阈值")
	require.Contains(t, body, "25 次")
}

func TestCustomContentModerationAutoBanUsesExactUserThresholdOverride(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 2
	cfg.UserBanThresholds = []UserBanThresholdOverride{{UserID: 1001, BanThreshold: 3}}
	cfg.ViolationWindowHours = 24

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.CreateLog(context.Background(), newContentModerationFlaggedLog(userID)))
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, nil, invalidator, nil)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)
	require.Equal(t, StatusActive, userRepo.user.Status, "用户专属阈值应覆盖更低的全局阈值")
	require.Empty(t, userRepo.updated)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)
	require.Equal(t, StatusDisabled, userRepo.user.Status)
	require.Len(t, userRepo.updated, 1)
	require.Equal(t, []int64{userID}, invalidator.userIDs)
	logs := requireContentModerationLogCount(t, repo, 3)
	require.Equal(t, 3, logs[2].ViolationCount)
	require.True(t, logs[2].AutoBanned)
}

func TestCustomContentModerationAutoBanFallsBackToGlobalThresholdForOtherUsers(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 2
	cfg.UserBanThresholds = []UserBanThresholdOverride{{UserID: 2002, BanThreshold: 20}}
	cfg.ViolationWindowHours = 24

	userID := int64(1001)
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.CreateLog(context.Background(), newContentModerationFlaggedLog(userID)))
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	svc := NewContentModerationService(nil, repo, nil, nil, userRepo, nil, nil, nil)

	svc.persistContentModerationLog(context.Background(), cfg, newContentModerationFlaggedLog(userID), "", false, true)
	require.Equal(t, StatusDisabled, userRepo.user.Status)
	require.Len(t, userRepo.updated, 1)
}

func TestCustomAPIAuditBanThresholdCanBanBeforeTotalThreshold(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 1, apiAuditCount: 1}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	invalidator := &contentModerationTestAuthCacheInvalidator{}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, invalidator, nil)
	AttachCustomViolationCounter(svc, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 5
	cfg.APIAuditBanEnabled = true
	cfg.APIAuditBanThreshold = 2
	log := newContentModerationFlaggedLog(userID)

	justBanned, notificationCfg := svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log)

	require.True(t, justBanned)
	require.True(t, log.AutoBanned)
	require.Equal(t, 2, log.ViolationCount)
	require.Equal(t, 2, notificationCfg.BanThreshold)
	require.Equal(t, "2", contentModerationEmailVariables(log, notificationCfg)["ban_threshold"])
	require.Equal(t, 5, cfg.BanThreshold, "API 专属通知阈值不得改写基础配置")
	require.Equal(t, 1, counter.calls)
	require.Equal(t, 1, counter.apiAuditCalls)
	require.Equal(t, []int64{userID}, invalidator.userIDs)
}

func TestCustomAPIAuditBanThresholdIncludesCyberPolicy(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 0, apiAuditCount: 0}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(svc, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 5
	cfg.APIAuditBanEnabled = true
	cfg.APIAuditBanThreshold = 1
	log := &ContentModerationLog{UserID: &userID, Flagged: true, Action: ContentModerationActionCyberPolicy}

	justBanned, notificationCfg := svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log)

	require.True(t, justBanned)
	require.True(t, log.AutoBanned)
	require.Equal(t, 1, log.ViolationCount)
	require.Equal(t, 1, notificationCfg.BanThreshold)
	require.Equal(t, 1, counter.apiAuditCalls, "cyber_policy 命中必须进入 API 专属累计")
	require.Len(t, userRepo.updated, 1)
}

func TestCustomAPIAuditBanThresholdStillSkipsAdminAccount(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 1, apiAuditCount: 1}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleAdmin, Status: StatusActive}}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(svc, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 10
	cfg.APIAuditBanEnabled = true
	cfg.APIAuditBanThreshold = 2
	log := newContentModerationFlaggedLog(userID)

	justBanned, notificationCfg := svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log)

	require.False(t, justBanned)
	require.False(t, log.AutoBanned)
	require.Equal(t, 2, log.ViolationCount)
	require.Equal(t, 2, notificationCfg.BanThreshold)
	require.Empty(t, userRepo.updated)
}

func TestCustomAPIAuditBanThresholdKeepsTotalRulePrecedence(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 2, apiAuditCount: 1}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(svc, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 3
	cfg.APIAuditBanEnabled = true
	cfg.APIAuditBanThreshold = 2
	log := newContentModerationFlaggedLog(userID)

	justBanned, notificationCfg := svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log)

	require.True(t, justBanned)
	require.Equal(t, 3, log.ViolationCount)
	require.Same(t, cfg, notificationCfg)
	require.Len(t, userRepo.updated, 1, "两条规则同时达到时只能更新一次用户")
}

func TestCustomAPIAuditBanThresholdDoesNotApplyToKeywordOrDisabledAutoBan(t *testing.T) {
	userID := int64(1001)
	tests := []struct {
		name       string
		autoBan    bool
		action     string
		apiEnabled bool
	}{
		{name: "keyword", autoBan: true, action: ContentModerationActionKeywordBlock, apiEnabled: true},
		{name: "master disabled", autoBan: false, action: ContentModerationActionBlock, apiEnabled: true},
		{name: "API rule disabled", autoBan: true, action: ContentModerationActionBlock, apiEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := &customViolationCounterStub{count: 0, apiAuditCount: 20}
			userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
			svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
			AttachCustomViolationCounter(svc, counter)
			cfg := defaultContentModerationConfig()
			cfg.AutoBanEnabled = tt.autoBan
			cfg.BanThreshold = 10
			cfg.APIAuditBanEnabled = tt.apiEnabled
			cfg.APIAuditBanThreshold = 1
			log := &ContentModerationLog{UserID: &userID, Flagged: true, Action: tt.action}

			justBanned, _ := svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log)

			require.False(t, justBanned)
			require.False(t, log.AutoBanned)
			require.Equal(t, 0, counter.apiAuditCalls)
			require.Empty(t, userRepo.updated)
		})
	}
}

func TestCustomAPIAuditBanThresholdIgnoresUserTotalOverride(t *testing.T) {
	userID := int64(1001)
	counter := &customViolationCounterStub{count: 1, apiAuditCount: 1}
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, userRepo, nil, nil, nil)
	AttachCustomViolationCounter(svc, counter)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 3
	cfg.UserBanThresholds = []UserBanThresholdOverride{{UserID: userID, BanThreshold: 20}}
	cfg.APIAuditBanEnabled = true
	cfg.APIAuditBanThreshold = 2
	effective := effectiveContentModerationConfigForUser(cfg, &userID)
	log := newContentModerationFlaggedLog(userID)

	justBanned, notificationCfg := svc.applyFlaggedAccountSideEffects(context.Background(), effective, log)

	require.True(t, justBanned)
	require.Equal(t, 2, log.ViolationCount)
	require.Equal(t, 2, notificationCfg.BanThreshold)
	require.Equal(t, 20, effective.BanThreshold)
}

func TestCustomContentModerationEmailUsesEffectiveUserBanThreshold(t *testing.T) {
	userID := int64(1001)
	cfg := defaultContentModerationConfig()
	cfg.BanThreshold = 10
	cfg.UserBanThresholds = []UserBanThresholdOverride{{UserID: userID, BanThreshold: 25}}
	effective := effectiveContentModerationConfigForUser(cfg, &userID)

	variables := contentModerationEmailVariables(&ContentModerationLog{UserID: &userID, ViolationCount: 12}, effective)
	require.Equal(t, "25", variables["ban_threshold"])
	body := buildContentModerationAccountDisabledEmailBody("Sub2API", &ContentModerationLog{
		UserID:         &userID,
		UserEmail:      "user@example.com",
		ViolationCount: 25,
	}, effective)
	require.Contains(t, body, "25 次（阈值 25）")
}

func TestCustomContentModerationUpdateConfigUserBanThresholdsRoundTripAndClear(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil, nil)

	view, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, view.UserBanThresholds)
	require.Empty(t, view.UserBanThresholds)

	overrides := []UserBanThresholdOverride{
		{UserID: 1001, BanThreshold: 25},
		{UserID: 1002, BanThreshold: 5},
	}
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{UserBanThresholds: &overrides})
	require.NoError(t, err)
	require.Equal(t, overrides, view.UserBanThresholds)
	overrides[0].BanThreshold = 99
	require.Equal(t, 25, view.UserBanThresholds[0].BanThreshold, "配置响应不得共享调用方切片")

	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{})
	require.NoError(t, err)
	require.Equal(t, 25, view.UserBanThresholds[0].BanThreshold, "省略字段必须保留已有覆盖")

	empty := []UserBanThresholdOverride{}
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{UserBanThresholds: &empty})
	require.NoError(t, err)
	require.NotNil(t, view.UserBanThresholds)
	require.Empty(t, view.UserBanThresholds)
}

func TestCustomContentModerationUpdateConfigRejectsInvalidUserBanThresholds(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name      string
		overrides []UserBanThresholdOverride
	}{
		{name: "invalid user", overrides: []UserBanThresholdOverride{{UserID: 0, BanThreshold: 5}}},
		{name: "invalid threshold", overrides: []UserBanThresholdOverride{{UserID: 1001, BanThreshold: 1001}}},
		{name: "duplicate user", overrides: []UserBanThresholdOverride{{UserID: 1001, BanThreshold: 5}, {UserID: 1001, BanThreshold: 6}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{UserBanThresholds: &tt.overrides})
			require.Error(t, err)
			require.Contains(t, err.Error(), "用户专属封禁阈值无效")
		})
	}
}

func TestCustomContentModerationAPIAuditBanConfigLegacyDefaultAndRoundTrip(t *testing.T) {
	legacy, err := parseContentModerationConfig(`{"ban_threshold":3}`)
	require.NoError(t, err)
	require.False(t, legacy.APIAuditBanEnabled)
	require.Equal(t, 3, legacy.APIAuditBanThreshold)

	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil, nil)
	enabled := true
	threshold := 2
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIAuditBanEnabled:   &enabled,
		APIAuditBanThreshold: &threshold,
	})
	require.NoError(t, err)
	require.True(t, view.APIAuditBanEnabled)
	require.Equal(t, 2, view.APIAuditBanThreshold)

	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{})
	require.NoError(t, err)
	require.True(t, view.APIAuditBanEnabled)
	require.Equal(t, 2, view.APIAuditBanThreshold)
}

func TestCustomContentModerationUpdateConfigRejectsInvalidAPIAuditBanThreshold(t *testing.T) {
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil, nil)

	for _, threshold := range []int{0, 1001} {
		_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
			APIAuditBanThreshold: &threshold,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "API 审计封禁阈值")
	}
}

type customModerationSettingRepo struct {
	values            map[string]string
	runtimeLoaded     chan struct{}
	runtimeLoadedOnce sync.Once
}

func (r *customModerationSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	if value, ok := r.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (r *customModerationSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *customModerationSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *customModerationSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	if r.runtimeLoaded != nil {
		r.runtimeLoadedOnce.Do(func() { close(r.runtimeLoaded) })
	}
	return values, nil
}

func (r *customModerationSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *customModerationSettingRepo) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *customModerationSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type customModerationRepo struct {
	mu   sync.Mutex
	logs []ContentModerationLog
}

func (r *customModerationRepo) CreateLog(_ context.Context, log *ContentModerationLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if log != nil {
		r.logs = append(r.logs, *log)
	}
	return nil
}

func (r *customModerationRepo) ListLogs(
	context.Context,
	ContentModerationLogFilter,
) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *customModerationRepo) CountFlaggedByUserSince(context.Context, int64, time.Time, bool) (int, error) {
	return 0, nil
}

func (r *customModerationRepo) CleanupExpiredLogs(
	context.Context,
	time.Time,
	time.Time,
) (*ContentModerationCleanupResult, error) {
	return &ContentModerationCleanupResult{}, nil
}

func (r *customModerationRepo) UpdateLogEmailSent(context.Context, int64, bool) error {
	return nil
}

func (r *customModerationRepo) snapshotLogs() []ContentModerationLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	logs := make([]ContentModerationLog, len(r.logs))
	copy(logs, r.logs)
	return logs
}

func TestCustomViolationCounterAttachmentOverridesOfficialCountPort(t *testing.T) {
	service := &ContentModerationService{}
	counter := &customViolationCounterStub{count: 7}
	AttachCustomViolationCounter(service, counter)

	count, err := service.countFlaggedByUserSince(context.Background(), 1001, time.Now(), true)

	require.NoError(t, err)
	require.Equal(t, 7, count)
	require.Equal(t, 1, counter.calls)
	require.True(t, counter.excludeCyberPolicy)
}

func TestCustomViolationCounterAttachmentProvidesAPIAuditCount(t *testing.T) {
	service := &ContentModerationService{}
	counter := &customViolationCounterStub{apiAuditCount: 4}
	AttachCustomViolationCounter(service, counter)

	count, err := service.countAPIAuditFlaggedByUserSince(context.Background(), 1001, time.Now())

	require.NoError(t, err)
	require.Equal(t, 4, count)
	require.Equal(t, 1, counter.apiAuditCalls)
}

func TestCustomModerationExcerptBoundaries(t *testing.T) {
	service := &ContentModerationService{}
	config := defaultContentModerationConfig()
	input := ContentModerationCheckInput{
		RequestID: "req-limit",
		Endpoint:  "/v1/chat/completions",
		Provider:  "openai",
	}
	plainText := strings.Repeat("中", maxModerationExcerptRunes+25)

	plainLog := service.buildLog(input, config, ContentModerationActionAllow, false, "", 0, nil, plainText, nil, nil, "")
	require.Len(t, []rune(plainLog.InputExcerpt), maxModerationExcerptRunes)

	customText := "token=abc123456789xyz " + strings.Repeat("前", 1200) +
		" https://example.com/private/path?token=abc123 风控绕过 后文"
	customLog := service.buildLog(
		input,
		config,
		ContentModerationActionKeywordBlock,
		true,
		contentModerationKeywordCategory,
		1,
		map[string]float64{contentModerationKeywordCategory: 1},
		customText,
		nil,
		nil,
		"",
	)
	applyCustomContentModerationKeywordExcerpt(customLog, customText, "风控绕过")

	require.LessOrEqual(t, len([]rune(customLog.InputExcerpt)), custommoderation.MaxKeywordExcerptRunes)
	require.Contains(t, customLog.InputExcerpt, "风控绕过")
	require.Contains(t, customLog.InputExcerpt, "[已脱敏]")
	require.NotContains(t, customLog.InputExcerpt, "abc123456789xyz")
	require.NotContains(t, customLog.InputExcerpt, "https://example.com/private/path")
	require.Equal(t, "风控绕过", customLog.MatchedKeyword)
}

func TestCustomModerationExcerptRedactsBeforeSelectingContext(t *testing.T) {
	text := strings.Repeat("前", 1200) + " token=abc123456789xyz"
	log := (&ContentModerationService{}).buildLog(
		ContentModerationCheckInput{},
		defaultContentModerationConfig(),
		ContentModerationActionKeywordBlock,
		true,
		contentModerationKeywordCategory,
		1,
		map[string]float64{contentModerationKeywordCategory: 1},
		text,
		nil,
		nil,
		"",
	)

	applyCustomContentModerationKeywordExcerpt(log, text, "abc123456789xyz")

	require.Len(t, []rune(log.InputExcerpt), custommoderation.MaxKeywordExcerptRunes)
	require.Equal(t, strings.Repeat("前", custommoderation.MaxKeywordExcerptRunes), log.InputExcerpt)
	require.NotContains(t, log.InputExcerpt, "abc123456789xyz")
	require.NotContains(t, log.InputExcerpt, custommoderation.KeywordContextSeparator)
}

func TestCustomModerationLateKeywordShortCircuitsUpstreamAndKeepsContext(t *testing.T) {
	upstreamCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{}}})
	}))
	defer server.Close()

	config := defaultContentModerationConfig()
	config.Enabled = true
	config.Mode = ContentModerationModePreBlock
	config.BaseURL = server.URL
	config.APIKeys = []string{"sk-test"}
	const keyword = "风控绕过"
	config.BlockedKeywords = []string{keyword}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	repository := &customModerationRepo{}
	service := NewContentModerationService(
		&customModerationSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawConfig),
		}},
		repository,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	prompt := strings.Repeat("前", maxModerationInputRunes-len([]rune(keyword))) + keyword
	body, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	})
	require.NoError(t, err)
	decision, err := service.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.False(t, upstreamCalled)
	require.Eventually(t, func() bool {
		return len(repository.snapshotLogs()) == 1
	}, time.Second, 10*time.Millisecond)
	logs := repository.snapshotLogs()
	require.Equal(t, keyword, logs[0].MatchedKeyword)
	require.Len(t, []rune(logs[0].InputExcerpt), custommoderation.MaxKeywordExcerptRunes)
	require.Contains(t, logs[0].InputExcerpt, custommoderation.KeywordContextSeparator)
	require.Contains(t, logs[0].InputExcerpt, keyword)
}

func TestCustomAPIAuditScopePreservesKeywordsAndNarrowsAPICalls(t *testing.T) {
	var upstreamCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{
			CategoryScores: map[string]float64{"sexual": 0.1},
		}}})
	}))
	defer server.Close()

	config := defaultContentModerationConfig()
	config.Enabled = true
	config.Mode = ContentModerationModePreBlock
	config.BaseURL = server.URL
	config.APIKeys = []string{"sk-test"}
	config.BlockedKeywords = []string{"blocked"}
	config.APIAuditScope = &APIAuditScope{GroupIDs: []int64{2}}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	service := NewContentModerationService(
		&customModerationSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawConfig),
		}},
		&customModerationRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	groupOne := int64(1)
	groupTwo := int64(2)
	request := func(groupID *int64, text string) *ContentModerationDecision {
		body, marshalErr := json.Marshal(map[string]any{
			"messages": []map[string]any{{"role": "user", "content": text}},
		})
		require.NoError(t, marshalErr)
		decision, checkErr := service.Check(context.Background(), ContentModerationCheckInput{
			GroupID:  groupID,
			Endpoint: "/v1/messages",
			Provider: "anthropic",
			Protocol: ContentModerationProtocolAnthropicMessages,
			Body:     body,
		})
		require.NoError(t, checkErr)
		return decision
	}

	require.True(t, request(&groupOne, "blocked input").Blocked)
	require.Equal(t, int64(0), upstreamCalls.Load())
	require.True(t, request(&groupOne, "clean input").Allowed)
	require.Equal(t, int64(0), upstreamCalls.Load())
	require.True(t, request(&groupTwo, "clean input").Allowed)
	require.Equal(t, int64(1), upstreamCalls.Load())
}

func TestCustomAPIAuditScopeLegacyConfigDefaultsToParentScope(t *testing.T) {
	config, err := parseContentModerationConfig(`{"all_groups":false,"group_ids":[7]}`)
	require.NoError(t, err)
	require.Equal(t, &APIAuditScope{AllInScope: true, GroupIDs: []int64{}}, config.APIAuditScope)

	inside := int64(7)
	outside := int64(8)
	require.True(t, config.includesAPIAuditGroup(&inside))
	require.False(t, config.includesAPIAuditGroup(&outside))

	emptyConfig, err := parseContentModerationConfig(`{"all_groups":false,"group_ids":[]}`)
	require.NoError(t, err)
	require.NoError(t, (&ContentModerationService{}).validateConfig(context.Background(), emptyConfig))
}

func TestCustomAPIAuditScopeRejectsEmptyActiveSelection(t *testing.T) {
	service := NewContentModerationService(
		&customModerationSettingRepo{values: map[string]string{}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	_, err := service.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIAuditScope: &APIAuditScope{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_CONTENT_MODERATION_API_AUDIT_SCOPE")
}

func TestCustomAPIAuditScopeRequirementMatchesAPIStrategy(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		strategy     string
		requireError bool
	}{
		{name: "pre-block combined", mode: ContentModerationModePreBlock, strategy: ContentModerationKeywordModeKeywordAndAPI, requireError: true},
		{name: "off combined", mode: ContentModerationModeOff, strategy: ContentModerationKeywordModeKeywordAndAPI, requireError: true},
		{name: "pre-block keyword only", mode: ContentModerationModePreBlock, strategy: ContentModerationKeywordModeKeywordOnly},
		{name: "off keyword only", mode: ContentModerationModeOff, strategy: ContentModerationKeywordModeKeywordOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := defaultContentModerationConfig()
			config.Mode = tt.mode
			config.KeywordBlockingMode = tt.strategy
			config.APIAuditScope = &APIAuditScope{}
			err := (&ContentModerationService{}).validateConfig(context.Background(), config)
			if tt.requireError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "INVALID_CONTENT_MODERATION_API_AUDIT_SCOPE")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCustomAPIAuditScopeObserveModeSkipsExcludedGroups(t *testing.T) {
	var upstreamCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{}}})
	}))
	defer server.Close()

	config := defaultContentModerationConfig()
	config.Enabled = true
	config.Mode = ContentModerationModeObserve
	config.BaseURL = server.URL
	config.APIKeys = []string{"sk-test"}
	config.APIAuditScope = &APIAuditScope{GroupIDs: []int64{2}}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	service := NewContentModerationService(
		&customModerationSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawConfig),
		}},
		&customModerationRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	body := []byte(`{"messages":[{"role":"user","content":"clean input"}]}`)
	groupOne := int64(1)
	groupTwo := int64(2)
	for _, groupID := range []*int64{&groupOne, &groupTwo} {
		decision, checkErr := service.Check(context.Background(), ContentModerationCheckInput{
			GroupID:  groupID,
			Endpoint: "/v1/messages",
			Provider: "anthropic",
			Protocol: ContentModerationProtocolAnthropicMessages,
			Body:     body,
		})
		require.NoError(t, checkErr)
		require.True(t, decision.Allowed)
	}
	require.Eventually(t, func() bool { return upstreamCalls.Load() == 1 }, time.Second, 10*time.Millisecond)
}

func TestCustomAPIAuditScopeWorkerRechecksScopeAfterDequeue(t *testing.T) {
	var upstreamCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{}}})
	}))
	defer server.Close()

	config := defaultContentModerationConfig()
	config.Enabled = true
	config.Mode = ContentModerationModeObserve
	config.BaseURL = server.URL
	config.APIKeys = []string{"sk-test"}
	config.WorkerCount = 1
	config.APIAuditScope = &APIAuditScope{GroupIDs: []int64{2}}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	runtimeLoaded := make(chan struct{})
	service := NewContentModerationService(
		&customModerationSettingRepo{
			values: map[string]string{
				SettingKeyRiskControlEnabled:      "true",
				SettingKeyContentModerationConfig: string(rawConfig),
			},
			runtimeLoaded: runtimeLoaded,
		},
		&customModerationRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	select {
	case <-runtimeLoaded:
	case <-time.After(time.Second):
		t.Fatal("worker did not load the initial runtime snapshot")
	}
	require.Eventually(t, func() bool { return service.runtimeSnapshot.Load() != nil }, time.Second, 10*time.Millisecond)

	_, err = service.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIAuditScope: &APIAuditScope{GroupIDs: []int64{1}},
	})
	require.NoError(t, err)

	groupTwo := int64(2)
	content := ContentModerationInput{Text: "clean input"}
	content.Normalize()
	service.enqueueAsync(ContentModerationCheckInput{
		GroupID:  &groupTwo,
		Endpoint: "/v1/messages",
		Provider: "anthropic",
		Protocol: ContentModerationProtocolAnthropicMessages,
	}, config, content, content.Hash())

	require.Eventually(t, func() bool { return len(service.asyncQueue) == 0 }, time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return upstreamCalls.Load() != 0 }, 200*time.Millisecond, 10*time.Millisecond)
}

func TestCustomContentModerationWorkerCompletesDequeuedLogAfterScaleDown(t *testing.T) {
	config := defaultContentModerationConfig()
	config.WorkerCount = 2
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	runtimeLoaded := make(chan struct{})
	settings := &customModerationSettingRepo{
		values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawConfig),
		},
		runtimeLoaded: runtimeLoaded,
	}
	repository := &customModerationRepo{}
	service := &ContentModerationService{
		settingRepo: settings,
		repo:        repository,
		asyncQueue:  make(chan contentModerationTask, 1),
	}
	go service.worker(1)

	select {
	case <-runtimeLoaded:
	case <-time.After(time.Second):
		t.Fatal("worker did not load the initial runtime snapshot")
	}
	require.Eventually(t, func() bool { return service.runtimeSnapshot.Load() != nil }, time.Second, 10*time.Millisecond)

	workerCount := 1
	_, err = service.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		WorkerCount: &workerCount,
	})
	require.NoError(t, err)

	service.enqueueRecord(
		ContentModerationCheckInput{Endpoint: "/v1/messages"},
		config,
		&ContentModerationLog{Action: ContentModerationActionAllow},
		"",
		false,
		false,
	)

	require.Eventually(t, func() bool { return len(repository.snapshotLogs()) == 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(0), service.asyncDropped.Load())
	require.Equal(t, int64(1), service.asyncProcessed.Load())
}
