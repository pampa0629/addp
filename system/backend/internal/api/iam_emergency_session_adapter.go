package api

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IAMEmergencySessionCreateRequest struct {
	ID                string `json:"id"`
	TargetPrincipalID string `json:"target_principal_id"`
	TargetFamilyID    string `json:"target_family_id"`
	Reason            string `json:"reason"`
	IncidentReference string `json:"incident_reference"`
}
type IAMEmergencySessionReviewRequest struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}
type IAMEmergencySessionExecuteRequest struct {
	Version     string `json:"version"`
	ExecutionID string `json:"execution_id"`
}
type iamEmergencySessionService interface {
	Create(context.Context, iam.CreateEmergencySessionInput) (*iam.EmergencySessionRequest, error)
	Mutate(context.Context, iam.MutateEmergencySessionInput, string) (*iam.EmergencySessionRequest, error)
	Get(context.Context, string, uuid.UUID) (*iam.EmergencySessionRequest, error)
	List(context.Context, string, int, int) ([]iam.EmergencySessionRequest, int64, error)
}
type IAMEmergencySessionHandler struct{ service iamEmergencySessionService }

func NewIAMEmergencySessionHandler(s iamEmergencySessionService) (*IAMEmergencySessionHandler, error) {
	if s == nil {
		return nil, commonapi.ErrBadRequest
	}
	return &IAMEmergencySessionHandler{service: s}, nil
}
func emergencyUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, commonapi.ErrBadRequest
	}
	return id, nil
}
func respondEmergencyError(c *gin.Context, err error) {
	if errors.Is(err, commonapi.ErrConflict) {
		c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, "system.iam.emergency_session_conflict"), ErrorCode: stringPointerAPI("resource_version_conflict")})
		return
	}
	respondIAMError(c, err)
}
func stringPointerAPI(v string) *string { return &v }

// List godoc
// @Summary 查询应急撤销请求 | List emergency revocation requests
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "页大小 | Page size"
// @Success 200 {object} object{data=[]iam.EmergencySessionRequest,total=int64,page=int,page_size=int,total_pages=int}
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.read"]
// @Router /tenant/emergency_session_requests [get]
// @Router /platform/emergency_session_requests [get]
func (h *IAMEmergencySessionHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, size := commonapi.ParsePagination(c)
	rows, total, err := h.service.List(c.Request.Context(), iamBearerToken(c.GetHeader("Authorization")), page, size)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

// Get godoc
// @Summary 核清应急撤销结果 | Resolve emergency revocation result
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.read"]
// @Router /tenant/emergency_session_requests/{id} [get]
// @Router /platform/emergency_session_requests/{id} [get]
func (h *IAMEmergencySessionHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := emergencyUUID(c.Param("id"))
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	row, err := h.service.Get(c.Request.Context(), iamBearerToken(c.GetHeader("Authorization")), id)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Create godoc
// @Summary 申请单会话应急撤销 | Request emergency single-session revocation
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body IAMEmergencySessionCreateRequest true "请求 | Request"
// @Success 201 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.create"]
// @Router /tenant/emergency_session_requests [post]
func (h *IAMEmergencySessionHandler) Create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var body IAMEmergencySessionCreateRequest
	if err := commonapi.BindOptionalJSONStrict(c, &body); err != nil {
		respondEmergencyError(c, commonapi.ErrBadRequest)
		return
	}
	id, err := emergencyUUID(body.ID)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	target, err := parseIAMDecimalID(body.TargetPrincipalID)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	family, err := parseIAMDecimalID(body.TargetFamilyID)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	row, err := h.service.Create(c.Request.Context(), iam.CreateEmergencySessionInput{ID: id, AccessToken: iamBearerToken(c.GetHeader("Authorization")), TargetPrincipalID: target, TargetFamilyID: family, Reason: body.Reason, IncidentReference: body.IncidentReference, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// Approve godoc
// @Summary 批准 | Approve
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Accept json
// @Param request body IAMEmergencySessionReviewRequest true "请求 | Request"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.approve"]
// @Router /platform/emergency_session_requests/{id}/approve [post]
func (h *IAMEmergencySessionHandler) Approve(c *gin.Context) { h.mutate(c, "approve") }

// Reject godoc
// @Summary 拒绝 | Reject
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Accept json
// @Param request body IAMEmergencySessionReviewRequest true "请求 | Request"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.reject"]
// @Router /platform/emergency_session_requests/{id}/reject [post]
func (h *IAMEmergencySessionHandler) Reject(c *gin.Context) { h.mutate(c, "reject") }

// Revoke godoc
// @Summary 撤回授权 | Revoke grant
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Accept json
// @Param request body IAMEmergencySessionReviewRequest true "请求 | Request"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.revoke"]
// @Router /platform/emergency_session_requests/{id}/revoke [post]
func (h *IAMEmergencySessionHandler) Revoke(c *gin.Context) { h.mutate(c, "revoke") }

// Cancel godoc
// @Summary 取消申请 | Cancel request
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Accept json
// @Param request body IAMEmergencySessionReviewRequest true "请求 | Request"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.cancel"]
// @Router /tenant/emergency_session_requests/{id}/cancel [post]
func (h *IAMEmergencySessionHandler) Cancel(c *gin.Context) { h.mutate(c, "cancel") }

// Execute godoc
// @Summary 单次执行会话撤销 | Execute single-session revocation
// @Tags 紧急会话撤销 | Emergency Session Revocation
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 UUID | Request UUID"
// @Accept json
// @Param request body IAMEmergencySessionExecuteRequest true "请求 | Request"
// @Success 200 {object} iam.EmergencySessionRequest
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.emergency_session_revocation.execute"]
// @Router /tenant/emergency_session_requests/{id}/execute [post]
func (h *IAMEmergencySessionHandler) Execute(c *gin.Context) { h.mutate(c, "execute") }

func (h *IAMEmergencySessionHandler) mutate(c *gin.Context, action string) {
	c.Header("Cache-Control", "no-store")
	id, err := emergencyUUID(c.Param("id"))
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	input := iam.MutateEmergencySessionInput{ID: id, AccessToken: iamBearerToken(c.GetHeader("Authorization")), Audit: iamAuditMetadataWithStatus(c, http.StatusOK)}
	if action == "execute" {
		var body IAMEmergencySessionExecuteRequest
		if err := commonapi.BindOptionalJSONStrict(c, &body); err != nil {
			respondEmergencyError(c, commonapi.ErrBadRequest)
			return
		}
		input.Version, err = parseIAMDecimalID(body.Version)
		if err == nil {
			input.ExecutionID, err = emergencyUUID(body.ExecutionID)
		}
	} else {
		var body IAMEmergencySessionReviewRequest
		if err := commonapi.BindOptionalJSONStrict(c, &body); err != nil {
			respondEmergencyError(c, commonapi.ErrBadRequest)
			return
		}
		input.Version, err = parseIAMDecimalID(body.Version)
		input.Reason = body.Reason
	}
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	row, err := h.service.Mutate(c.Request.Context(), input, action)
	if err != nil {
		respondEmergencyError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
