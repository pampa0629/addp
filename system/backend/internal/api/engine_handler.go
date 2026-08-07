package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type engineResponse struct {
	models.Engine
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
}

type EngineHandler struct {
	engineService        *service.EngineService
	storageEngineService *service.StorageEngineService
}

func NewEngineHandler(engineService *service.EngineService) *EngineHandler {
	return &EngineHandler{
		engineService:        engineService,
		storageEngineService: service.NewStorageEngineService(),
	}
}

// Create godoc
// @Summary      创建引擎 | Create engine
// @Description  创建新的引擎连接；声明 addp.workflow/v1 的扩展工作流引擎会在保存前探测 /health 和 /api/operators | Create a new engine connection; addp.workflow/v1 workflow extensions are probed before save
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.EngineCreateRequest true "引擎信息 | Engine info"
// @Success      201 {object} models.Engine
// @Failure      400 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.create"]
// @Router       /engines [post]
func (h *EngineHandler) Create(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.probeWorkflowRuntimeBeforeSave(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engine, err := h.engineService.Create(&req, actorID, tenantID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondCreated(c, toEngineResponse(engine))
}

// List godoc
// @Summary      获取引擎列表 | List engines
// @Description  按当前 Tenant、引擎类型、能力分组、来源、内置状态和生命周期过滤，返回完整脱敏数组；System 管理页面在前端分页；User 与 Service Principal 使用同一契约 | Filter by current tenant, engine type, capability group, origin, builtin state, and lifecycle, then return the complete masked array; the System management page paginates client-side; users and service principals share the same contract
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        engine_type query string false "引擎类型 | Engine type"
// @Param        capability_groups query string false "能力分组，逗号分隔：storage,compute | Comma-separated capability groups: storage,compute"
// @Param        engine_origins query string false "引擎来源，逗号分隔：general,extension | Comma-separated engine origins: general,extension"
// @Param        include_builtin query bool false "是否包含内置引擎 | Whether to include builtin engines" default(true)
// @Param        lifecycle_states query string false "生命周期，逗号分隔：active,disabled,deleting | Comma-separated lifecycle states: active,disabled,deleting" default(active)
// @Success      200 {array} models.EngineResponse "完整引擎数组 | Complete engine array"
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.read"]
// @Router       /engines [get]
func (h *EngineHandler) List(c *gin.Context) {
	filter := service.EngineListFilter{
		EngineType:       c.Query("engine_type"),
		CapabilityGroups: splitCommaSeparatedQuery(c.Query("capability_groups")),
		EngineOrigins:    splitCommaSeparatedQuery(c.Query("engine_origins")),
		IncludeBuiltin:   c.DefaultQuery("include_builtin", "true") == "true",
		LifecycleStates:  splitCommaSeparatedQuery(c.Query("lifecycle_states")),
	}

	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engines, err := h.engineService.List(filter, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidEngineLifecycle) {
			commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgEngineLifecycleInvalid))
			return
		}
		commonapi.RespondError(c, 500, err.Error())
		return
	}
	c.JSON(http.StatusOK, toEngineResponses(engines))
}

