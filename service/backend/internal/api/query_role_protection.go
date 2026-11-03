package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
// @Description 当前第一方用户的原生表或可证明单表直接字段 SQL 候选；SQL 只准备不执行，沿 Service 独立投影核验，不接受角色或算法参数 | Native-table or provable direct-field single-table SQL methods for the current first-party User; SQL is prepared without execution against the Service projection; caller roles and algorithm parameters are not accepted
// @Tags QueryService
// @Accept json
// @Produce json
// @Param request body models.QueryProtectionOptionsRequest true "原生表或可证明的单表 SQL 来源 | Native table or provable single-table SQL source"
// @Success 200 {object} models.QueryProtectionOptionsResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /query-protection-options [post]
// @Security BearerAuth
func (h *QueryServiceHandler) QueryProtectionOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request models.QueryProtectionOptionsRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if len(c.Request.URL.Query()) != 0 || decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || !request.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidQueryRequest), "error_code": "invalid_query_request"})
		return
	}
	var result *models.QueryProtectionOptionsResponse
	result, err := h.executorSvc.QueryProtectionOptions(queryProtectionContext(c), tenantIDValue(c), &request)
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
