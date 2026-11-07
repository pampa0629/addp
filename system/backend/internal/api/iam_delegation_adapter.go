package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type iamDelegationService interface {
	IssueDelegatedAccessToken(
		context.Context,
		iam.IssueDelegatedAccessTokenInput,
	) (*iam.IssuedDelegatedAccessToken, error)
}

type IAMDelegationHandler struct {
	service iamDelegationService
	consume func(context.Context, int64, authorization.OntologyRuleCall, iam.AuditMetadata) (*authorization.CurrentSubjectRoles, error)
	now     func() time.Time
}

// ConsumeOntologyRuleEvaluation godoc
// @Summary 消费单记录规则取数授权 | Consume a single-record rule read authorization
// @Tags 认证 | Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.OntologyRuleCall true "本次规则求值绑定 | Rule evaluation binding"
// @Success 200 {object} authorization.CurrentSubjectRoles
// @Failure 400,401,403,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.ontology_fact.execute"]
// @Router /runtime/ontology-rule-evaluations/consume [post]
func (h *IAMDelegationHandler) ConsumeOntologyRuleEvaluation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var request authorization.OntologyRuleCall
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMDelegationError(c, commonapi.ErrBadRequest)
		return
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMDelegationError(c, commonapi.ErrForbidden)
		return
	}
	if h.consume == nil {
		respondIAMDelegationError(c, commonapi.ErrInternalServerError)
		return
	}
	result, err := h.consume(c.Request.Context(), int64(tenantID), request, iamAuditMetadataWithStatus(c, http.StatusOK))
	if err != nil {
		respondIAMDelegationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func NewIAMDelegationHandler(
	service iamDelegationService,
	now func() time.Time,
) (*IAMDelegationHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: IAM delegation service is required", commonapi.ErrBadRequest)
	}
	if now == nil {
		now = time.Now
	}
	return &IAMDelegationHandler{service: service, now: now}, nil
}

// CreateDelegation godoc
// @Summary      签发受委托访问令牌 | Issue delegated access token
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMDelegationRequest true "委托边界 | Delegation boundary"
// @Success      201 {object} IAMDelegationResponse
// @x-addp-auth-mode "authenticated"
// @Router       /auth/delegations [post]
func (h *IAMDelegationHandler) CreateDelegation(c *gin.Context) {
	var request IAMDelegationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMDelegationError(c, fmt.Errorf("%w: invalid delegation request", commonapi.ErrBadRequest))
		return
	}
	sourceAccessToken := iamBearerToken(c.GetHeader("Authorization"))
	if sourceAccessToken == "" {
		respondIAMDelegationError(c, commonapi.ErrUnauthorized)
		return
	}
	audit := iamAuditMetadata(c)
	status := http.StatusCreated
	audit.HTTPStatus = &status
	issued, err := h.service.IssueDelegatedAccessToken(c.Request.Context(), iam.IssueDelegatedAccessTokenInput{
		SourceAccessToken: sourceAccessToken,
		Audience:          request.Audience,
		Scopes:            append([]string(nil), request.Scopes...),
		AgentRunID:        request.AgentRunID,
		ToolCallID:        request.ToolCallID,
		RuleEvaluation:    request.RuleEvaluation,
		Audit:             audit,
	})
	if err != nil {
		respondIAMDelegationError(c, err)
		return
	}
	response, err := newIAMDelegationResponse(issued, h.now().UTC())
	if err != nil {
		respondIAMDelegationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, response)
}

func respondIAMDelegationError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	messageID := sysi18n.MsgInternalError
	errorCode := "delegation_internal_error"
	switch {
	case errors.Is(err, commonapi.ErrBadRequest):
		messageID = commoni18n.MsgInvalidParams
		errorCode = "invalid_delegation_request"
	case errors.Is(err, commonapi.ErrUnauthorized):
		messageID = commoni18n.MsgUnauthorized
		errorCode = "authentication_required"
	case errors.Is(err, commonapi.ErrForbidden):
		messageID = commoni18n.MsgForbidden
		errorCode = "permission_denied"
	case errors.Is(err, commonapi.ErrConflict):
		messageID = sysi18n.MsgDelegationConflict
		errorCode = "delegation_conflict"
	}
	c.JSON(status, IAMErrorResponse{
		Error:     commoni18n.T(c, messageID),
		ErrorCode: &errorCode,
	})
}
