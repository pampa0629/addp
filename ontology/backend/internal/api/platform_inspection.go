package api

import (
	"net/http"

	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
)

type PlatformDefinitionResponse = service.PlatformDefinition

// PlatformDefinitions lists frozen releases for platform User inspection.
// @Summary 列出平台本体能力 | List platform ontology capabilities
// @Description 仅 Platform User 的只读核实目录；PG 选择同版 active，FalkorDB 展开依赖，最多 32 项、128 KiB、5 秒，超限、损坏或图失败整次拒绝。拒绝 query、委托及机器身份，不授予执行权。 | Read-only inspection for Platform Users. PG selects active releases and FalkorDB expands same-generation dependencies, bounded to 32 items, 128 KiB and 5 seconds; oversize, corruption or graph failure rejects the whole read. Rejects query, delegation and machine identities; grants no execution rights.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.platform_definition.read"]
// @Success 200 {object} PlatformCapabilitiesResponse "已激活能力，允许为空 | Active capabilities, possibly empty"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 413 {object} ErrorResponse "结果超限 | Result too large"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /platform/definitions [get]
func (h *Handler) PlatformDefinitions(c *gin.Context) {
	h.PlatformCapabilities(c)
}

// PlatformDefinition reads context and evidence from the same active PG release.
// @Summary 核实平台本体定义及依据 | Inspect a platform ontology release and evidence
// @Description 仅 Platform User。一次 PG active 读取恢复同一不可变快照的 context 与 review；无仓库文件读取、源文件回退或图查询。只描述选定能力，不证明全模块覆盖或当前可执行。拒绝 query、编辑、委托及机器身份。 | Platform Users only. Restores context and review from one immutable active PG release; no repository reads, source fallback or graph queries. Describes selected capabilities, not full-module coverage or current execution rights. Rejects query, editing, delegation and machine identities.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.platform_definition.read"]
// @Param capability path string true "平台能力标识 | Platform capability identifier"
// @Success 200 {object} PlatformDefinitionResponse "定义及冻结依据 | Definition and frozen evidence"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "未提供此能力 | Capability not provided"
// @Failure 409 {object} ErrorResponse "尚未激活 | Not active"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /platform/definitions/{capability} [get]
func (h *Handler) PlatformDefinition(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || platform.ValidateIdentity(c.Param("capability"), 1) != nil {
		fail(c, repository.ErrInvalid)
		return
	}
	result, err := h.platformDefinitions.PlatformDefinition(c.Request.Context(), c.Param("capability"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
