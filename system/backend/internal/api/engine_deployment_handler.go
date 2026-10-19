package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type EngineDeploymentHandler struct {
	service   *service.EngineDeploymentService
	discovery *service.DeploymentDiscoveryService
}

// Discovery godoc
// @Summary 查看自带部署发现来源 | Read built-in deployment discovery
// @Description 仅读取部署侧有界快照；过期或失败清除候选，不自动登记或推断主机 | Read bounded deployment snapshots; stale or failed sources clear candidates without registration or host inference
// @Tags 引擎部署 | Engine Deployments
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.DeploymentDiscovery
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.engine_deployment.read"]
// @Router /platform/engine_deployments/discovery [get]
func (h *EngineDeploymentHandler) Discovery(c *gin.Context) {
	if _, err := engineDeploymentActor(c); err != nil {
		engineDeploymentError(c, err)
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 {
		engineDeploymentError(c, commonapi.ErrBadRequest)
		return
	}
	value:=h.discovery.Read(c.Request.Context())
	if err:=h.service.MarkRegisteredDiscovery(c.Request.Context(),&value);err!=nil {engineDeploymentError(c,err);return}
	c.JSON(http.StatusOK,value)
}

func engineDeploymentActor(c *gin.Context) (int64, error) {
	actor, err := iamPlatformUserActor(c)
	if err != nil {
		return 0, err
	}
	context, _ := middleware.IAMAuthContextFromGin(c)
	if context.Delegation != nil || context.Token.Type == middleware.IAMTokenTypeDelegatedAccess {
		return 0, commonapi.ErrForbidden
	}
	return actor, nil
}

func engineDeploymentError(c *gin.Context, err error) {
	status, key, code := 500, "failed", "engine_deployment_failed"
	switch {
	case errors.Is(err, commonapi.ErrBadRequest):
		status, key, code = 400, "invalid", "engine_deployment_invalid"
	case errors.Is(err, commonapi.ErrNotFound):
		status, key, code = 404, "not_found", "engine_deployment_not_found"
	case errors.Is(err, commonapi.ErrConflict):
		status, key, code = 409, "version_conflict", "resource_version_conflict"
	case errors.Is(err, repository.ErrDeploymentSourceOccupied):
		status, key, code = 409, "source_occupied", "engine_deployment_source_occupied"
	case errors.Is(err, repository.ErrDeploymentHostUnavailable):
		status, key, code = 409, "host_unavailable", "engine_deployment_host_unavailable"
	case errors.Is(err, service.ErrDeploymentSourceChanged):
		status,key,code=409,"source_changed","engine_deployment_source_changed"
	case errors.Is(err, commonapi.ErrUnauthorized), errors.Is(err, commonapi.ErrForbidden):
		respondIAMError(c, err)
		return
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, "system.engine_deployment."+key), "error_code": code})
}

