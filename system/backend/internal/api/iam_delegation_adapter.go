package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	commonapi "github.com/addp/common/api"
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
	now     func() time.Time
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
