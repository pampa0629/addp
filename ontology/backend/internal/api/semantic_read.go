package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ClassDirectoryResponse = service.ClassDirectory
type ClassContextResponse = service.SemanticContext

// ListClasses reads only the activated native definition.
// @Summary 列出激活本体的类 | List classes of the active ontology
// @Description 只读定义，不返回草稿或业务实例。ontology_id 必须由用户明确指定。 | Definitions only, no drafts or business instances. The user must identify ontology_id.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Success 200 {object} ClassDirectoryResponse "类与读取时激活版本 | Classes and activation binding at read time"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "不存在 | Not found"
// @Failure 409 {object} ErrorResponse "未激活 | Not active"
// @Failure 413 {object} ErrorResponse "结果过大 | Result too large"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /ontologies/{ontology_id}/semantic/classes [get]
func (h *Handler) ListClasses(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		fail(c, repository.ErrInvalid)
		return
	}
	result, err := h.revisions.ListClasses(c.Request.Context(), a, s.OntologyID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ClassContext reads one class under an exact activation binding.
// @Summary 获取确定类的语义上下文 | Get a pinned class context
// @Description revision、generation、activation_version 必须来自类目录；变化时返回 409，不回退旧版本。只解释原生定义，不执行规则或验证业务数据。 | Use the class directory binding; changed activation returns 409, without historical fallback. Native definitions only; no rule execution or business-data verification.
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param class_id path string true "类标识 | Class identifier"
// @Param revision query int true "目录返回的修订 | Revision returned by directory"
// @Param generation query string true "目录返回的投影 | Generation returned by directory"
// @Param activation_version query int true "目录返回的激活版本 | Activation version returned by directory"
// @Success 200 {object} ClassContextResponse "确定类上下文 | Pinned class context"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "不存在 | Not found"
// @Failure 409 {object} ErrorResponse "未激活或版本变化 | Inactive or activation changed"
// @Failure 413 {object} ErrorResponse "结果过大 | Result too large"
// @Failure 500 {object} ErrorResponse "读取失败 | Read failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /ontologies/{ontology_id}/semantic/classes/{class_id} [get]
func (h *Handler) ClassContext(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	// ParseQuery must reject malformed escapes instead of silently dropping fields.
	q, err := parseSemanticQuery(c.Request.URL.RawQuery)
	if err != nil || !identifier.MatchString(c.Param("class_id")) {
		fail(c, repository.ErrInvalid)
		return
	}
	revision, _ := strconv.ParseUint(q.Get("revision"), 10, 64)
	activation, _ := strconv.ParseUint(q.Get("activation_version"), 10, 64)
	result, err := h.revisions.ClassContext(c.Request.Context(), a, s.OntologyID, c.Param("class_id"), revision, q.Get("generation"), activation)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func parseSemanticQuery(raw string) (url.Values, error) {
	q, err := url.ParseQuery(raw)
	if err != nil || len(q) != 3 {
		return nil, repository.ErrInvalid
	}
	for key, values := range q {
		if len(values) != 1 {
			return nil, repository.ErrInvalid
		}
		switch key {
		case "revision", "activation_version":
			n, err := strconv.ParseUint(values[0], 10, 64)
			if err != nil || !positive(n) || strconv.FormatUint(n, 10) != values[0] {
				return nil, repository.ErrInvalid
			}
		case "generation":
			if !canonicalUUID(values[0]) {
				return nil, repository.ErrInvalid
			}
		default:
			return nil, repository.ErrInvalid
		}
	}
	return q, nil
}
