package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type metaCollectionService interface {
	GetMetaCollectionTaskFacts(context.Context, engineaccess.Actor, int64, string) (*execution.MetaScanTaskFacts, error)
	ApproveMetaCollection(context.Context, engineaccess.ApproveMetaCollectionInput) (*engineaccess.MetaCollectionQualification, error)
	RevokeMetaCollection(context.Context, engineaccess.RevokeMetaCollectionInput) (*engineaccess.MetaCollectionQualification, error)
	ListMetaCollections(context.Context, engineaccess.Actor, int64, int, int) ([]engineaccess.MetaCollectionQualification, int64, error)
	CheckMetaCollection(context.Context, engineaccess.Actor, execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error)
}
type MetaCollectionHandler struct{ service metaCollectionService }

func RegisterMetaCollectionRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *MetaCollectionHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("Meta collection dependencies required")
	}
	userTenant := sharedauth.MustNewContextGuard("tenant")
	user := api.Group("/engines/:id")
	user.Use(runtime.Authentication, runtime.UserAccessCredential, userTenant)
	user.GET("/meta_collection_task_facts/:task_id", sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionQualificationRead), h.TaskFacts)
	user.GET("/meta_collection_qualifications", sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionQualificationRead), h.List)
	user.POST("/meta_collection_qualifications", sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionQualificationApprove), h.Approve)
	user.DELETE("/meta_collection_qualifications/:qualification_id", sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionQualificationRevoke), h.Revoke)
	serviceTenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	api.POST("/runtime/meta_collection_checks", runtime.Authentication, runtime.ServiceCredential, serviceTenant, sharedauth.MustNewServiceClientGuard("addp-meta"), sharedauth.MustNewPermissionGuard(authorization.PermissionSystemMetaCollectionQualificationExecute), h.Check)
	return nil
}

type ApproveMetaCollectionRequest struct {
	CommandID                    uuid.UUID `json:"command_id"`
	TaskID                       string    `json:"task_id"`
	ExpectedTaskDefinitionDigest string    `json:"expected_task_definition_digest"`
	Purpose                      string    `json:"purpose"`
	ExpiresAt                    time.Time `json:"expires_at"`
}
type RevokeMetaCollectionRequest struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// TaskFacts godoc
// @Summary 核对待批准的采集任务 | Inspect a collection task before approval
// @Description 当前 Tenant User 需采集资格读取 Permission 与引擎管理资格；通过统一 MetaClient 只读当前任务，不运行扫描或发放内容权限 | Requires current collection-read permission and engine management qualification. Reads current task facts through MetaClient without scanning or granting content access
// @Tags 元数据采集资格 | Metadata Collection Qualifications
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param task_id path string true "任务 ID | Task ID"
// @Success 200 {object} execution.MetaScanTaskFacts
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection_qualification.read"]
// @Router /engines/{id}/meta_collection_task_facts/{task_id} [get]
func (h *MetaCollectionHandler) TaskFacts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.GetMetaCollectionTaskFacts(c.Request.Context(), actor, engineID, c.Param("task_id"))
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// List godoc
// @Summary 查询采集资格与历史 | List collection qualifications and history
// @Description 持久状态不代表当前任务可执行；Worker 仍逐次核验任务、引擎版本、期限和租约 | Persistent state is not an execution Allow; Workers recheck the task, Engine version, expiry and lease
// @Tags 元数据采集资格 | Metadata Collection Qualifications
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(10)
// @Success 200 {object} object{data=[]engineaccess.MetaCollectionQualification,total=int64,page=int,page_size=int,total_pages=int}
// @Failure 400,401,403,404,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection_qualification.read"]
// @Router /engines/{id}/meta_collection_qualifications [get]
func (h *MetaCollectionHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.ContentLength != 0 || c.Request.URL.ForceQuery {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			respondIAMError(c, commonapi.ErrBadRequest)
			return
		}
	}
	page, size := commonapi.ParsePagination(c)
	rows, total, err := h.service.ListMetaCollections(c.Request.Context(), actor, engineID, page, size)
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

