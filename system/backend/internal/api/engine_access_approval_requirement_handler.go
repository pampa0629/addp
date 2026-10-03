package api

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonauthorization "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type engineAccessApprovalRequirementService interface {
	InitializeApprovalRequirement(context.Context, engineaccess.InitializeApprovalRequirementInput) (*engineaccess.ApprovalRequirementView, error)
	ListApprovalRequirements(context.Context, engineaccess.Actor, int64, int, int) ([]engineaccess.ApprovalRequirementView, int64, error)
	GetApprovalRequirement(context.Context, engineaccess.Actor, int64, uuid.UUID) (*engineaccess.ApprovalRequirementView, error)
	GetHandlingRequirement(context.Context, engineaccess.Actor, engineplugin.EngineCatalogPath) (*engineaccess.HandlingRequirementView, error)
}
type EngineAccessApprovalRequirementHandler struct {
	service engineAccessApprovalRequirementService
}

type InitializeEngineAccessApprovalRequirementRequest struct {
	CatalogPath engineplugin.EngineCatalogPath `json:"catalog_path" binding:"required"`
	Reason      string                         `json:"reason" binding:"required"`
}

type EngineAccessHandlingRequirementRequest struct {
	Version  string                              `json:"version" binding:"required"`
	Segments []engineplugin.EngineCatalogSegment `json:"segments" binding:"required"`
}

// HandlingRequirement godoc
// @Summary 观察精确目标的当前批准要求 | Observe the exact target's current approval requirement
// @Description 只读结构化查询。仅本人办理权限和当前引擎管理委派，不枚举配置、不初始化缺失要求，不授予访问；版本为无损字符串，正式提交仍须核验原预期版本 | Read-only structured query under current human handling permission and engine delegation. No configuration enumeration, initialization or access grant; the version is a lossless string and formal submission rechecks the original expected version
// @Tags 源授权办理 | Source Access Fulfillment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID，唯一来源 | Engine ID, sole source"
// @Param request body EngineAccessHandlingRequirementRequest true "完整结构化叶子路径，含结构根 | Complete structured leaf path including its root"
// @Success 200 {object} engineaccess.HandlingRequirementView "当前批准要求观察，不是凭据 | Current approval requirement observation, not a credential"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_fulfillment.create"]
// @Router /engines/{id}/access_handling_requirement [post]
func (h *EngineAccessApprovalRequirementHandler) HandlingRequirement(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	var request EngineAccessHandlingRequirementRequest
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	path := engineplugin.EngineCatalogPath{EngineID: uint(engineID), Version: request.Version, Segments: request.Segments}
	if _, err := commonauthorization.EncodeSharingTarget(path); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.GetHandlingRequirement(c.Request.Context(), actor, path)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Initialize godoc
// @Summary 初始化源目标 Catalog 批准要求 | Initialize Catalog approval requirement for a source target
// @Description 当前用户需独立初始化权限和有效引擎管理委派；只新建精确目标的 Catalog 要求，不覆盖已有要求，不授予内容访问 | Requires independent initialization permission and current engine management delegation; creates only the exact target's Catalog requirement without replacing existing facts or granting content access
// @Tags 源授权批准要求 | Source Access Approval Requirements
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body InitializeEngineAccessApprovalRequirementRequest true "精确源路径与原因 | Exact source path and reason"
// @Success 201 {object} engineaccess.ApprovalRequirementView "已初始化 | Initialized"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_approval_requirement.initialize"]
// @Router /engines/{id}/access_approval_requirements [post]
func (h *EngineAccessApprovalRequirementHandler) Initialize(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	var request InitializeEngineAccessApprovalRequirementRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.InitializeApprovalRequirement(c.Request.Context(), engineaccess.InitializeApprovalRequirementInput{
		Actor: actor, EngineID: engineID, CatalogPath: request.CatalogPath, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondApprovalRequirementError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// List godoc
// @Summary 查询引擎源授权批准要求 | List source access approval requirements for an engine
// @Description 当前租户及当前管理委派范围内的配置事实，不包含连接信息或内容授权 | Configuration facts in the current tenant and management delegation, without connection information or content grants
// @Tags 源授权批准要求 | Source Access Approval Requirements
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页数量，默认 10，最多 100 | Page size, default 10, maximum 100"
// @Success 200 {object} object{data=[]engineaccess.ApprovalRequirementView,total=int64,page=int,page_size=int,total_pages=int} "批准要求列表 | Approval requirements"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_approval_requirement.read"]
// @Router /engines/{id}/access_approval_requirements [get]
func (h *EngineAccessApprovalRequirementHandler) List(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	page, size := commonapi.ParsePagination(c)
	rows, total, err := h.service.ListApprovalRequirements(c.Request.Context(), actor, engineID, page, size)
	if err != nil {
		respondApprovalRequirementError(c, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

// Get godoc
// @Summary 读取源授权批准要求 | Get a source access approval requirement
// @Description 按当前租户及引擎隔离，并核验当前管理委派；不存在及跨租户均返回 404 | Scoped to current tenant and engine with current delegation checks; missing and cross-tenant IDs return 404
// @Tags 源授权批准要求 | Source Access Approval Requirements
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param requirement_id path string true "批准要求 UUID | Approval requirement UUID"
// @Success 200 {object} engineaccess.ApprovalRequirementView "批准要求 | Approval requirement"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_approval_requirement.read"]
// @Router /engines/{id}/access_approval_requirements/{requirement_id} [get]
func (h *EngineAccessApprovalRequirementHandler) Get(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("requirement_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("requirement_id") {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.GetApprovalRequirement(c.Request.Context(), actor, engineID, id)
	if err != nil {
		respondApprovalRequirementError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func approvalRequirementActor(c *gin.Context) (engineaccess.Actor, int64, bool) {
	if rejectTenantIDQuery(c) {
		return engineaccess.Actor{}, 0, false
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return actor, 0, false
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return actor, 0, false
	}
	return actor, engineID, true
}

func respondApprovalRequirementError(c *gin.Context, err error) {
	if errors.Is(err, engineaccess.ErrApprovalRequirementExists) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, "system.engine_access_approval_requirement.already_initialized"),
			"error_code": "engine_access_approval_requirement_already_initialized"})
		return
	}
	respondIAMError(c, err)
}
