package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type engineAccessGrantService interface {
	RevokeGrant(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error)
}

type EngineAccessGrantHandler struct{ service engineAccessGrantService }

type RevokeEngineAccessGrantRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// Revoke godoc
// @Summary 撤销指定源数据 Grant | Revoke a specific source-data Grant
// @Description 当前租户用户须同时具备有效引擎管理委派和独立撤销权限；停用引擎仍可撤销。只收回此 Grant，同参重试恢复原撤销记录，不影响其他独立授权 | Current tenant user needs an effective engine management delegation and independent revocation Permission, even for disabled engines. Withdraws only this Grant; identical retries return original history without affecting independent Grants
// @Tags 源数据授权撤销 | Source Data Grant Revocation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request_id path string true "原办理 UUID | Original fulfillment UUID"
// @Param request body RevokeEngineAccessGrantRequest true "撤销原因 | Revocation reason"
// @Success 200 {object} engineaccess.GrantRevocation "不可变撤销事实 | Immutable revocation fact"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.revoke"]
// @Router /engines/{id}/access_grants/{request_id}/revoke [post]
func (h *EngineAccessGrantHandler) Revoke(c *gin.Context) {
	if rejectTenantIDQuery(c) {
		return
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	id, err := uuid.Parse(c.Param("request_id"))
	var request RevokeEngineAccessGrantRequest
	if err != nil || id == uuid.Nil || id.String() != c.Param("request_id") || commonapi.BindOptionalJSONStrict(c, &request) != nil ||
		strings.TrimSpace(request.Reason) == "" || utf8.RuneCountInString(request.Reason) > 2000 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.RevokeGrant(c.Request.Context(), engineaccess.RevokeGrantInput{Actor: actor, EngineID: engineID,
		RequestID: id, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if errors.Is(err, engineaccess.ErrGrantRevocationConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgGrantRevocationConflict), "error_code": "engine_access_grant_revocation_conflict"})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
