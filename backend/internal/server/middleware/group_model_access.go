package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupmodelaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// bindAndEnforceGroupModelAccess is the thin authentication bridge for the
// custom group model policy. It must run after the API key group is loaded and
// before downstream route/model rewrites execute.
func bindAndEnforceGroupModelAccess(c *gin.Context, apiKey *service.APIKey) bool {
	var blockedModels []string
	if apiKey != nil && apiKey.Group != nil {
		blockedModels = apiKey.Group.ModelsListConfig.BlockedModels
	}
	model, blocked, err := groupmodelaccess.BindAndInspectRequest(c.Request, blockedModels)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request_error", "message": "Failed to read request body"},
		})
		return false
	}
	if !blocked {
		return true
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
	groupmodelaccess.WriteBlockedResponse(c, model)
	return false
}
