package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
)

type BusinessEvaluationRequest struct {
	Revision          uint64 `json:"revision" binding:"required"`
	Generation        string `json:"generation" binding:"required"`
	ActivationVersion uint64 `json:"activation_version" binding:"required"`
	ObjectID          string `json:"object_id" binding:"required" maxLength:"512"`
}

type BusinessEvaluationResponse = service.BusinessEvaluation

// EvaluateBusiness evaluates a rule against one authorized business record.
// @Summary 求值单条业务记录 | Evaluate a single business record
// @Description 使用已发布规则的版本化服务绑定；保护变换或证据不可确认时返回 unknown，不交付原始业务字段。 | Uses the published rule's versioned service binding; returns unknown when protected transformations or incomplete evidence prevent verification, with no raw business fields.
// @Tags Ontology
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["ontology.semantic.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param rule_id path string true "规则标识 | Rule identifier"
// @Param request body BusinessEvaluationRequest true "激活身份与对象标识 | Active identity and object identifier"
// @Success 200 {object} BusinessEvaluationResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 413 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /ontologies/{ontology_id}/semantic/rules/{rule_id}/evaluate [post]
func (h *Handler) EvaluateBusiness(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, scope, ok := scope(c)
	if !ok {
		return
	}
	ruleID := c.Param("rule_id")
	if c.Request.URL.RawQuery != "" || !identifier.MatchString(ruleID) || c.ContentType() != "application/json" {
		fail(c, repository.ErrInvalid)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	var req BusinessEvaluationRequest
	if commonapi.BindOptionalJSONStrict(c, &req) != nil || !positive(req.Revision) ||
		!positive(req.ActivationVersion) || !canonicalUUID(req.Generation) {
		fail(c, repository.ErrInvalid)
		return
	}
	ctx := c.Request.Context()
	bearer := ""
	facts, _ := commonauth.AuthContextFromGin(c)
	if facts.Delegation != nil {
		binding := authorization.OntologyRuleEvaluation{OntologyID: scope.OntologyID, RuleID: ruleID, Revision: req.Revision,
			Generation: req.Generation, ActivationVersion: req.ActivationVersion, ObjectID: req.ObjectID}
		if binding.Digest() == "" {
			fail(c, repository.ErrInvalid)
			return
		}
		ctx = service.WithOntologyRuleCall(ctx, authorization.OntologyRuleCall{AgentRunID: facts.Delegation.AgentRunID,
			ToolCallID: facts.Delegation.ToolCallID, PrincipalID: facts.Principal.ID, Evaluation: binding})
	} else {
		bearer = commonauth.CanonicalBearerToken(c.GetHeader("Authorization"))
	}
	result, err := h.revisions.EvaluateBusiness(ctx, actor, scope.OntologyID, ruleID, req.Revision,
		req.Generation, req.ActivationVersion, req.ObjectID, bearer)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
