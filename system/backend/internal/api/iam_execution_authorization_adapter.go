package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	systemservice "github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IAMIssueExecutionAuthorizationRequest struct {
	Audience    string   `json:"audience"`
	ExecutionID string   `json:"execution_id"`
	EngineIDs   []string `json:"engine_ids"`
	Effects     []string `json:"effects"`
	ExpiresIn   int64    `json:"expires_in"`
}

type IAMIssueExecutionAuthorizationFromExecutionRequest struct {
	ParentExecutionID string   `json:"parent_execution_id"`
	Audience          string   `json:"audience"`
	ExecutionID       string   `json:"execution_id"`
	EngineIDs         []string `json:"engine_ids"`
	Effects           []string `json:"effects"`
	ExpiresIn         int64    `json:"expires_in"`
}

type IAMIssueExecutionAuthorizationFromServiceDefinitionRequest struct {
	ExecutionID       string   `json:"execution_id"`
	EngineIDs         []string `json:"engine_ids"`
	DefinitionID      string   `json:"definition_id"`
	DefinitionVersion string   `json:"definition_version"`
	ExpiresIn         int64    `json:"expires_in"`
}

type IAMExecutionAuthorizationResponse struct {
	ID                         string    `json:"id"`
	ExecutionID                string    `json:"execution_id"`
	Audience                   string    `json:"audience"`
	EngineIDs                  []string  `json:"engine_ids"`
	Effects                    []string  `json:"effects"`
	ExpiresAt                  time.Time `json:"expires_at"`
	ActorPrincipalID           string    `json:"actor_principal_id"`
	TenantID                   string    `json:"tenant_id"`
	TenantMembershipID         string    `json:"tenant_membership_id"`
	IssuedAuthorizationVersion string    `json:"issued_authorization_version"`
	SourceType                 string    `json:"source_type"`
	SourceDefinitionID         *string   `json:"source_definition_id,omitempty"`
	SourceDefinitionVersion    *string   `json:"source_definition_version,omitempty"`
}

type IAMExecutionEngineAccessRequest struct {
	ExecutionID     string   `json:"execution_id"`
	EngineID        string   `json:"engine_id"`
	RequiredEffects []string `json:"required_effects"`
}

type IAMExecutionEngineAccessResponse struct {
	AuthorizationID string         `json:"authorization_id"`
	ExecutionID     string         `json:"execution_id"`
	Audience        string         `json:"audience"`
	EngineID        string         `json:"engine_id"`
	Effects         []string       `json:"effects"`
	ExpiresAt       time.Time      `json:"expires_at"`
	Engine          *models.Engine `json:"engine"`
}

type iamExecutionAuthorizationService interface {
	Issue(context.Context, iam.IssueExecutionAuthorizationInput) (*iam.IssuedExecutionAuthorization, error)
	IssueFromExecution(context.Context, iam.IssueExecutionAuthorizationFromExecutionInput) (*iam.IssuedExecutionAuthorization, error)
	IssueFromServiceDefinition(context.Context, iam.IssueExecutionAuthorizationFromServiceDefinitionInput) (*iam.IssuedExecutionAuthorization, error)
	AuthorizeEngineAccess(context.Context, iam.AuthorizeExecutionEngineAccessInput) (*iam.AuthorizedExecutionEngineAccess, error)
}

