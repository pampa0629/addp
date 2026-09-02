package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

type ElementRevisionResolutionHandler struct {
	service *service.ElementRevisionResolutionService
}

type elementRevisionResolutionRequest struct {
	ElementIDs []string  `json:"element_ids" binding:"required,min=1,max=200,unique,dive,required" minItems:"1" maxItems:"200"`
	AsOf       time.Time `json:"as_of" binding:"required" format:"date-time"`
}

type elementRevisionResolutionResponse struct {
	Results []service.ElementRevisionResolution `json:"results"`
}

func NewElementRevisionResolutionHandler(resolutionService *service.ElementRevisionResolutionService) *ElementRevisionResolutionHandler {
	return &ElementRevisionResolutionHandler{service: resolutionService}
}

// Resolve 按同一时点精确解析数据元修订。
// @Summary 按时点批量解析数据元修订 | Resolve data element revisions at one point in time
// @Description 仅 addp-catalog 与 addp-model Tenant Service Principal 可按统一 as_of 解析精确数据元修订及其码值集快照；跨 Tenant、不存在或该时点无生效修订统一 found=false | Only addp-catalog and addp-model tenant service principals may resolve exact data element revisions and code-set snapshots at one shared as_of; cross-tenant, missing, or non-effective elements return found=false
// @Tags StandardRuntime
// @Accept json
// @Produce json
// @Param request body elementRevisionResolutionRequest true "数据元 ID 与统一查询时点，最多 200 个 | Element IDs and one shared query time, up to 200"
// @Success 200 {object} elementRevisionResolutionResponse
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "需要认证 | Authentication required"
// @Failure 403 {object} map[string]string "仅允许 addp-catalog 或 addp-model 且需数据元读取权限 | addp-catalog or addp-model and element read permission required"
// @Failure 500 {object} map[string]string "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read"]
// @Router /runtime/element-revisions/resolve [post]
// @Security BearerAuth
func (h *ElementRevisionResolutionHandler) Resolve(c *gin.Context) {
	var request elementRevisionResolutionRequest
	if h == nil || h.service == nil || c.ShouldBindJSON(&request) != nil || request.AsOf.IsZero() {
		respondError(c, http.StatusBadRequest, service.ErrInvalidElementRevisionResolutionRequest)
		return
	}
	elementIDs := make([]int64, 0, len(request.ElementIDs))
	for _, value := range request.ElementIDs {
		elementID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || elementID <= 0 || strconv.FormatInt(elementID, 10) != value {
			respondError(c, http.StatusBadRequest, service.ErrInvalidElementRevisionResolutionRequest)
			return
		}
		elementIDs = append(elementIDs, elementID)
	}
	results, err := h.service.Resolve(c.Request.Context(), getTenantID(c), elementIDs, request.AsOf)
	if err != nil {
		if errors.Is(err, service.ErrInvalidElementRevisionResolutionRequest) {
			respondError(c, http.StatusBadRequest, err)
			return
		}
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, elementRevisionResolutionResponse{Results: results})
}
