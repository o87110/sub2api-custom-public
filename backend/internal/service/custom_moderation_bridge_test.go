//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

type customModerationSettingRepo struct {
	values map[string]string
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
