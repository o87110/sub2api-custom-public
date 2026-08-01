package apikeyrouting

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHandlerSupportsPlatform(t *testing.T) {
	tests := []struct {
		name     string
		family   HandlerFamily
		platform string
		want     bool
	}{
		{name: "gateway chat anthropic", family: HandlerFamilyGatewayChat, platform: service.PlatformAnthropic, want: true},
		{name: "gateway chat gemini", family: HandlerFamilyGatewayChat, platform: service.PlatformGemini, want: true},
		{name: "gateway chat antigravity", family: HandlerFamilyGatewayChat, platform: service.PlatformAntigravity, want: true},
		{name: "gateway chat openai", family: HandlerFamilyGatewayChat, platform: service.PlatformOpenAI, want: false},
		{name: "gateway responses anthropic", family: HandlerFamilyGatewayResponses, platform: service.PlatformAnthropic, want: true},
		{name: "gateway responses antigravity", family: HandlerFamilyGatewayResponses, platform: service.PlatformAntigravity, want: true},
		{name: "gateway responses gemini", family: HandlerFamilyGatewayResponses, platform: service.PlatformGemini, want: false},
		{name: "count tokens anthropic", family: HandlerFamilyGatewayCountTokens, platform: service.PlatformAnthropic, want: true},
		{name: "count tokens gemini", family: HandlerFamilyGatewayCountTokens, platform: service.PlatformGemini, want: false},
		{name: "gemini native gemini", family: HandlerFamilyGeminiNative, platform: service.PlatformGemini, want: true},
		{name: "gemini native antigravity", family: HandlerFamilyGeminiNative, platform: service.PlatformAntigravity, want: true},
		{name: "gemini native anthropic", family: HandlerFamilyGeminiNative, platform: service.PlatformAnthropic, want: false},
		{name: "unknown family", family: HandlerFamily(255), platform: service.PlatformAnthropic, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, HandlerSupportsPlatform(tt.family, tt.platform))
		})
	}
}
