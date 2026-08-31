package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupModelsListConfigDefaultsToNoBlockedModels(t *testing.T) {
	normalized := normalizeGroupModelsListConfig(GroupModelsListConfig{})

	require.False(t, normalized.Enabled)
	require.Nil(t, normalized.Models)
	require.Nil(t, normalized.BlockedModels)
	require.False(t, (&Group{ModelsListConfig: normalized}).BlocksModel("gpt-5.4-mini"))
}

func TestNormalizeGroupModelsListConfigNormalizesListsIndependently(t *testing.T) {
	normalized := normalizeGroupModelsListConfig(GroupModelsListConfig{
		Enabled:       true,
		Models:        []string{" gpt-5.6-sol ", "", "gpt-5.6-sol", "gpt-5.6-terra"},
		BlockedModels: []string{" gpt-5.6-luna ", "", "gpt-5.6-luna", "gpt-5.4-mini"},
	})

	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-terra"}, normalized.Models)
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.4-mini"}, normalized.BlockedModels)
}

func TestOpenAIAccountModelAccessChecksOnlyResolvedFinalModel(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-5.6-luna", "gpt-5.4-mini"},
	}})
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"luna-alias":  "gpt-5.6-luna",
			"haiku-alias": "gpt-5.4-mini",
			"sol-alias":   "gpt-5.6-sol",
		}},
	}

	require.Error(t, CheckOpenAIAccountModelAccess(ctx, account, "luna-alias", false))
	require.Error(t, CheckOpenAIAccountModelAccess(ctx, account, "haiku-alias", false))
	require.NoError(t, CheckOpenAIAccountModelAccess(ctx, account, "sol-alias", false))
	require.NoError(t, CheckOpenAIAccountModelAccess(ctx, account, "gpt-5.6-terra", false))
}

func TestOpenAIAccountModelAccessNormalizesGPT56ToSol(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-5.6-luna"},
	}})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	require.Equal(t, "gpt-5.6-sol", resolveOpenAIAccountModelForAccess(ctx, account, "gpt-5.6", false))
	require.NoError(t, CheckOpenAIAccountModelAccess(ctx, account, "gpt-5.6", false))
}

func TestOpenAIAccountModelAccessSkipsOnlyBlockedMappedPath(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-5.4-mini"},
	}})
	blockedAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"public-model": "gpt-5.4-mini",
		}},
	}
	allowedAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"public-model": "gpt-5.6-sol",
		}},
	}

	require.True(t, modelAccessBlocksOpenAIAccount(ctx, blockedAccount, "public-model", false))
	require.False(t, modelAccessBlocksOpenAIAccount(ctx, allowedAccount, "public-model", false))
}

func TestOpenAIMessagesHaikuDispatchToMiniIsBlockedBeforeNetwork(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-5.4-mini"},
	}})
	ctx = groupmodelaccess.WithFallbackModel(ctx, "gpt-5.4-mini")
	svc := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	_, err := svc.ForwardAsAnthropic(
		ctx,
		nil,
		account,
		[]byte(`{"model":"claude-haiku-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`),
		"",
		"gpt-5.4-mini",
	)

	require.True(t, IsGroupModelBlockedError(err))
	require.Equal(t, "gpt-5.4-mini", GroupModelBlockedModel(err))
}

func TestOpenAIOAuthImagePathIgnoresInternalResponsesModelBlock(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{openAIImagesResponsesMainModel},
	}})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	err := CheckOpenAIAccountModelAccess(ctx, account, openAIImagesResponsesMainModel, false)
	require.True(t, IsGroupModelBlockedError(err))
	require.Equal(t, openAIImagesResponsesMainModel, GroupModelBlockedModel(err))
	require.NoError(t, CheckOpenAIAccountModelAccess(ctx, account, "gpt-image-2", false))
}

func TestOpenAIOAuthImagePathStillBlocksRequestedImageModel(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-image-2"},
	}})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	err := CheckOpenAIAccountModelAccess(ctx, account, "gpt-image-2", false)

	require.True(t, IsGroupModelBlockedError(err))
	require.Equal(t, "gpt-image-2", GroupModelBlockedModel(err))
}

func TestSelectionGroupModelAccessUnionsOriginalAndFallbackPolicies(t *testing.T) {
	ctx := groupmodelaccess.WithPolicy(context.Background(), groupmodelaccess.NewPolicy([]string{"gpt-5.6-luna"}))
	selection := &AccountSelectionResult{
		modelAccessPolicy: groupmodelaccess.NewPolicy([]string{"gpt-5.4-mini"}),
	}

	ctx = ContextWithSelectionGroupModelAccess(ctx, selection)

	require.Error(t, CheckGroupModelAccess(ctx, "gpt-5.6-luna"))
	require.Error(t, CheckGroupModelAccess(ctx, "gpt-5.4-mini"))
	require.NoError(t, CheckGroupModelAccess(ctx, "gpt-5.6-sol"))
}

func TestFilterCodexModelsManifestKeepsSolAndRemovesOnlyBlockedLuna(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gpt-5.6-luna"},
	}})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"models":[{"slug":"gpt-5.6"},{"slug":"gpt-5.6-sol"},{"slug":"gpt-5.6-terra"},{"slug":"gpt-5.6-luna"}],"version":1}`)

	filtered, changed, err := FilterCodexModelsManifest(ctx, account, body, nil, false)

	require.NoError(t, err)
	require.True(t, changed)
	var envelope struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(filtered, &envelope))
	slugs := make([]string, 0, len(envelope.Models))
	for _, model := range envelope.Models {
		slugs = append(slugs, model.Slug)
	}
	require.Equal(t, []string{"gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra"}, slugs)
}

func TestFilterCodexModelsManifestRemovesBlockedPublicSlugWithoutSelectedAccount(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"public-blocked"},
	}})
	body := []byte(`{"models":[{"slug":"public-blocked"},{"slug":"public-allowed"}],"version":1}`)

	filtered, changed, err := FilterCodexModelsManifest(ctx, nil, body, nil, false)

	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, string(filtered), "public-blocked")
	require.Contains(t, string(filtered), "public-allowed")
}

func TestGeminiAndAntigravityFinalGuardsBlockMappedTargets(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"gemini-3.1-pro"},
	}})
	account := &Account{
		Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"public-gemini": "gemini-3.1-pro",
		}},
	}
	geminiSvc := &GeminiMessagesCompatService{}

	_, err := geminiSvc.ForwardNative(ctx, nil, account, "public-gemini", "generateContent", false, []byte(`{"contents":[]}`))
	require.True(t, IsGroupModelBlockedError(err))

	account.Platform = PlatformAntigravity
	antigravitySvc := &AntigravityGatewayService{}
	_, err = antigravitySvc.ForwardGemini(ctx, nil, account, "public-gemini", "generateContent", false, []byte(`{"contents":[]}`), false)
	require.True(t, IsGroupModelBlockedError(err))
}

func TestAntigravityUpstreamPassthroughFinalGuardBlocksActualBodyModel(t *testing.T) {
	ctx := WithCurrentGroupModelAccess(context.Background(), &Group{ModelsListConfig: GroupModelsListConfig{
		BlockedModels: []string{"claude-blocked"},
	}})
	account := &Account{
		Platform: PlatformAntigravity, Type: AccountTypeUpstream,
		Credentials: map[string]any{"base_url": "https://example.test", "api_key": "secret"},
	}
	svc := &AntigravityGatewayService{}

	_, err := svc.ForwardUpstream(ctx, nil, account, []byte(`{"model":"claude-blocked","max_tokens":8,"messages":[]}`))

	require.True(t, IsGroupModelBlockedError(err))
}
