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
// @Description 同一 PG 只读快照选择已激活定义，按确定 generation 查询 FalkorDB 依赖，再从同版冻结内容补全。按能力升序，最多 32 项、128 KiB、5 秒；超限、损坏或图不可用整次失败，不截断、不换源。不证明可执行，不读取业务数据，拒绝 query。 | Selects active definitions in one read-only PG snapshot, expands FalkorDB dependencies at the exact generation, and hydrates the same frozen release. Ordered by capability, bounded to 32 entries, 128 KiB and 5 seconds. Oversize, corruption or graph failure rejects the whole read without truncation or fallback. No execution rights or business facts are asserted; query parameters are rejected.
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

// PlatformCapabilityContext expands the PG-selected active graph dependencies.
// @Summary 获取平台能力语义 | Get platform capability semantics
// @Description PG 选择 active 修订、摘要和 ready generation，FalkorDB 展开同版能力依赖，PG 冻结内容补全；总预算 5 秒。首个能力为 Transfer 创建。没有记录 404，未激活 409，损坏或图读取失败 500；不回退源文件或 PG 图遍历，不证明可执行，不读取业务数据。 | PG selects the active revision, digest and ready generation; FalkorDB expands its dependencies and the same frozen release supplies descriptions, within 5 seconds. The first capability is Transfer creation. Missing records return 404, inactive records 409, corruption or graph read failure 500. No source-file or PG graph-traversal fallback, execution assertion or business data reads.
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
