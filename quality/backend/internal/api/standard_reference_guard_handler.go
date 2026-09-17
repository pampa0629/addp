package api

import (
	commonclient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type StandardReferenceGuardHandler struct {
	service *service.StandardReferenceGuardService
}

func NewStandardReferenceGuardHandler(s *service.StandardReferenceGuardService) *StandardReferenceGuardHandler {
	return &StandardReferenceGuardHandler{service: s}
}

// @Summary 设置 Quality 标准引用屏障 | Set Quality Standard reference guard
// @Tags StandardReference
// @Accept json
// @Produce json
// @Param resource_type path string true "标准资源类型 | Standard resource type" Enums(domain)
// @Param resource_id path int true "标准资源 ID | Standard resource ID"
// @Param request body map[string]string true "屏障状态 | Guard state"
// @Success 200 {object} commonclient.StandardReferenceGuardResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 403 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.standard_reference.update"]
// @Router /standard-reference-guards/{resource_type}/{resource_id} [put]
// @Security BearerAuth
func (h *StandardReferenceGuardHandler) SetState(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("resource_id"), 10, 64)
	if err != nil || id <= 0 || c.Param("resource_type") != "domain" {
		respondInvalidRequest(c, "")
		return
	}
	var request struct {
		State string `json:"state" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		respondInvalidRequest(c, "")
		return
	}
	var result *commonclient.StandardReferenceGuardResponse
	result, err = h.service.SetState(int64(commonAuth.GetTenantID(c)), id, request.State)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}
