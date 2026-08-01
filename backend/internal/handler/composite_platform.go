package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/custom/apikeyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func ensureCompositeTargetPlatform(c *gin.Context, apiKey *service.APIKey, model string) {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
		return
	}
	if _, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
		return
	}
	if platform, ok := service.DetectModelPlatform(model); ok {
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), platform))
	}
}

func compositeTargetPlatformAllowed(c *gin.Context, apiKey *service.APIKey, model string, allowed ...string) bool {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
		return true
	}
	ensureCompositeTargetPlatform(c, apiKey, model)
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	if !ok {
		return false
	}
	for _, allowedPlatform := range allowed {
		if platform == allowedPlatform {
			return true
		}
	}
	return false
}

func compositeTargetPlatformResolved(c *gin.Context, apiKey *service.APIKey, model string) bool {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
		return true
	}
	ensureCompositeTargetPlatform(c, apiKey, model)
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	return ok
}

func compositeTargetPlatformAllowedForHandler(
	c *gin.Context,
	apiKey *service.APIKey,
	model string,
	family apikeyrouting.HandlerFamily,
) bool {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
		return true
	}
	ensureCompositeTargetPlatform(c, apiKey, model)
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	return ok && apikeyrouting.HandlerSupportsPlatform(family, platform)
}

// gatewayChatCompositeTargetAllowed mirrors the concrete forwarders owned by
// GatewayHandler.ChatCompletions. OpenAI/Grok candidates belong to the
// OpenAIGatewayHandler family and must not be sent through the Anthropic
// compatibility forwarder selected for an earlier candidate.
func gatewayChatCompositeTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
	return compositeTargetPlatformAllowedForHandler(
		c, apiKey, model, apikeyrouting.HandlerFamilyGatewayChat)
}

// gatewayResponsesCompositeTargetAllowed mirrors the concrete forwarders
// owned by GatewayHandler.Responses. Gemini has a Chat Completions adapter but
// no Responses adapter in this handler family.
func gatewayResponsesCompositeTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
	return compositeTargetPlatformAllowedForHandler(
		c, apiKey, model, apikeyrouting.HandlerFamilyGatewayResponses)
}

func gatewayCountTokensCompositeTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
	return compositeTargetPlatformAllowedForHandler(
		c, apiKey, model, apikeyrouting.HandlerFamilyGatewayCountTokens)
}

func geminiNativeCompositeTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
	return compositeTargetPlatformAllowedForHandler(
		c, apiKey, model, apikeyrouting.HandlerFamilyGeminiNative)
}

func effectiveAPIKeyPlatform(c *gin.Context, apiKey *service.APIKey) string {
	if c != nil && c.Request != nil {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}
