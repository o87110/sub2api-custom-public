//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type customModerationHandlerSettingRepo struct {
	values map[string]string
}

func (r *customModerationHandlerSettingRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	if value, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *customModerationHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *customModerationHandlerSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *customModerationHandlerSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *customModerationHandlerSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *customModerationHandlerSettingRepo) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *customModerationHandlerSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func TestCustomCyberPolicyOutOfScopeBypassesLocalBlockLookup(t *testing.T) {
	c := newTestGinContext()
	c.Request = httptest.NewRequest("POST", "/openai/v1/responses", strings.NewReader(`{}`))
	moderationService := service.NewContentModerationService(
		&customModerationHandlerSettingRepo{values: map[string]string{
			service.SettingKeyRiskControlEnabled:      "true",
			service.SettingKeyContentModerationConfig: `{"all_groups":false,"group_ids":[7]}`,
		}},
		nil, nil, nil, nil, nil, nil,
	)
	outsideGroupID := int64(9)
	handler := &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		contentModerationService: moderationService,
	}
	key := &service.APIKey{ID: 1, GroupID: &outsideGroupID}

	blocked := handler.rejectIfCyberSessionBlocked(
		c,
		key,
		[]byte(`{"prompt_cache_key":"old-blocked-session"}`),
		"gpt-5",
		cyberBlockFormatResponses,
	)

	require.False(t, blocked)
}