// IssueFromServiceDefinition godoc
// @Summary      从已发布服务定义签发只读执行授权 | Issue read-only execution authorization from a published service definition
// @Description  仅 addp-service 可基于当前已发布定义版本为 DuckDB Runtime 签发最长 60 秒的只读授权 | Only addp-service may issue a read-only authorization of at most 60 seconds for the DuckDB Runtime from the current published definition version
// @Tags         Runtime 执行授权 | Runtime Execution Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMIssueExecutionAuthorizationFromServiceDefinitionRequest true "服务定义和授权边界 | Service definition and authorization boundary"
// @Success      201 {object} IAMExecutionAuthorizationResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router       /runtime/execution-authorizations/service-definitions [post]
func (h *IAMExecutionAuthorizationHandler) IssueFromServiceDefinition(c *gin.Context) {
	var request IAMIssueExecutionAuthorizationFromServiceDefinitionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid service definition authorization request", commonapi.ErrBadRequest))
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	engineIDs, err := parseExecutionEngineIDs(request.EngineIDs)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	definitionID, err := parseCanonicalIAMInt64(request.DefinitionID)
	if err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid service definition ID", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		if err == nil {
			err = commonapi.ErrForbidden
		}
		respondExecutionAuthorizationError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.IssueFromServiceDefinition(c.Request.Context(), iam.IssueExecutionAuthorizationFromServiceDefinitionInput{
		ExecutionID: executionID, EngineIDs: engineIDs,
		DefinitionID: definitionID, DefinitionVersion: request.DefinitionVersion,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), ExpiresIn: time.Duration(request.ExpiresIn) * time.Second,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIssuedExecutionAuthorization(issued))
}

type executionEngineResolver interface {
	GetForExecution(id, tenantID uint) (*models.Engine, error)
}

type IAMExecutionAuthorizationHandler struct {
	service iamExecutionAuthorizationService
	engines executionEngineResolver
}

func NewIAMExecutionAuthorizationHandler(
	service iamExecutionAuthorizationService,
	engines executionEngineResolver,
) (*IAMExecutionAuthorizationHandler, error) {
	if service == nil || engines == nil {
		return nil, fmt.Errorf("%w: execution authorization service and engine resolver are required", commonapi.ErrBadRequest)
	}
	return &IAMExecutionAuthorizationHandler{service: service, engines: engines}, nil
}

