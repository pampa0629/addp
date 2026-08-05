package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// EngineHandler 引擎管理 API 处理器
type EngineHandler struct {
	systemClient *commonClient.SystemServiceClient
}

// NewEngineHandler 创建引擎处理器
func NewEngineHandler(systemClient *commonClient.SystemServiceClient) *EngineHandler {
	return &EngineHandler{
		systemClient: systemClient,
	}
}

// ListEngines 获取查询工作台可用的引擎列表。
// @Summary 获取查询工作台引擎列表 | List engines available to the query workbench
// @Tags Engines
// @Produce json
// @Success 200 {array} models.EngineRuntimeDescriptor "引擎运行时描述列表 | Engine runtime descriptor list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /engines [get]
func (h *EngineHandler) ListEngines(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 从 System 模块获取所有声明 compute.query 能力的引擎。
	engines, err := h.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgEngineListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, service.FilterQueryEngineDescriptors(engines))
}

// ListWorkflowEngines 获取工作流引擎列表
// @Summary 获取支持 workflow 的计算引擎列表 | List workflow-capable compute engines
// @Tags Engines
// @Produce json
// @Success 200 {array} models.EngineRuntimeDescriptor "工作流引擎运行时描述列表 | Workflow engine runtime descriptor list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /workflow-engines [get]
func (h *EngineHandler) ListWorkflowEngines(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 从 System 模块获取所有工作流引擎
	engines, err := h.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgWorkflowListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, filterDevelopEngineDescriptors(engines, func(engine *commonModels.Engine) bool {
		return utils.SupportsComputeEntrypoint(engine, "workflow")
	}))
}

// ListSparkRuntimes 获取 Apache Spark 通用引擎资源列表
// @Summary 获取所有 Apache Spark 通用引擎资源列表 | List all Apache Spark general engine resources
// @Tags Engines
// @Produce json
// @Success 200 {array} models.EngineRuntimeDescriptor "Spark通用引擎描述列表 | Spark general engine descriptor list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read"]
// @Router /spark-runtimes [get]
func (h *EngineHandler) ListSparkRuntimes(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 从 System 模块获取所有 Spark 通用引擎资源
	runtimes, err := h.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgSparkListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, filterDevelopEngineDescriptors(runtimes, func(engine *commonModels.Engine) bool {
		return engine.EngineType == "spark"
	}))
}

func filterDevelopEngineDescriptors(
	descriptors []commonModels.EngineRuntimeDescriptor,
	include func(*commonModels.Engine) bool,
) []commonModels.EngineRuntimeDescriptor {
	filtered := make([]commonModels.EngineRuntimeDescriptor, 0, len(descriptors))
	for index := range descriptors {
		descriptor := &descriptors[index]
		engine := descriptor.AsEngine()
		if descriptor.LifecycleState == commonModels.EngineLifecycleActive && include(engine) {
			filtered = append(filtered, *descriptor)
		}
	}
	return filtered
}
