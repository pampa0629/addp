package api

import (
	"net/http"

	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/gin-gonic/gin"
)

type PlatformCapabilityContextResponse = platform.Context

// PlatformCapabilityContext reads code-published platform semantics only.
// @Summary 获取平台能力语义 | Get platform capability semantics
// @Description 首个能力仅为 Transfer 任务创建；不表示模块可用、有权限或已经执行，不读取租户定义或业务数据。 | The first capability is Transfer task creation; it does not assert availability, permission or execution, and reads no Tenant definitions or business data.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param capability path string true "平台能力标识 | Platform capability identifier" Enums(transfer.task.create)
// @Success 200 {object} PlatformCapabilityContextResponse "平台定义上下文 | Platform definition context"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "未提供此能力 | Capability not provided"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /platform/capabilities/{capability} [get]
func (h *Handler) PlatformCapabilityContext(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		fail(c, repository.ErrInvalid)
		return
	}
	if c.Param("capability") != "transfer.task.create" {
		fail(c, repository.ErrNotFound)
		return
	}
	result, err := platform.TransferContext()
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
