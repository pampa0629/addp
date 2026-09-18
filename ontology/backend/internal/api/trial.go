package api

import (
	"context"
	"net/http"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
)

type TrialInput struct {
	State semantic.FactState `json:"state" binding:"required" enums:"known,absent,unknown,invalid"`
	Value any                `json:"value,omitempty"`
}

type TrialRequest struct {
	Revision          uint64                `json:"revision" binding:"required"`
	Generation        string                `json:"generation" binding:"required"`
	ActivationVersion uint64                `json:"activation_version" binding:"required"`
	Inputs            map[string]TrialInput `json:"inputs" binding:"required"`
}

type TrialResponse = service.TrialResult

// Trial evaluates a hypothesis against a pinned active definition.
// @Summary 试算激活本体规则 | Try an active ontology rule
// @Description 仅限用户手工假设输入，不读取或认证业务事实，不持久化结果。最多 16 项输入、96 KiB 请求；版本变化返回 409。 | User hypotheses only, no business facts or persistence. At most 16 inputs and 96 KiB; changed activation returns 409.
// @Tags Ontology
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param rule_id path string true "规则标识 | Rule identifier"
// @Param request body TrialRequest true "激活绑定与假设输入；非 known 的 value 必须省略或为 null | Activation binding and hypotheses; non-known value must be omitted or null"
// @Success 200 {object} TrialResponse "假设性四态结果，不表示业务验证成功 | Hypothetical four-state result, not business verification"
// @Failure 400 {object} ErrorResponse "无效请求 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "本体或规则不存在 | Ontology or rule not found"
// @Failure 409 {object} ErrorResponse "未激活或版本变化 | Inactive or activation changed"
// @Failure 413 {object} ErrorResponse "结果过大 | Result too large"
// @Failure 500 {object} ErrorResponse "试算失败 | Trial failed"
// @Failure 503 {object} ErrorResponse "尚未就绪 | Not ready"
// @Router /ontologies/{ontology_id}/semantic/rules/{rule_id}/trial [post]
func (h *Handler) Trial(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || !identifier.MatchString(c.Param("rule_id")) || c.ContentType() != "application/json" {
		fail(c, repository.ErrInvalid)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 96<<10)
	var req TrialRequest
	if commonapi.BindOptionalJSONStrict(c, &req) != nil || !positive(req.Revision) || !positive(req.ActivationVersion) || !canonicalUUID(req.Generation) || req.Inputs == nil || len(req.Inputs) > 16 {
		fail(c, repository.ErrInvalid)
		return
	}
	facts := make(map[string]semantic.Fact, len(req.Inputs))
	for name, input := range req.Inputs {
		if !identifier.MatchString(name) || (input.State != semantic.Known && input.State != semantic.Absent && input.State != semantic.Unknown && input.State != semantic.Invalid) || (input.State != semantic.Known && input.Value != nil) {
			fail(c, repository.ErrInvalid)
			return
		}
		facts[name] = semantic.Fact{State: input.State, Value: input.Value}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	result, err := h.revisions.Trial(ctx, a, s.OntologyID, c.Param("rule_id"), req.Revision, req.Generation, req.ActivationVersion, facts)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
