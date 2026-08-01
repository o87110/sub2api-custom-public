package handler

import (
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompositeTargetPlatformAllowedResolvesKnownAllowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.True(t, compositeTargetPlatformAllowed(c, apiKey, "text-embedding-3-large", service.PlatformOpenAI))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenAI, platform)
}

func TestOpenAICompatibleTextTargetAllowsCompositeGrokModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

		require.True(t, openAICompatibleTextTargetAllowed(c, apiKey, "grok-4.3"), "path=%s", path)
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok, "path=%s", path)
		require.Equal(t, service.PlatformGrok, platform, "path=%s", path)
	}
}

func TestCompositeTargetPlatformAllowedRejectsWrongOrUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name  string
		model string
	}{
		{name: "wrong provider", model: "claude-sonnet-4-5"},
		{name: "unknown provider", model: "llama-4-maverick"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
			apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

			require.False(t, compositeTargetPlatformAllowed(c, apiKey, tc.model, service.PlatformOpenAI))
		})
	}
}

func TestCompositeTargetPlatformResolvedRejectsUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.False(t, compositeTargetPlatformResolved(c, apiKey, "llama-4-maverick"))
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.False(t, ok)
}

func TestCompositeTargetPlatformResolvedAllowsConcreteGroupWithoutResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformAnthropic}}

	require.True(t, compositeTargetPlatformResolved(c, apiKey, "llama-4-maverick"))
}

func TestCompositeHandlerPlatformMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		allowed  func(*gin.Context, *service.APIKey, string) bool
		platform string
		want     bool
	}{
		{name: "gateway chat anthropic", allowed: gatewayChatCompositeTargetAllowed, platform: service.PlatformAnthropic, want: true},
		{name: "gateway chat gemini", allowed: gatewayChatCompositeTargetAllowed, platform: service.PlatformGemini, want: true},
		{name: "gateway chat antigravity", allowed: gatewayChatCompositeTargetAllowed, platform: service.PlatformAntigravity, want: true},
		{name: "gateway chat rejects openai", allowed: gatewayChatCompositeTargetAllowed, platform: service.PlatformOpenAI, want: false},
		{name: "gateway responses anthropic", allowed: gatewayResponsesCompositeTargetAllowed, platform: service.PlatformAnthropic, want: true},
		{name: "gateway responses antigravity", allowed: gatewayResponsesCompositeTargetAllowed, platform: service.PlatformAntigravity, want: true},
		{name: "gateway responses rejects gemini", allowed: gatewayResponsesCompositeTargetAllowed, platform: service.PlatformGemini, want: false},
		{name: "count tokens anthropic", allowed: gatewayCountTokensCompositeTargetAllowed, platform: service.PlatformAnthropic, want: true},
		{name: "count tokens rejects gemini", allowed: gatewayCountTokensCompositeTargetAllowed, platform: service.PlatformGemini, want: false},
		{name: "gemini native gemini", allowed: geminiNativeCompositeTargetAllowed, platform: service.PlatformGemini, want: true},
		{name: "gemini native antigravity", allowed: geminiNativeCompositeTargetAllowed, platform: service.PlatformAntigravity, want: true},
		{name: "gemini native rejects anthropic", allowed: geminiNativeCompositeTargetAllowed, platform: service.PlatformAnthropic, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/test", nil)
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), tt.platform))
			apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

			require.Equal(t, tt.want, tt.allowed(c, apiKey, "public-model"))
		})
	}
}

func TestClientRequestedModelUsesCompositePublicModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "public-alias",
		TargetPlatform: service.PlatformOpenAI,
		UpstreamModel:  "gpt-5",
	}))

	input := buildContentModerationInput(c, nil, middleware2.AuthSubject{UserID: 42}, service.ContentModerationProtocolOpenAIChat, "gpt-5", nil)
	require.Equal(t, "public-alias", input.Model)
	require.Equal(t, service.PlatformOpenAI, input.Provider)

	fields := clientRequestedUsageFields(c, service.ChannelMappingResult{MappedModel: "gpt-5"}, "gpt-5", "gpt-5")
	require.Equal(t, "public-alias", fields.OriginalModel)
	require.Equal(t, "public-alias", fields.ChannelMappedModel)
	require.Equal(t, "public-alias\u2192gpt-5", fields.ModelMappingChain)
}
