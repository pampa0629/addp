package api

import (
	"errors"
	"net/http"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	commonAuth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

// SourceProtectionOptions returns candidates only for the current User and exact source.
// @Summary 本人导出脱敏候选 | My export masking options
// @Tags 数据保护 | Data protection
// @Produce json
// @Param source_locator query string true "精确源数据项 | Exact source data item"
// @Success 200 {object} client.TransferProtectionOptions
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
// @Router /source-protection-options [get]
func (h *TaskHandler) SourceProtectionOptions(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	values := c.Request.URL.Query()
	if len(values) != 1 || len(values["source_locator"]) != 1 || c.Query("source_locator") == "" || c.Request.ContentLength != 0 {
		commonAPI.BadRequestError(c, i18n.T(c, "transfer.protection.invalid"))
		return
	}
	result, err := h.taskService.SourceProtectionOptions(transferTaskAuthorizationContext(c), commonAuth.GetTenantID(c), c.Query("source_locator"))
	if err != nil {
		writeTransferProtectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CheckSourceProtection validates saved references without granting data access.
// @Summary 核验本人导出脱敏选择 | Check my export masking selection
// @Tags 数据保护 | Data protection
// @Accept json
// @Produce json
// @Param request body client.TransferSourceProtectionSelection true "源与字段选项引用 | Source and field option references"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.execute"]
// @Router /source-protection-checks [post]
func (h *TaskHandler) CheckSourceProtection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req commonClient.TransferSourceProtectionSelection
	if c.Request.URL.RawQuery != "" || commonAPI.BindOptionalJSONStrict(c, &req) != nil || req.SourceLocator == "" || len(req.Choices) == 0 {
		commonAPI.BadRequestError(c, i18n.T(c, "transfer.protection.invalid"))
		return
	}
	if err := h.taskService.CheckSourceProtection(transferTaskAuthorizationContext(c), commonAuth.GetTenantID(c), &req); err != nil {
		writeTransferProtectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true})
}
func writeTransferProtectionError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrForbidden) {
		commonAPI.ForbiddenError(c, i18n.T(c, i18n.MsgForbidden))
		return
	}
	if errors.Is(err, commonAPI.ErrUnauthorized) {
		commonAPI.UnauthorizedError(c, i18n.T(c, i18n.MsgUnauthorized))
		return
	}
	key, code, status := "transfer.protection.unavailable", "transfer_protection_unavailable", http.StatusServiceUnavailable
	if errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		key, code, status = "transfer.protection.ineligible", "transfer_protection_roles_unavailable", http.StatusConflict
	}
	if errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		key, code, status = "transfer.protection.invalid", "transfer_protection_choice_invalid", http.StatusConflict
	}
	c.JSON(status, gin.H{"error": i18n.T(c, key), "error_code": code})
}