// ListRuntimeDescriptors godoc
// @Summary      获取引擎运行时描述列表 | List engine runtime descriptors
// @Description  返回同 Tenant 可见的脱敏控制面投影；仅工作流/脚本运行时包含 protocol/host/port，数据引擎不返回 connection_info | Return same-tenant masked control-plane projections; only workflow/script runtimes include protocol/host/port and data engines never expose connection_info
// @Tags         运行时控制面 | Runtime Control Plane
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number" default(1)
// @Param        page_size query int false "每页数量 | Page size" default(10)
// @Param        engine_type query string false "引擎类型 | Engine type"
// @Param        capability_groups query string false "能力分组，逗号分隔：storage,compute | Comma-separated capability groups: storage,compute"
// @Param        engine_origins query string false "引擎来源，逗号分隔：general,extension | Comma-separated engine origins: general,extension"
// @Success      200 {object} object{data=[]models.EngineRuntimeDescriptor,total=int,page=int,page_size=int}
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_descriptor.read"]
// @Router       /runtime/engine-descriptors [get]
func (h *EngineHandler) ListRuntimeDescriptors(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	filter := service.EngineListFilter{
		EngineType:       c.Query("engine_type"),
		CapabilityGroups: splitCommaSeparatedQuery(c.Query("capability_groups")),
		EngineOrigins:    splitCommaSeparatedQuery(c.Query("engine_origins")),
		IncludeBuiltin:   c.DefaultQuery("include_builtin", "true") == "true",
		LifecycleStates:  []string{models.EngineLifecycleActive},
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	descriptors, total, err := h.engineService.ListRuntimeDescriptors(page, pageSize, filter, tenantID)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	commonapi.RespondPaginated(c, descriptors, total, page, pageSize)
}

// GetRuntimeDescriptor godoc
// @Summary      获取引擎运行时描述 | Get engine runtime descriptor
// @Description  返回单个同 Tenant Engine Instance 的脱敏控制面投影，不返回数据引擎明文连接 | Return one same-tenant masked Engine Instance control-plane projection without data-engine plaintext connection details
// @Tags         运行时控制面 | Runtime Control Plane
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Success      200 {object} models.EngineRuntimeDescriptor
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_descriptor.read"]
// @Router       /runtime/engine-descriptors/{id} [get]
func (h *EngineHandler) GetRuntimeDescriptor(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	descriptor, err := h.engineService.GetRuntimeDescriptor(id, tenantID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}
	commonapi.RespondSuccess(c, descriptor)
}

func splitCommaSeparatedQuery(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// GetByID godoc
// @Summary      获取引擎详情 | Get engine detail
// @Description  User 返回脱敏连接信息；具有 system.engine.read 的 Tenant Service Principal 返回同 Tenant 的解密连接信息 | Return masked connection details to users and decrypted same-tenant details to tenant service principals with system.engine.read
// @Tags         引擎管理 | Engine Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Success      200 {object} models.EngineResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.read"]
// @Router       /engines/{id} [get]
func (h *EngineHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	_, tenantID, principalType, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var engine *models.Engine
	if principalType == "service_principal" {
		engine, err = h.engineService.GetForConnection(id, tenantID)
	} else {
		engine, err = h.engineService.GetByID(id, tenantID)
	}
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, toEngineDetailResponse(engine))
}

// Update godoc
// @Summary      更新引擎 | Update engine
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        request body models.EngineUpdateRequest true "引擎更新信息 | Engine update info"
// @Success      200 {object} models.EngineResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.update"]
// @Router       /engines/{id} [put]
func (h *EngineHandler) Update(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.EngineUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engine, err := h.engineService.Update(id, tenantID, &req)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, toEngineResponse(engine))
}

// EnableSpatialWorkspace godoc
// @Summary      启用空间工作区 | Enable spatial workspace
// @Description  通过已绑定的 SuperMap 工作流运行时对 PostgreSQL 引擎执行高危启用动作，并根据 kind 初始化 SuperMap SDX+ for PostGIS 或 SuperMap SDX+ for PostgreSQL 空间工作区。| Trigger the bound SuperMap workflow runtime to initialize the SuperMap SDX+ for PostGIS or SuperMap SDX+ for PostgreSQL workspace selected by kind on a PostgreSQL engine.
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        ecosystem path string true "空间工作区生态 | Spatial workspace ecosystem"
// @Param        kind path string true "空间工作区类型 | Spatial workspace kind"
// @Success      200 {object} models.EngineResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.execute"]
// @Router       /engines/{id}/spatial-workspaces/{ecosystem}/{kind}/enable [post]
func (h *EngineHandler) EnableSpatialWorkspace(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	ecosystem := strings.ToLower(strings.TrimSpace(c.Param("ecosystem")))
	kind := strings.ToLower(strings.TrimSpace(c.Param("kind")))
	if ecosystem == "" || kind == "" {
		commonapi.RespondError(c, http.StatusBadRequest, "空间工作区生态和类型不能为空")
		return
	}

	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engine, err := h.engineService.EnableSpatialWorkspace(c.Request.Context(), id, ecosystem, kind, tenantID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	commonapi.RespondSuccess(c, toEngineResponse(engine))
}

// CreateDeletionAssessment godoc
// @Summary      评估删除引擎的影响 | Assess engine deletion impact
// @Description  在不改变引擎生命周期的前提下，请各 owner 模块扫描任务、服务、运行执行和派生产物影响 | Ask owner modules to scan task, service, running execution, and derived artifact impact without changing the engine lifecycle
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        request body models.EngineDeletionAssessmentRequest false "外部产物策略 | External artifact policy"
// @Success      202 {object} models.EngineDeletionAssessmentResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.delete"]
// @Router       /engines/{id}/deletion-assessments [post]
func (h *EngineHandler) CreateDeletionAssessment(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var req models.EngineDeletionAssessmentRequest
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			commonapi.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	assessmentID, err := h.engineService.CreateDeletionAssessment(id, tenantID, actorID, req.ExternalArtifactPolicy)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, models.EngineDeletionAssessmentResponse{AssessmentID: assessmentID})
}

