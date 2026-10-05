package api

import (
	"context"
	"errors"
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/gin-gonic/gin"
)

// ObservabilityIdentities godoc
// @Summary 读取当前观测身份 | Read current observability identities
// @Description 固定 Monitor Platform Service Client 读取有界节点及有效实例投影；不含端点、元数据、凭据或租户数据 | The fixed Monitor Platform Service Client reads a bounded projection of nodes and valid instances without endpoints, metadata, credentials or tenant data
// @Tags 运行登记 | Runtime Registry
// @Produce json
// @Security BearerAuth
// @Success 200 {object} commonmodels.ObservabilityIdentitySnapshot
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @Failure 504 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.observability_identity.read"]
// @Router /runtime/observability-identities [get]
func (h *ModuleRegistryHandler) ObservabilityIdentities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: commoni18n.T(c, "system.observability_identity.invalid"), ErrorCode: "observability_identity_invalid"})
		return
	}
	var result *commonmodels.ObservabilityIdentitySnapshot
	result, err := h.service.ObservabilityIdentities(c.Request.Context())
	if err != nil {
		status, code, key := http.StatusServiceUnavailable, "observability_identity_unavailable", "system.observability_identity.unavailable"
		if errors.Is(err, repository.ErrObservabilityIdentityBudget) {
			code, key = "observability_identity_budget_exceeded", "system.observability_identity.budget_exceeded"
		} else if errors.Is(err, context.DeadlineExceeded) {
			status, code, key = http.StatusGatewayTimeout, "observability_identity_timeout", "system.observability_identity.timeout"
		}
		c.JSON(status, models.ErrorResponse{Error: commoni18n.T(c, key), ErrorCode: code})
		return
	}
	c.JSON(http.StatusOK, result)
}
