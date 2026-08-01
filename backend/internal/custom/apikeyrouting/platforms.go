package apikeyrouting

import "github.com/Wei-Shaw/sub2api/internal/service"

// HandlerFamily identifies the concrete protocol adapter selected before an
// API-key candidate is attempted. Multi-group routing may resolve every
// composite candidate independently, but it must not send a candidate through
// an adapter that cannot encode that provider's upstream protocol.
type HandlerFamily uint8

const (
	HandlerFamilyGatewayChat HandlerFamily = iota + 1
	HandlerFamilyGatewayResponses
	HandlerFamilyGatewayCountTokens
	HandlerFamilyGeminiNative
)

// HandlerSupportsPlatform is the single source of truth for candidate/provider
// compatibility after the HTTP router has selected a concrete handler family.
func HandlerSupportsPlatform(family HandlerFamily, platform string) bool {
	switch family {
	case HandlerFamilyGatewayChat:
		return platform == service.PlatformAnthropic ||
			platform == service.PlatformGemini ||
			platform == service.PlatformAntigravity
	case HandlerFamilyGatewayResponses:
		return platform == service.PlatformAnthropic ||
			platform == service.PlatformAntigravity
	case HandlerFamilyGatewayCountTokens:
		return platform == service.PlatformAnthropic
	case HandlerFamilyGeminiNative:
		return platform == service.PlatformGemini ||
			platform == service.PlatformAntigravity
	default:
		return false
	}
}