// GetDeletionAssessment godoc
// @Summary      查询删除引擎影响评估 | Get engine deletion impact assessment
// @Tags         引擎管理 | Engine Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        assessment_id path string true "影响评估ID | Impact assessment ID"
// @Success      200 {object} models.TaskStatusResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.delete"]
// @Router       /engines/{id}/deletion-assessments/{assessment_id} [get]
func (h *EngineHandler) GetDeletionAssessment(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	assessment, err := h.engineService.GetDeletionAssessment(id, tenantID, c.Param("assessment_id"))
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}
	commonapi.RespondSuccess(c, assessment)
}

// Delete godoc
// @Summary      删除引擎 | Delete engine
// @Description  必须引用已完成的影响评估并提交与引擎名称一致的确认文本；确认后引擎进入 deleting 并执行权威复扫 | Requires a completed impact assessment and a confirmation token equal to the engine name; the engine then enters deleting and performs an authoritative rescan
// @Tags         引擎管理 | Engine Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        request body models.EngineDeleteRequest true "评估引用、确认文本与外部产物策略 | Assessment reference, confirmation token, and external artifact policy"
// @Success      202 {object} object{message=string,engine=models.EngineResponse}
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.delete"]
// @Router       /engines/{id} [delete]
func (h *EngineHandler) Delete(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var req models.EngineDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	engine, err := h.engineService.BeginDeletion(id, tenantID, actorID, &req)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": commoni18n.T(c, sysi18n.MsgEngineDeletionStarted),
		"engine":  toEngineResponse(engine),
	})
}

func (h *EngineHandler) respondWithResourceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrResourceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrResourceForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrBuiltinResourceImmutable):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrUnsupportedEngineType):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrSpatialWorkspaceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrEngineIdentityImmutable):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineIdentityImmutable))
	case errors.Is(err, service.ErrEngineDeleting):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineDeleting))
	case errors.Is(err, service.ErrInvalidEngineLifecycle):
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgEngineLifecycleInvalid))
	case errors.Is(err, service.ErrInvalidArtifactPolicy):
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgEngineArtifactPolicyInvalid))
	case errors.Is(err, service.ErrEngineCleanupUnavailable):
		commonapi.RespondError(c, http.StatusServiceUnavailable, commoni18n.T(c, sysi18n.MsgEngineCleanupUnavailable))
	case errors.Is(err, service.ErrDeletionAssessmentInvalid):
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgEngineDeletionAssessmentInvalid))
	case errors.Is(err, service.ErrDeletionConfirmation):
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, sysi18n.MsgEngineDeletionConfirmationInvalid))
	case errors.Is(err, service.ErrDeletionAssessmentPending):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineDeletionAssessmentPending))
	case errors.Is(err, service.ErrDeletionAssessmentExpired):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineDeletionAssessmentExpired))
	case errors.Is(err, service.ErrDeletionImpactChanged):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineDeletionImpactChanged))
	case errors.Is(err, service.ErrDeletionRunningExecutions):
		commonapi.RespondError(c, http.StatusConflict, commoni18n.T(c, sysi18n.MsgEngineDeletionRunningExecutions))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

// TestConnection 测试存储引擎连接（用户手动触发，同步返回结果）
// @Summary      测试已有引擎连接 | Test existing engine connection
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "引擎ID | Engine ID"
// @Param        request body models.EngineUpdateRequest false "临时连接配置 | Temporary connection info"
// @Success      200 {object} models.EngineConnectionTestResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.execute"]
// @Router       /engines/{id}/test [post]
func (h *EngineHandler) TestConnection(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.EngineUpdateRequest
	var override *models.ConnectionInfo
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			commonapi.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		override = req.ConnectionInfo
	}

	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engine, err := h.engineService.BuildConnectionTestEngine(id, tenantID, override)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	probe, err := h.testEngineConnection(engine)
	if err != nil {
		// 更新为offline
		h.engineService.RecordConnectionStatus(id, "offline", err.Error())

		commonapi.RespondSuccess(c, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	// 更新为online
	h.engineService.RecordConnectionStatus(id, "online", "连接正常")

	commonapi.RespondSuccess(c, gin.H{
		"success": true,
		"message": "连接成功",
		"probe":   probe,
	})
}

// TestConnectionBeforeCreate 创建前测试连接
// @Summary      创建前测试连接 | Test connection before create
// @Tags         引擎管理 | Engine Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.EngineCreateRequest true "引擎信息 | Engine info"
// @Success      200 {object} models.EngineConnectionTestResponse
// @Failure      400 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.execute"]
// @Router       /engines/test-connection [post]
func (h *EngineHandler) TestConnectionBeforeCreate(c *gin.Context) {
	var req models.EngineCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 构建临时资源对象用于测试
	engine := &models.Engine{
		EngineType:     req.EngineType,
		EngineOrigin:   req.EngineOrigin,
		Name:           req.Name,
		ConnectionInfo: req.ConnectionInfo,
		Capabilities:   req.Capabilities,
	}

	probe, err := h.testEngineConnection(engine)
	if err != nil {
		commonapi.RespondSuccess(c, gin.H{
			"success": false,
			"message": "连接失败",
			"error":   err.Error(),
		})
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"success": true,
		"message": "连接成功",
		"probe":   probe,
	})
}

