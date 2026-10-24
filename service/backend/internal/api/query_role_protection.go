package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/addp/common/dataprotection"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/protection"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

func queryProtectionContext(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if _, application := c.Get(apiConsumerContextKey); application || strings.TrimSpace(c.GetHeader("X-API-Key")) != "" {
		return ctx
	}
	facts, ok := commonauth.AuthContextFromGin(c)
	if !ok {
		return ctx
	}
	return svc.WithQueryProtectionUser(ctx, facts, commonauth.CanonicalBearerToken(c.GetHeader("Authorization")))
}

// QueryProtectionOptions returns only this user's methods for one explicit source.
// @Summary 查询字段脱敏可选方式 | List selectable field masking methods
// @Description 仅当前第一方登录用户及精确 DataItem 来源，沿 Service 保护投影核验 | Verify the current first-party User and exact DataItem against the Service protection projection
// @Tags QueryService
// @Produce json
// @Param target_locator query string true "来源 DataItem locator | Source DataItem locator"
// @Success 200 {object} models.QueryProtectionOptionsResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query-protection-options [get]
// @Security BearerAuth
func (h *QueryServiceHandler) QueryProtectionOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if len(c.Request.URL.Query()) != 1 || len(c.Request.URL.Query()["target_locator"]) != 1 || c.Query("target_locator") == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	var result *models.QueryProtectionOptionsResponse
	result, err := h.executorSvc.QueryProtectionOptions(queryProtectionContext(c), tenantIDValue(c), c.Query("target_locator"))
	if err != nil {
		if !writeQueryProtectionError(c, err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, servicei18n.MsgProtectionOptionsUnavailable), "error_code": "query_protection_unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeQueryProtectionError(c *gin.Context, err error) bool {
	if queryExecutionErrorCode(err) == "query_execution_failed" {
		return false
	}
	switch {
	case errors.Is(err, dataprotection.ErrRoleOptionIneligible):
		c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, servicei18n.MsgProtectionRolesUnavailable), "error_code": "query_protection_roles_unavailable"})
	case errors.Is(err, dataprotection.ErrRoleOptionUnavailable):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, servicei18n.MsgProtectionChoiceInvalid), "error_code": "query_protection_choice_invalid"})
	case errors.Is(err, protection.ErrRequired), errors.Is(err, dataprotection.ErrDenied):
		c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, servicei18n.MsgDataProtectionRequired), "error_code": "data_protection_required"})
	default:
		return false
	}
	return true
}
