package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	commonapi "github.com/addp/common/api"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type iamAuditEventWriter interface {
	Write(context.Context, iam.AuditEvent) error
}

type IAMInternalAuditHandler struct {
	writer iamAuditEventWriter
}

func NewIAMInternalAuditHandler(writer iamAuditEventWriter) (*IAMInternalAuditHandler, error) {
	if writer == nil {
		return nil, fmt.Errorf("%w: internal audit writer is required", commonapi.ErrBadRequest)
	}
	return &IAMInternalAuditHandler{writer: writer}, nil
}

// Create godoc
// @Summary      追加内部审计事件 | Append internal audit event
// @Tags         内部接口 | Internal
// @Accept       json
// @Produce      json
// @Param        request body models.AuditLogCreateRequest true "审计事件 | Audit event"
// @Success      201 {object} object{message=string}
// @x-addp-auth-mode "internal"
// @Router       /internal/audit-logs [post]
func (h *IAMInternalAuditHandler) Create(c *gin.Context) {
	var request commonmodels.AuditLogCreateRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid audit event", commonapi.ErrBadRequest))
		return
	}
	event, err := internalAuditEvent(request)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.writer.Write(c.Request.Context(), event); err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "audit event appended"})
}

// CreateService godoc
// @Summary      追加 Tenant 服务审计事件 | Append tenant service audit event
// @Description  Principal、Context 与 Tenant 只从 Service Access Token 获取，请求体中的同名字段不会成为授权事实 | Principal, context, and tenant are derived only from the Service Access Token; matching request fields are not authorization facts
// @Tags         Tenant 审计 | Tenant Audit
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.AuditLogCreateRequest true "审计事件 | Audit event"
// @Success      201 {object} object{message=string}
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.create"]
// @Router       /tenant/audit/events [post]
func (h *IAMInternalAuditHandler) CreateService(c *gin.Context) {
	var request commonmodels.AuditLogCreateRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid audit event", commonapi.ErrBadRequest))
		return
	}
	if err := iamServiceOwnsModule(c, request.ModuleName); err != nil {
		respondIAMError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Context.TenantID == nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	principalID := authContext.Principal.ID
	principalType := authContext.Principal.Type
	contextType := authContext.Context.Type
	tenantID := *authContext.Context.TenantID
	request.PrincipalID = &principalID
	request.PrincipalType = &principalType
	request.ContextType = &contextType
	request.TenantID = &tenantID

	event, err := internalAuditEvent(request)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.writer.Write(c.Request.Context(), event); err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "audit event appended"})
}

func internalAuditEvent(request commonmodels.AuditLogCreateRequest) (iam.AuditEvent, error) {
	if strings.TrimSpace(request.EventName) == "" || strings.TrimSpace(request.ModuleName) == "" ||
		strings.TrimSpace(request.EntityType) == "" || strings.TrimSpace(request.EntityID) == "" {
		return iam.AuditEvent{}, fmt.Errorf("%w: audit event identity is incomplete", commonapi.ErrBadRequest)
	}
	metadata := iam.AuditMetadata{
		HTTPMethod: request.HTTPMethod, ResourcePath: request.ResourcePath, HTTPStatus: request.HTTPStatus,
		RequestID: request.RequestID, IPAddress: request.IPAddress, UserAgent: request.UserAgent,
	}
	if request.PrincipalID != nil {
		principalID, err := parseIAMDecimalID(*request.PrincipalID)
		if err != nil || request.PrincipalType == nil {
			return iam.AuditEvent{}, fmt.Errorf("%w: invalid audit principal", commonapi.ErrBadRequest)
		}
		principalType := iam.PrincipalType(*request.PrincipalType)
		if principalType != iam.PrincipalTypeUser && principalType != iam.PrincipalTypeServicePrincipal {
			return iam.AuditEvent{}, fmt.Errorf("%w: invalid audit principal type", commonapi.ErrBadRequest)
		}
		metadata.PrincipalID, metadata.PrincipalType = &principalID, &principalType
	} else if request.PrincipalType != nil {
		return iam.AuditEvent{}, fmt.Errorf("%w: audit principal type requires principal", commonapi.ErrBadRequest)
	}
	if request.ContextType != nil {
		contextType := iam.ContextType(*request.ContextType)
		if contextType != iam.ContextTypePlatform && contextType != iam.ContextTypeTenant {
			return iam.AuditEvent{}, fmt.Errorf("%w: invalid audit context", commonapi.ErrBadRequest)
		}
		metadata.ContextType = &contextType
	}
	if request.TenantID != nil {
		tenantID, err := parseIAMDecimalID(*request.TenantID)
		if err != nil {
			return iam.AuditEvent{}, err
		}
		metadata.TenantID = &tenantID
	}
	result := iam.AuditResult(request.Result)
	switch result {
	case iam.AuditResultSucceeded, iam.AuditResultFailed, iam.AuditResultDenied, iam.AuditResultIgnored:
	default:
		return iam.AuditEvent{}, fmt.Errorf("%w: invalid audit result", commonapi.ErrBadRequest)
	}
	risk := iam.AuditRiskLevel(request.RiskLevel)
	switch risk {
	case iam.AuditRiskLow, iam.AuditRiskMedium, iam.AuditRiskHigh, iam.AuditRiskCritical:
	default:
		return iam.AuditEvent{}, fmt.Errorf("%w: invalid audit risk", commonapi.ErrBadRequest)
	}
	return iam.AuditEvent{
		Metadata: metadata, EventName: request.EventName, Result: result, RiskLevel: risk,
		ModuleName: request.ModuleName, EntityType: request.EntityType, EntityID: request.EntityID,
		Details: request.Details,
	}, nil
}
