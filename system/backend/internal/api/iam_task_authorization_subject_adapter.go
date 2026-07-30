package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IAMTaskAuthorizationSubjectRequest struct {
	OwnerModule    string `json:"owner_module"`
	TaskType       string `json:"task_type"`
	TaskRef        string `json:"task_ref"`
	DefinitionHash string `json:"definition_hash"`
}

type IAMTaskAuthorizationSubjectResponse struct {
	ID                   string    `json:"id"`
	OwnerModule          string    `json:"owner_module"`
	TaskType             string    `json:"task_type"`
	TaskRef              string    `json:"task_ref"`
	DefinitionHash       string    `json:"definition_hash"`
	TenantID             string    `json:"tenant_id"`
	PrincipalID          string    `json:"principal_id"`
	TenantMembershipID   string    `json:"tenant_membership_id"`
	AuthorizationVersion string    `json:"authorization_version"`
	AuthorizedAt         time.Time `json:"authorized_at"`
}

type taskAuthorizationSubjectService interface {
	Authorize(context.Context, iam.AuthorizeTaskSubjectInput) (*iam.ResolvedTaskAuthorizationSubject, error)
	Resolve(context.Context, iam.ResolveTaskSubjectInput) (*iam.ResolvedTaskAuthorizationSubject, error)
}

type IAMTaskAuthorizationSubjectHandler struct {
	service taskAuthorizationSubjectService
}

func NewIAMTaskAuthorizationSubjectHandler(service taskAuthorizationSubjectService) (*IAMTaskAuthorizationSubjectHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: task authorization subject service is required", commonapi.ErrBadRequest)
	}
	return &IAMTaskAuthorizationSubjectHandler{service: service}, nil
}

// Authorize godoc
// @Summary      授权持久任务主体 | Authorize a persisted task subject
// @Description  从当前 Tenant User AuthContext 为一个版本化任务定义创建或替换任务授权主体，不保存 Access Token | Create or replace a task authorization subject for a versioned task definition from the current tenant user context without storing an access token
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMTaskAuthorizationSubjectRequest true "任务定义身份 | Task definition identity"
// @Success      200 {object} IAMTaskAuthorizationSubjectResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["orchestrator.workflow.execute"]
// @Router       /auth/task-authorization-subjects [post]
func (h *IAMTaskAuthorizationSubjectHandler) Authorize(c *gin.Context) {
	request, taskRef, ok := bindTaskAuthorizationSubjectRequest(c)
	if !ok {
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeUser) {
		if err == nil {
			err = commonapi.ErrForbidden
		}
		respondExecutionAuthorizationError(c, err)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Context.TenantMembershipID == nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	membershipID, err := parseCanonicalIAMInt64(*authContext.Context.TenantMembershipID)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	authorizationVersion, err := parseCanonicalIAMInt64(authContext.Authorization.AuthorizationVersion)
	if err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrUnauthorized)
		return
	}
	result, err := h.service.Authorize(c.Request.Context(), iam.AuthorizeTaskSubjectInput{
		OwnerModule: request.OwnerModule, TaskType: request.TaskType, TaskRef: taskRef,
		DefinitionHash: request.DefinitionHash, TenantID: int64(tenantID),
		PrincipalID: int64(principalID), TenantMembershipID: membershipID,
		AuthorizationVersion: authorizationVersion,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapTaskAuthorizationSubject(result))
}

// Resolve godoc
// @Summary      解析定时任务授权主体 | Resolve a scheduled task authorization subject
// @Description  仅 addp-orchestrator Tenant Service Principal 可解析与当前定义哈希匹配且仍有效的任务授权主体 | Only the addp-orchestrator tenant service principal may resolve a still-valid task subject matching the current definition hash
// @Tags         Runtime 执行授权 | Runtime Execution Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "任务授权主体 ID | Task authorization subject ID"
// @Param        request body IAMTaskAuthorizationSubjectRequest true "任务定义身份 | Task definition identity"
// @Success      200 {object} IAMTaskAuthorizationSubjectResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.task_authorization.execute"]
// @Router       /runtime/task-authorization-subjects/{id}/resolve [post]
func (h *IAMTaskAuthorizationSubjectHandler) Resolve(c *gin.Context) {
	subjectID, err := parseCanonicalIAMInt64(c.Param("id"))
	if err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid task authorization subject ID", commonapi.ErrBadRequest))
		return
	}
	request, taskRef, ok := bindTaskAuthorizationSubjectRequest(c)
	if !ok {
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
	result, err := h.service.Resolve(c.Request.Context(), iam.ResolveTaskSubjectInput{
		SubjectID: subjectID, OwnerModule: request.OwnerModule, TaskType: request.TaskType,
		TaskRef: taskRef, DefinitionHash: request.DefinitionHash, TenantID: int64(tenantID),
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapTaskAuthorizationSubject(result))
}

func bindTaskAuthorizationSubjectRequest(c *gin.Context) (IAMTaskAuthorizationSubjectRequest, uuid.UUID, bool) {
	var request IAMTaskAuthorizationSubjectRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid task authorization subject request", commonapi.ErrBadRequest))
		return request, uuid.Nil, false
	}
	taskRef, err := uuid.Parse(request.TaskRef)
	if err != nil || taskRef == uuid.Nil || taskRef.String() != request.TaskRef {
		respondExecutionAuthorizationError(c, fmt.Errorf("%w: invalid task authorization task_ref", commonapi.ErrBadRequest))
		return request, uuid.Nil, false
	}
	return request, taskRef, true
}

func mapTaskAuthorizationSubject(subject *iam.ResolvedTaskAuthorizationSubject) IAMTaskAuthorizationSubjectResponse {
	return IAMTaskAuthorizationSubjectResponse{
		ID: strconv.FormatInt(subject.ID, 10), OwnerModule: subject.OwnerModule,
		TaskType: subject.TaskType, TaskRef: subject.TaskRef.String(), DefinitionHash: subject.DefinitionHash,
		TenantID: strconv.FormatInt(subject.TenantID, 10), PrincipalID: strconv.FormatInt(subject.PrincipalID, 10),
		TenantMembershipID: strconv.FormatInt(subject.TenantMembershipID, 10),
		AuthorizationVersion: strconv.FormatInt(subject.AuthorizationVersion, 10),
		AuthorizedAt: subject.AuthorizedAt.UTC(),
	}
}