func (h *EngineHandler) testEngineConnection(engine *models.Engine) (gin.H, error) {
	if h.storageEngineService.ShouldProbeWorkflowRuntime(engine) {
		operatorCount, err := h.storageEngineService.ProbeWorkflowRuntimeContract(engine)
		if err != nil {
			return nil, err
		}
		return gin.H{
			"runtime_protocol": engineplugin.WorkflowRuntimeAPIAddpV1,
			"operators_count":  operatorCount,
		}, nil
	}
	if err := h.storageEngineService.TestConnection(engine); err != nil {
		return nil, err
	}
	return gin.H{}, nil
}

func (h *EngineHandler) probeWorkflowRuntimeBeforeSave(req *models.EngineCreateRequest) error {
	if req == nil {
		return nil
	}
	engine := &models.Engine{
		EngineType:     req.EngineType,
		EngineOrigin:   req.EngineOrigin,
		Name:           req.Name,
		ConnectionInfo: req.ConnectionInfo,
		Capabilities:   req.Capabilities,
	}
	if !h.storageEngineService.ShouldProbeWorkflowRuntime(engine) {
		return nil
	}
	_, err := h.testEngineConnection(engine)
	return err
}

func toEngineResponses(engines []models.Engine) []engineResponse {
	responses := make([]engineResponse, 0, len(engines))
	for i := range engines {
		responses = append(responses, toEngineResponse(&engines[i]))
	}
	return responses
}

func toEngineResponse(engine *models.Engine) engineResponse {
	engineCopy := *engine
	if engineCopy.Capabilities != nil && *engineCopy.Capabilities != "" {
		engineCopy.CapabilitiesView = service.BuildCapabilitiesView(engineCopy.Capabilities, engineCopy.EngineType)
	}
	response := engineResponse{Engine: engineCopy}
	if engine.Capabilities != nil && *engine.Capabilities != "" {
		response.Capabilities = json.RawMessage(*engine.Capabilities)
	}
	return response
}

func toEngineDetailResponse(engine *models.Engine) engineResponse {
	return toEngineResponse(engine)
}

// ListCatalogChildren 列出指定引擎的实时 catalog 子节点。
// @Summary 列出实时 catalog 子节点 | List live catalog children
// @Description 基于 System 管理的引擎连接信息实时浏览真实引擎 catalog。请求空 path 返回显性结构 root；请求 root path 返回 schema、bucket、database、directory 等第一层业务节点。| Browse live engine catalog using System-managed connection information. Empty path returns the explicit structural root; root path returns first business branches.
// @Tags 引擎管理 | Engine Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎ID | Engine ID"
// @Param request body models.CatalogListChildrenRequest true "Catalog 路径请求 | Catalog path request"
// @Success 200 {object} models.CatalogListChildrenResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.read"]
// @Router /engines/{id}/catalog/children [post]
func (h *EngineHandler) ListCatalogChildren(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.CatalogListChildrenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engine, err := h.engineService.GetForConnection(id, tenantID)
	if err != nil {
		h.respondWithResourceError(c, err)
		return
	}

	nodes, err := h.storageEngineService.ListCatalogChildren(c.Request.Context(), engine, req)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, models.CatalogListChildrenResponse{Nodes: nodes})
}

// RuntimeEngineRegistrationRequest 是内置工作流 Runtime 的平台注册请求。
type RuntimeEngineRegistrationRequest struct {
	EngineType     string                 `json:"engine_type" binding:"required"`
	Name           string                 `json:"name" binding:"required"`
	Description    string                 `json:"description"`
	ConnectionInfo map[string]interface{} `json:"connection_info" binding:"required"`
	Capabilities   *models.JSONString     `json:"capabilities"`
	IsBuiltin      bool                   `json:"is_builtin"` // 是否为内置引擎（对所有租户可见）
}

