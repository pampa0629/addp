package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	transferi18n "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

type FieldDefinitionRecommendationHandler struct {
	service *service.FieldDefinitionRecommendationService
}

func NewFieldDefinitionRecommendationHandler(recommendationService *service.FieldDefinitionRecommendationService) *FieldDefinitionRecommendationHandler {
	return &FieldDefinitionRecommendationHandler{service: recommendationService}
}

// Create analyzes exact source values and recommends target field definitions.
// @Summary 推荐目标字段定义 | Recommend target field definitions
// @Description 全量扫描指定源 DECIMAL 字段的实际值，为新建 MySQL 目标表返回不会截断当前源数据的最小 precision 和 scale。| Fully scans the selected source DECIMAL fields and returns the minimum precision and scale that preserve all current source values in a new MySQL target table.
// @Tags 字段定义 | Field Definitions
// @Accept json
// @Produce json
// @Param request body service.FieldDefinitionRecommendationRequest true "推荐请求 | Recommendation request"
// @Success 200 {object} service.FieldDefinitionRecommendationResult "推荐结果 | Recommendation result"
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 422 {object} map[string]string "源或目标不支持分析 | Source or target does not support analysis"
// @Failure 503 {object} map[string]string "分析服务不可用 | Analysis unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.create"]
// @Router /field-definition-recommendations [post]
// @Security BearerAuth
func (h *FieldDefinitionRecommendationHandler) Create(c *gin.Context) {
	var request service.FieldDefinitionRecommendationRequest
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, transferi18n.MsgFieldRecommendationUnavailable)})
		return
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, transferi18n.MsgFieldRecommendationInvalid)})
		return
	}
	result, err := h.service.Recommend(c.Request.Context(), commonAuth.GetTenantID(c), request)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFieldRecommendationInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, transferi18n.MsgFieldRecommendationInvalid)})
		case errors.Is(err, service.ErrFieldRecommendationUnsupported):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, transferi18n.MsgFieldRecommendationUnsupported)})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, transferi18n.MsgFieldRecommendationUnavailable)})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}