// Approve godoc
// @Summary 批准 basic 采集任务 | Approve a basic collection task
// @Description 独立审批 Permission 与当前引擎管理资格取交集。首期仅 PG 整个引擎 basic；System 反查 Meta 当前任务摘要并冻结当前引擎版本，需明确用途和到期时间。重复执行不重新审批、不续期；同命令只恢复原历史，不恢复撤销。既有任务不自动获批，不创建数据 Grant | Requires approval permission and current engine management scope. Initially supports PG entire-engine basic tasks. System reads Meta's current task digest and freezes its own Engine version, purpose and explicit expiry. Repeated execution needs no new approval and never renews expiry; identical commands recover history without restoring revocation. Existing tasks are not auto-approved; no data Grant is issued
// @Tags 元数据采集资格 | Metadata Collection Qualifications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body ApproveMetaCollectionRequest true "任务摘要、用途和期限 | Task digest, purpose and expiry"
// @Success 201 {object} engineaccess.MetaCollectionQualification
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection_qualification.approve"]
// @Router /engines/{id}/meta_collection_qualifications [post]
func (h *MetaCollectionHandler) Approve(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	var request ApproveMetaCollectionRequest
	if bindMetaCollection(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.ApproveMetaCollection(c.Request.Context(), engineaccess.ApproveMetaCollectionInput{Actor: actor, EngineID: engineID, TaskID: request.TaskID, CommandID: request.CommandID, ExpectedTaskDefinitionDigest: request.ExpectedTaskDefinitionDigest, Purpose: request.Purpose, ExpiresAt: request.ExpiresAt, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// Revoke godoc
// @Summary 撤销采集资格 | Revoke a collection qualification
// @Description 当前权限、引擎管理资格及版本均须有效。撤销保留历史，不删除成果、不撤销用户数据 Grant，期限已过记录只读 | Requires current permission, engine management scope and matching version. Keeps history without deleting outputs or revoking user data Grants. Expired records are read-only
// @Tags 元数据采集资格 | Metadata Collection Qualifications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param qualification_id path string true "资格 UUID | Qualification UUID"
// @Param request body RevokeMetaCollectionRequest true "版本和原因 | Version and reason"
// @Success 200 {object} engineaccess.MetaCollectionQualification
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection_qualification.revoke"]
// @Router /engines/{id}/meta_collection_qualifications/{qualification_id} [delete]
func (h *MetaCollectionHandler) Revoke(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("qualification_id"))
	var request RevokeMetaCollectionRequest
	if err != nil || id == uuid.Nil || id.String() != c.Param("qualification_id") || bindMetaCollection(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	version, err := parseIAMDecimalID(request.Version)
	if err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.RevokeMetaCollection(c.Request.Context(), engineaccess.RevokeMetaCollectionInput{Actor: actor, EngineID: engineID, ID: id, Version: version, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Check godoc
// @Summary 核验当前 Meta basic 采集资格 | Check current Meta basic collection access
// @Description 固定 addp-meta Tenant Service；System 独立核验当前机器身份、Common 执行与 lease、Meta 计划和任务摘要及资格期限。200 仅为当次观察，不是可缓存凭据；重试和后续读取重新核验 | Restricted to addp-meta Tenant Service. Independently checks current machine identity, canonical Common execution and lease, Meta plan/task digest and qualification expiry. HTTP 200 is a current observation, never a cacheable credential; retries and subsequent reads recheck
// @Tags 元数据采集资格 | Metadata Collection Qualifications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body execution.MetaCollectionAccessRequest true "执行和租约 | Execution and lease"
// @Success 200 {object} execution.MetaCollectionAccessObservation
// @Failure 400,401,403,404,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.meta_collection_qualification.execute"]
// @Router /runtime/meta_collection_checks [post]
func (h *MetaCollectionHandler) Check(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request execution.MetaCollectionAccessRequest
	if bindMetaCollection(c, &request) != nil || request.Validate() != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	ac, ok := sharedauth.AuthContextFromGin(c)
	if !ok || ac.Context.TenantID == nil || ac.Context.TenantMembershipID == nil {
		respondIAMError(c, commonapi.ErrForbidden)
		return
	}
	var actor engineaccess.Actor
	for i, value := range []string{*ac.Context.TenantID, ac.Principal.ID, *ac.Context.TenantMembershipID, ac.Authorization.AuthorizationVersion} {
		parsed, err := parseIAMDecimalID(value)
		if err != nil {
			respondIAMError(c, commonapi.ErrForbidden)
			return
		}
		*[]*int64{&actor.TenantID, &actor.PrincipalID, &actor.MembershipID, &actor.AuthorizationVersion}[i] = parsed
	}
	actor.TokenExpiresAt = ac.Token.ExpiresAt
	row, err := h.service.CheckMetaCollection(c.Request.Context(), actor, request)
	if err != nil {
		respondMetaCollectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func bindMetaCollection(c *gin.Context, dst any) error {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return commonapi.ErrBadRequest
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil {
		return commonapi.ErrBadRequest
	}
	if _, err := jsonmap.DecodeObjectStrict(raw, 8192, 8); err != nil {
		return commonapi.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
func respondMetaCollectionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, engineaccess.ErrMetaCollectionFactsUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, modulei18n.MsgMetaCollectionUnavailable), "error_code": "meta_collection_facts_unavailable"})
	case errors.Is(err, engineaccess.ErrMetaCollectionChanged):
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgMetaCollectionChanged), "error_code": "meta_collection_configuration_changed"})
	default:
		respondIAMError(c, err)
	}
}
