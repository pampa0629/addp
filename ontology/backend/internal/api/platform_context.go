package api

import (
	"net/http"

	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/gin-gonic/gin"
)

type PlatformCapabilityContextResponse = platform.Context

type PlatformCapabilitiesResponse = platform.Catalog

// PlatformCapabilities lists active definitions for semantic operation selection.
// @Summary 获取平台能力目录 | List platform capability semantics
// @Description 同一 PG 只读快照中核验已激活定义，按能力身份升序。最多 32 项和 128 KiB，超限或损坏整次失败，不截断、不换源；不证明可执行，不读取业务数据，拒绝 query。 | Verifies active definitions in one read-only PG snapshot, ordered by capability. At most 32 entries and 128 KiB; oversized or corrupt results fail as a whole without truncation or source fallback. No execution permission or business facts are asserted; query parameters are rejected.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Success 200 {object} PlatformCapabilitiesResponse "平台能力目录，允许为空 | Platform capability catalog, possibly empty"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 413 {object} ErrorResponse "结果超限 | Result too large"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /platform/capabilities [get]
func (h *Handler) PlatformCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" {
		fail(c, repository.ErrInvalid)
		return
	}
	result, err := h.platformDefinitions.PlatformCapabilities(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// PlatformCapabilityContext reads the current active platform PG definition.
// @Summary 获取平台能力语义 | Get platform capability semantics
// @Description 从 PG 当前 active 平台修订读取；首个能力为 Transfer 任务创建。没有记录返回 404，尚未激活返回 409，损坏返回 500；不回退源文件，不表示模块可用、用户可执行或已经执行，不读取租户定义或业务数据。 | Reads the current active platform PG revision; the first capability is Transfer task creation. Missing records return 404, inactive records 409, and corruption 500. There is no source-file fallback or assertion of availability, execution permission or completed execution; no Tenant definitions or business data are read.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param capability path string true "平台能力标识，例如 transfer.task.create | Platform capability identifier, e.g. transfer.task.create"
// @Success 200 {object} PlatformCapabilityContextResponse "平台定义上下文 | Platform definition context"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "未提供此能力 | Capability not provided"
// @Failure 409 {object} ErrorResponse "平台能力尚未激活 | Platform capability not active"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /platform/capabilities/{capability} [get]
func (h *Handler) PlatformCapabilityContext(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" {
		fail(c, repository.ErrInvalid)
		return
	}
	if platform.ValidateIdentity(c.Param("capability"), 1) != nil {
		fail(c, repository.ErrInvalid)
		return
	}
	result, err := h.platformDefinitions.PlatformCapabilityContext(c.Request.Context(), c.Param("capability"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
