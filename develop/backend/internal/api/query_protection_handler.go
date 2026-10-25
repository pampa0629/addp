package api

import (
	"errors"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
	"github.com/gin-gonic/gin"
)

// QueryProtectionOptions only returns eligible methods for the explicitly selected data item.
// @Summary 查询本人可选脱敏方式 | List my selectable masking methods
// @Tags Query
// @Produce json
// @Param engine_id query int true "引擎 ID | Engine ID"
// @Param target_locator query string true "精确数据项定位符 | Exact data item locator"
// @Success 200 {object} models.QueryProtectionOptionsResponse "字段可选方式 | Selectable field methods"
// @Failure 400 {object} models.ErrorResponse "无效来源 | Invalid source"
// @Failure 401 {object} models.ErrorResponse "需要本人登录 | Current user login required"
// @Failure 409 {object} models.ErrorResponse "角色或保护方式不可用 | Role or masking method unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.execute","develop.data_read.execute"]
// @Router /query-protection-options [get]
// @Security BearerAuth
func (h *QueryHandler) QueryProtectionOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	engine, err := strconv.ParseUint(c.Query("engine_id"), 10, 32)
	values := c.Request.URL.Query()
	if err != nil || engine == 0 || c.Query("target_locator") == "" || len(values) != 2 || len(values["engine_id"]) != 1 || len(values["target_locator"]) != 1 || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryProtectionChoiceInvalid), "error_code": "query_protection_choice_invalid"})
		return
	}
	bearer, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryProtectionRolesUnavailable), "error_code": "authentication_required"})
		return
	}
	var result *models.QueryProtectionOptionsResponse
	result, err = h.sqlEngine.QueryProtectionOptions(c.Request.Context(), tenantIDValue(c), bearer, uint(engine), c.Query("target_locator"))
	if err != nil {
		if !writeQueryProtectionError(c, err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, developi18n.MsgQueryProtectionUnavailable), "error_code": "query_protection_unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeQueryProtectionError(c *gin.Context, err error) bool {
	return writeProtectionError(c, err, developi18n.MsgQueryProtectionChoiceInvalid, developi18n.MsgQueryProtectionRolesUnavailable)
}

func writeQueryExportProtectionError(c *gin.Context, err error) bool {
	return writeProtectionError(c, err, developi18n.MsgQueryExportChoiceInvalid, developi18n.MsgQueryExportRolesUnavailable)
}

func writeProtectionError(c *gin.Context, err error, choiceMessage, rolesMessage string) bool {
	if errors.Is(err, commonAPI.ErrForbidden) {
		commonAPI.ForbiddenError(c, commoni18n.T(c, commoni18n.MsgForbidden))
		return true
	}
	if errors.Is(err, commonAPI.ErrUnauthorized) {
		commonAPI.UnauthorizedError(c, commoni18n.T(c, commoni18n.MsgUnauthorized))
		return true
	}
	key, code := "", ""
	switch {
	case errors.Is(err, dataprotection.ErrRoleOptionIneligible):
		key, code = rolesMessage, "query_protection_roles_unavailable"
	case errors.Is(err, dataprotection.ErrRoleOptionUnavailable):
		key, code = choiceMessage, "query_protection_choice_invalid"
	default:
		return false
	}
	c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, key), "error_code": code})
	return true
}