// List godoc
// @Summary 查询平台引擎部署 | List platform engine deployments
// @Description 只读管理员确认的部署声明；不读取租户引擎或业务数据 | Read confirmed deployment declarations without Tenant engines or business data
// @Tags 引擎部署 | Engine Deployments
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Param search query string false "部署名称片段 | Deployment name fragment"
// @Param node_id query string false "声明的主机 UUID | Declared host UUID"
// @Success 200 {object} models.EngineDeploymentPage
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.engine_deployment.read"]
// @Router /platform/engine_deployments [get]
func (h *EngineDeploymentHandler) List(c *gin.Context) {
	if _, err := engineDeploymentActor(c); err != nil {
		engineDeploymentError(c, err)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.ContentLength != 0 {
		engineDeploymentError(c, commonapi.ErrBadRequest)
		return
	}
	for key, values := range query {
		if (key != "page" && key != "page_size" && key != "search" && key != "node_id") || len(values) != 1 || values[0] == "" {
			engineDeploymentError(c, commonapi.ErrBadRequest)
			return
		}
	}
	page, size := 1, 20
	if query.Has("page") {
		page, err = strconv.Atoi(query.Get("page"))
		if err != nil {
			engineDeploymentError(c, commonapi.ErrBadRequest)
			return
		}
	}
	if query.Has("page_size") {
		size, err = strconv.Atoi(query.Get("page_size"))
		if err != nil {
			engineDeploymentError(c, commonapi.ErrBadRequest)
			return
		}
	}
	value, err := h.service.List(c.Request.Context(), page, size, models.EngineDeploymentFilter{Search: query.Get("search"), NodeID: query.Get("node_id")})
	if err != nil {
		engineDeploymentError(c, err)
		return
	}
	c.JSON(200, value)
}

// Get godoc
// @Summary 查看平台引擎部署 | Read platform engine deployment
// @Description 返回确认快照和当前纳管关联，不代表在线或采集成功 | Return the confirmed snapshot and current management association, not live collection status
// @Tags 引擎部署 | Engine Deployments
// @Produce json
// @Security BearerAuth
// @Param deployment_id path string true "部署 UUID | Deployment UUID"
// @Success 200 {object} models.EngineDeployment
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.engine_deployment.read"]
// @Router /platform/engine_deployments/{deployment_id} [get]
func (h *EngineDeploymentHandler) Get(c *gin.Context) {
	if _, err := engineDeploymentActor(c); err != nil {
		engineDeploymentError(c, err)
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		engineDeploymentError(c, commonapi.ErrBadRequest)
		return
	}
	value, err := h.service.Get(c.Request.Context(), c.Param("deployment_id"))
	if err != nil {
		engineDeploymentError(c, err)
		return
	}
	c.JSON(200, value)
}

// Create godoc
// @Summary 批量确认自带引擎部署 | Confirm built-in engine deployments in a batch
// @Description 管理员确认 PostgreSQL 来源与主机；不创建租户引擎、不探测 SQL 或 Docker | Confirm PostgreSQL source and host without creating Tenant engines or probing SQL or Docker
// @Tags 引擎部署 | Engine Deployments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.EngineDeploymentAdmissionRequest true "至多16个显式来源及主机确认 | Up to 16 explicit source and host confirmations"
// @Success 201 {object} models.EngineDeploymentAdmissionResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.engine_deployment.create", "platform.host_node.read"]
// @Router /platform/engine_deployments [post]
func (h *EngineDeploymentHandler) Create(c *gin.Context) {
	if _, err := engineDeploymentActor(c); err != nil {
		engineDeploymentError(c, err)
		return
	}
	var input models.EngineDeploymentAdmissionRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if c.Request.URL.RawQuery != "" || bindEngineDeployment(c, &input) != nil {
		engineDeploymentError(c, commonapi.ErrBadRequest)
		return
	}
	value, err := h.service.Admit(c.Request.Context(), input, h.discovery.Read(c.Request.Context()), iamAuditMetadataWithStatus(c, 201))
	if err != nil {
		engineDeploymentError(c, err)
		return
	}
	c.JSON(201, value)
}

// Update godoc
// @Summary 更新平台引擎部署声明 | Update platform engine deployment declaration
// @Description 完整替换及版本 CAS，保留原声明有效区间与审计；不执行部署操作 | Full replacement with version CAS and retained declaration intervals and audit, without deployment operations
// @Tags 引擎部署 | Engine Deployments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param deployment_id path string true "部署 UUID | Deployment UUID"
// @Param request body models.EngineDeploymentUpdateRequest true "完整声明及当前版本 | Complete declaration and current version"
// @Success 200 {object} models.EngineDeployment
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.engine_deployment.update", "platform.host_node.read"]
// @Router /platform/engine_deployments/{deployment_id} [put]
func (h *EngineDeploymentHandler) Update(c *gin.Context) {
	if _, err := engineDeploymentActor(c); err != nil {
		engineDeploymentError(c, err)
		return
	}
	var input models.EngineDeploymentUpdateRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if c.Request.URL.RawQuery != "" || bindEngineDeployment(c, &input) != nil {
		engineDeploymentError(c, commonapi.ErrBadRequest)
		return
	}
	value, err := h.service.Update(c.Request.Context(), c.Param("deployment_id"), input, iamAuditMetadata(c))
	if err != nil {
		engineDeploymentError(c, err)
		return
	}
	c.JSON(200, value)
}
