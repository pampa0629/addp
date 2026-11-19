package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/addp/common/logger"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/infraconfig"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type InfraConfigurationHandler struct {
	service *infraconfig.Configuration
	audit   middleware.RequestAuditWriter
}

type InfraParameterApplyRequest struct {
	Value string `json:"value" binding:"required" maxLength:"32"`
}

// @Summary 查看基础设施运行配置 | Read infrastructure runtime configuration
// @Tags 基础设施配置 | Infrastructure Configuration
// @Produce json
// @Security BearerAuth
// @Success 200 {array} infraconfig.Component "组件配置观测 | Component settings observations"
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.infra_configuration.read"]
// @Router /platform/infra_configuration [get]
func (h *InfraConfigurationHandler) Read(c *gin.Context) {
	if _, err := iamPlatformUserActor(c); err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.service.Read(c.Request.Context()))
}

// @Summary 应用一个基础设施参数 | Apply one infrastructure parameter
// @Description 返回应用、持久化与读回结果 | Return application, persistence, and verification results
// @Tags 基础设施配置 | Infrastructure Configuration
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param component path string true "部署角色 | Deployment role"
// @Param parameter path string true "参数名称 | Parameter name"
// @Param request body InfraParameterApplyRequest true "原生单位中的值 | Value in the native unit"
// @Success 200 {object} infraconfig.Outcome "操作结果 | Operation outcome"
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @Failure 404 {object} IAMErrorResponse
// @Failure 409 {object} IAMErrorResponse
// @Failure 500 {object} IAMErrorResponse
// @Failure 503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.infra_configuration.read","system.infra_configuration.update"]
// @Router /platform/infra_configuration/{component}/parameters/{parameter}/apply [post]
func (h *InfraConfigurationHandler) Apply(c *gin.Context) {
	actor, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request InfraParameterApplyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF || request.Value == "" || len(request.Value) > 32 {
		infraConfigurationError(c, infraconfig.ErrInvalid)
		return
	}

	component, parameter := c.Param("component"), c.Param("parameter")
	principalType, contextType := iam.PrincipalType("user"), iam.ContextType("platform")
	requestID := c.GetString("request_id")
	event := iam.AuditEvent{
		Metadata: iam.AuditMetadata{PrincipalID: &actor, PrincipalType: &principalType, ContextType: &contextType,
			RequestID: &requestID},
		EventName: "system.infra_configuration.apply_requested", Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskMedium,
		ModuleName: "system", EntityType: "infra_parameter", EntityID: component + "/" + parameter,
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	outcome, err := h.service.Apply(ctx, component, parameter, request.Value, func(endpoint, previous string) error {
		event.Details = map[string]any{"endpoint": endpoint, "previous_value": previous, "requested_value": request.Value}
		return h.audit.Write(ctx, event)
	})
	if err != nil {
		infraConfigurationError(c, err)
		return
	}

	status, method, path := http.StatusOK, c.Request.Method, c.Request.URL.Path
	event.Metadata.HTTPStatus = &status
	event.Metadata.HTTPMethod = &method
	event.Metadata.ResourcePath = &path
	event.EventName = "system.infra_configuration.apply_completed"
	if outcome.FailureStage != "" {
		event.Result = iam.AuditResultFailed
	}
	event.Details["application"] = outcome.Application
	event.Details["persistence"] = outcome.Persistence
	event.Details["failure_stage"] = outcome.FailureStage
	if outcome.Current != nil {
		event.Details["observed_value"] = outcome.Current.Value
	}
	auditContext, cancelAudit := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
	defer cancelAudit()
	outcome.AuditRecorded = h.audit.Write(auditContext, event) == nil
	if !outcome.AuditRecorded {
		logger.L().Error("infra_configuration_completion_audit_failed", "request_id", requestID, "parameter", event.EntityID)
	}
	c.JSON(status, outcome)
}

func infraConfigurationError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "audit_failed"
	switch {
	case errors.Is(err, infraconfig.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_value"
	case errors.Is(err, infraconfig.ErrNotFound):
		status, code = http.StatusNotFound, "unknown_parameter"
	case errors.Is(err, infraconfig.ErrReadOnly):
		status, code = http.StatusConflict, "read_only"
	case errors.Is(err, infraconfig.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "unavailable"
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, "system.infra_configuration."+code), "error_code": "infra_configuration_" + code})
}