// RegisterRuntimeEngine godoc
// @Summary      注册内置工作流 Runtime | Register built-in workflow runtime
// @Description  平台 Service Principal 创建或更新与自身身份对应的内置 Runtime，并异步触发连接检查 | A platform service principal creates or updates its owned built-in runtime and asynchronously starts a connection check
// @Tags         运行时注册 | Runtime Registry
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body RuntimeEngineRegistrationRequest true "Runtime 注册信息 | Runtime registration"
// @Success      202 {object} object{success=bool,message=string,engine_id=int,engine_type=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/engines [post]
func (h *EngineHandler) RegisterRuntimeEngine(c *gin.Context) {
	var req RuntimeEngineRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	fmt.Printf("[RegisterEngine] 📥 收到引擎注册请求: type=%s, name=%s\n", req.EngineType, req.Name)

	if err := h.engineService.ValidateSystemEngineRegistration(req.EngineType, req.Capabilities); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 1. 自动填充 host（从请求来源 IP，规范化回环地址）
	clientIP := c.ClientIP()
	// 将 IPv6/IPv4 回环地址统一规范化为 localhost
	if clientIP == "::1" || clientIP == "127.0.0.1" || clientIP == "localhost" {
		req.ConnectionInfo["host"] = "localhost"
	} else {
		req.ConnectionInfo["host"] = clientIP
	}
	fmt.Printf("[RegisterEngine] 🌐 自动填充 host: %s (来源 IP: %s)\n", req.ConnectionInfo["host"], clientIP)

	// 2. 查找是否已存在（engine_type + tenant_id IS NULL 表示平台级引擎）
	existingEngine, err := h.engineService.GetByEngineTypeAndTenant(req.EngineType, nil)

	var engine *models.Engine
	if err != nil {
		// 不存在 - 创建新记录
		fmt.Printf("[RegisterEngine] ➕ 引擎不存在，创建新记录 (is_builtin=%v)\n", req.IsBuiltin)
		newEngine := models.Engine{
			Name:                   req.Name,
			EngineType:             req.EngineType,
			EngineOrigin:           "extension",
			Description:            req.Description,
			ConnectionInfo:         req.ConnectionInfo,
			Capabilities:           req.Capabilities,
			LifecycleState:         models.EngineLifecycleActive,
			ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
			IsBuiltin:              req.IsBuiltin, // 使用请求中的 is_builtin 值
			TenantID:               nil,           // 平台级引擎
			ConnectionStatus:       "unknown",
		}

		if err := h.engineService.CreateEngine(&newEngine); err != nil {
			fmt.Printf("[RegisterEngine] ❌ 创建引擎失败: %v\n", err)
			commonapi.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("创建引擎失败: %v", err))
			return
		}
		fmt.Printf("[RegisterEngine] ✅ 引擎创建成功，ID=%d\n", newEngine.ID)
		engine = &newEngine
	} else {
		// 已存在 - 更新记录
		fmt.Printf("[RegisterEngine] 🔄 引擎已存在 (ID=%d)，更新记录 (is_builtin=%v)\n", existingEngine.ID, req.IsBuiltin)
		existingEngine.Name = req.Name
		existingEngine.Description = req.Description
		existingEngine.ConnectionInfo = req.ConnectionInfo
		existingEngine.Capabilities = req.Capabilities
		existingEngine.IsBuiltin = req.IsBuiltin // 更新 is_builtin 字段

		if err := h.engineService.UpdateEngine(existingEngine); err != nil {
			fmt.Printf("[RegisterEngine] ❌ 更新引擎失败: %v\n", err)
			h.respondWithResourceError(c, err)
			return
		}
		fmt.Printf("[RegisterEngine] ✅ 引擎更新成功\n")
		engine = existingEngine
	}

	// 3. 异步触发连接检查
	fmt.Printf("[RegisterEngine] 🔍 触发异步连接测试...\n")
	if err := h.engineService.AsyncCheckConnection(engine.ID); err != nil {
		// 连接检查失败不影响注册成功，只记录日志
		fmt.Printf("[RegisterEngine] ⚠️  触发连接检查失败: %v\n", err)
	} else {
		fmt.Printf("[RegisterEngine] ✅ 异步连接测试已启动\n")
	}

	c.JSON(http.StatusAccepted, gin.H{
		"success":     true,
		"message":     "引擎注册成功，连接检查已启动",
		"engine_id":   engine.ID,
		"engine_type": req.EngineType,
	})
}