// Issue godoc
// @Summary      签发执行授权 | Issue execution authorization
// @Description  从当前 Tenant User Access Token 派生绑定唯一执行、引擎和效果的短期授权；效果权限在请求体解析后动态校验 | Derive a short-lived authorization bound to one execution, its engines, and effects from the current tenant user access token; effect permissions are checked after parsing the request
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMIssueExecutionAuthorizationRequest true "执行授权边界 | Execution authorization boundary"
// @Success      201 {object} IAMExecutionAuthorizationResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.create"]
// @x-addp-conditional-permissions ["develop.task.execute","develop.data_read.execute","develop.data_write.execute","develop.data_ddl.execute","develop.data_external_effect.execute","model.materialization.execute","quality.check_task.execute","service.definition.create","service.data_read.execute"]
// @Router       /auth/execution-authorizations [post]
func (h *IAMExecutionAuthorizationHandler) Issue(c *gin.Context) {
	var request IAMIssueExecutionAuthorizationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid execution authorization request", commonapi.ErrBadRequest))
		return
	}
	executionID, engineIDs, expiresIn, err := parseExecutionAuthorizationRequest(request)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	sourceAccessToken := iamBearerToken(c.GetHeader("Authorization"))
	if sourceAccessToken == "" {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	status := http.StatusCreated
	audit := iamAuditMetadataWithStatus(c, status)
	issued, err := h.service.Issue(c.Request.Context(), iam.IssueExecutionAuthorizationInput{
		SourceAccessToken: sourceAccessToken,
		Audience:          request.Audience,
		ExecutionID:       executionID,
		EngineIDs:         engineIDs,
		Effects:           append([]string(nil), request.Effects...),
		ExpiresIn:         expiresIn,
		Audit:             audit,
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(status, mapIssuedExecutionAuthorization(issued))
}

// IssueFromExecution godoc
// @Summary      从父子执行来源签发执行授权 | Issue execution authorization from execution provenance
// @Description  仅匹配 audience 的 Tenant Runtime Service Principal 可基于可验证的 Orchestrator 父 execution 与 owner 子 execution 来源链签发授权 | Only the tenant runtime service principal matching the audience may issue from a verified Orchestrator parent and owner child execution chain
// @Tags         Runtime 执行授权 | Runtime Execution Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMIssueExecutionAuthorizationFromExecutionRequest true "父子执行与授权边界 | Parent/child execution and authorization boundary"
// @Success      201 {object} IAMExecutionAuthorizationResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router       /runtime/execution-authorizations [post]
func (h *IAMExecutionAuthorizationHandler) IssueFromExecution(c *gin.Context) {
	var request IAMIssueExecutionAuthorizationFromExecutionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid execution authorization request", commonapi.ErrBadRequest))
		return
	}
	parentExecutionID, err := parseCanonicalExecutionUUID(request.ParentExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	executionID, engineIDs, expiresIn, err := parseExecutionAuthorizationRequest(IAMIssueExecutionAuthorizationRequest{
		Audience: request.Audience, ExecutionID: request.ExecutionID,
		EngineIDs: request.EngineIDs, Effects: request.Effects, ExpiresIn: request.ExpiresIn,
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		if err == nil {
			err = commonapi.ErrForbidden
		}
		respondExecutionAuthorizationError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.IssueFromExecution(c.Request.Context(), iam.IssueExecutionAuthorizationFromExecutionInput{
		ParentExecutionID: parentExecutionID, Audience: request.Audience,
		ExecutionID: executionID, EngineIDs: engineIDs, Effects: append([]string(nil), request.Effects...),
		ExpiresIn: expiresIn, ServicePrincipalID: int64(principalID),
		ServiceClientID: *authContext.Client.ClientID, TenantID: int64(tenantID),
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIssuedExecutionAuthorization(issued))
}

// AuthorizeEngineAccess godoc
// @Summary      消费执行授权并获取引擎访问 | Consume execution authorization for engine access
// @Description  仅匹配 audience 的 Tenant Runtime Service Principal 可消费执行授权并取得同 Tenant 的明文引擎连接 | Only the tenant runtime service principal matching the audience may consume the authorization and receive same-tenant plaintext engine connection details
// @Tags         Runtime 执行授权 | Runtime Execution Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "执行授权 ID | Execution authorization ID"
// @Param        request body IAMExecutionEngineAccessRequest true "执行和效果边界 | Execution and effect boundary"
// @Success      200 {object} IAMExecutionEngineAccessResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      404 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router       /execution-authorizations/{id}/engine-accesses [post]
func (h *IAMExecutionAuthorizationHandler) AuthorizeEngineAccess(c *gin.Context) {
	authorizationID, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid execution authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMExecutionEngineAccessRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid execution engine access request", commonapi.ErrBadRequest))
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	engineID, err := parseCanonicalIAMInt64(request.EngineID)
	if err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid engine ID", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		if err == nil {
			err = commonapi.ErrForbidden
		}
		respondExecutionAuthorizationError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	authorized, err := h.service.AuthorizeEngineAccess(c.Request.Context(), iam.AuthorizeExecutionEngineAccessInput{
		AuthorizationID: int64(authorizationID), ExecutionID: executionID,
		EngineID: int64(engineID), RequiredEffects: append([]string(nil), request.RequiredEffects...),
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	engine, err := h.engines.GetForExecution(uint(authorized.EngineID), uint(authorized.TenantID))
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMExecutionEngineAccessResponse{
		AuthorizationID: strconv.FormatInt(authorized.AuthorizationID, 10),
		ExecutionID:     authorized.ExecutionID.String(), Audience: authorized.Audience,
		EngineID: strconv.FormatInt(authorized.EngineID, 10),
		Effects:  append([]string(nil), authorized.Effects...), ExpiresAt: authorized.ExpiresAt.UTC(),
		Engine: engine,
	})
}

func parseExecutionAuthorizationRequest(
	request IAMIssueExecutionAuthorizationRequest,
) (uuid.UUID, []int64, time.Duration, error) {
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		return uuid.Nil, nil, 0, err
	}
	engineIDs := make([]int64, 0, len(request.EngineIDs))
	for _, value := range request.EngineIDs {
		engineID, err := parseCanonicalIAMInt64(value)
		if err != nil {
			return uuid.Nil, nil, 0, fmt.Errorf("%w: invalid engine ID", commonapi.ErrBadRequest)
		}
		engineIDs = append(engineIDs, engineID)
	}
	if request.ExpiresIn < 0 || request.ExpiresIn > int64(time.Hour/time.Second) {
		return uuid.Nil, nil, 0, fmt.Errorf("%w: invalid execution authorization expiry", commonapi.ErrBadRequest)
	}
	return executionID, engineIDs, time.Duration(request.ExpiresIn) * time.Second, nil
}

func parseExecutionEngineIDs(values []string) ([]int64, error) {
	engineIDs := make([]int64, 0, len(values))
	for _, value := range values {
		engineID, err := parseCanonicalIAMInt64(value)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid engine ID", commonapi.ErrBadRequest)
		}
		engineIDs = append(engineIDs, engineID)
	}
	return engineIDs, nil
}

func parseCanonicalExecutionUUID(value string) (uuid.UUID, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return uuid.Nil, fmt.Errorf("%w: invalid execution ID", commonapi.ErrBadRequest)
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("%w: invalid execution ID", commonapi.ErrBadRequest)
	}
	return parsed, nil
}

func parseCanonicalIAMInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("invalid canonical IAM ID")
	}
	return parsed, nil
}

