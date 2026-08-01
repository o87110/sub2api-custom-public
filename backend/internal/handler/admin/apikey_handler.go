package admin

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService service.AdminService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService: adminService,
	}
}

// AdminUpdateAPIKeyGroupRequest represents the request to update an API key.
type AdminUpdateAPIKeyGroupRequest struct {
	GroupID             dto.NullableInt64Field `json:"group_id"`               // null/0=解绑, >0=单分组
	GroupIDs            dto.Int64SliceField    `json:"group_ids"`              // 出现时完整替换有序列表
	ResetRateLimitUsage *bool                  `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.GroupID.Set && req.GroupIDs.Set {
		response.ErrorFrom(c, service.ErrAPIKeyGroupFieldsConflict)
		return
	}

	var resetKey *service.APIKey
	if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage {
		resetKey, err = h.adminService.AdminResetAPIKeyRateLimitUsage(c.Request.Context(), keyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	var result *service.AdminUpdateAPIKeyGroupIDResult
	if req.GroupIDs.Set {
		updater, ok := h.adminService.(interface {
			AdminUpdateAPIKeyGroups(context.Context, int64, []int64) (*service.AdminUpdateAPIKeyGroupIDResult, error)
		})
		if !ok {
			response.BadRequest(c, "ordered API key groups are not supported")
			return
		}
		result, err = updater.AdminUpdateAPIKeyGroups(c.Request.Context(), keyID, req.GroupIDs.Value)
	} else {
		var groupID *int64
		if req.GroupID.Set {
			if req.GroupID.Value == nil {
				zero := int64(0)
				groupID = &zero
			} else {
				groupID = req.GroupID.Value
			}
		}
		result, err = h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, groupID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if resetKey != nil && !req.GroupID.Set && !req.GroupIDs.Set {
		result.APIKey = resetKey
	}

	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}