func mapIssuedExecutionAuthorization(
	issued *iam.IssuedExecutionAuthorization,
) IAMExecutionAuthorizationResponse {
	engineIDs := make([]string, 0, len(issued.EngineIDs))
	for _, engineID := range issued.EngineIDs {
		engineIDs = append(engineIDs, strconv.FormatInt(engineID, 10))
	}
	response := IAMExecutionAuthorizationResponse{
		ID: strconv.FormatInt(issued.ID, 10), ExecutionID: issued.ExecutionID.String(),
		Audience: issued.Audience, EngineIDs: engineIDs, Effects: append([]string(nil), issued.Effects...),
		ExpiresAt: issued.ExpiresAt.UTC(), ActorPrincipalID: strconv.FormatInt(issued.ActorPrincipalID, 10),
		TenantID:                   strconv.FormatInt(issued.TenantID, 10),
		TenantMembershipID:         strconv.FormatInt(issued.TenantMembershipID, 10),
		IssuedAuthorizationVersion: strconv.FormatInt(issued.IssuedAuthorizationVersion, 10),
		SourceType:                 issued.SourceType,
	}
	if issued.SourceDefinitionID != nil {
		value := strconv.FormatInt(*issued.SourceDefinitionID, 10)
		response.SourceDefinitionID = &value
	}
	if issued.SourceDefinitionVersion != nil {
		value := *issued.SourceDefinitionVersion
		response.SourceDefinitionVersion = &value
	}
	return response
}

func respondExecutionAuthorizationError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	messageID := sysi18n.MsgInternalError
	errorCode := "execution_authorization_internal_error"
	switch {
	case errors.Is(err, iam.ErrExecutionAuthorizationConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgExecutionAuthorizationConflict
		errorCode = "execution_authorization_conflict"
	case errors.Is(err, iam.ErrExecutionAuthorizationUnavailable):
		status = http.StatusForbidden
		messageID = sysi18n.MsgExecutionAuthorizationUnavailable
		errorCode = "execution_authorization_unavailable"
	case errors.Is(err, commonapi.ErrBadRequest):
		messageID = commoni18n.MsgInvalidParams
		errorCode = "invalid_execution_authorization_request"
	case errors.Is(err, commonapi.ErrUnauthorized):
		messageID = commoni18n.MsgUnauthorized
		errorCode = "authentication_required"
	case errors.Is(err, commonapi.ErrForbidden):
		messageID = commoni18n.MsgForbidden
		errorCode = "permission_denied"
	case errors.Is(err, commonapi.ErrNotFound):
		messageID = sysi18n.MsgExecutionAuthorizationUnavailable
		errorCode = "execution_authorization_not_found"
	case errors.Is(err, systemservice.ErrResourceNotFound):
		status = http.StatusNotFound
		messageID = sysi18n.MsgExecutionAuthorizationUnavailable
		errorCode = "engine_not_found"
	case errors.Is(err, systemservice.ErrResourceForbidden):
		status = http.StatusForbidden
		messageID = sysi18n.MsgExecutionAuthorizationUnavailable
		errorCode = "engine_unavailable"
	}
	c.JSON(status, IAMErrorResponse{Error: commoni18n.T(c, messageID), ErrorCode: &errorCode})
}
