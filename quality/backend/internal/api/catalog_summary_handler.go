package api

import (
	"errors"
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type CatalogSummaryHandler struct {
	service *service.CatalogSummaryService
}

func NewCatalogSummaryHandler(summaryService *service.CatalogSummaryService) *CatalogSummaryHandler {
	return &CatalogSummaryHandler{service: summaryService}
}

// Resolve 动态解析当前 Tenant 的数据表质量摘要。
// @Summary 解析企业目录质量摘要 | Resolve enterprise catalog quality summaries
// @Description 按请求顺序解析 1 到 200 个 PostgreSQL 表的当前质量摘要，仅供 Catalog 服务消费 | Resolve current quality summaries for 1 to 200 PostgreSQL tables in request order; Catalog service only
// @Tags Catalog Integration
// @Accept json
// @Produce json
// @Param request body models.ResolveCatalogSummariesRequest true "结构化表引用 | Structured table references"
// @Success 200 {object} models.ResolveCatalogSummariesResponse
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "需要认证 | Authentication required"
// @Failure 403 {object} map[string]interface{} "非 Catalog 服务或权限不足 | Catalog service or permission required"
// @Failure 500 {object} map[string]interface{} "解析失败 | Resolution failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.catalog.read"]
// @Router /runtime/catalog-summaries/resolve [post]
// @Security BearerAuth
func (h *CatalogSummaryHandler) Resolve(c *gin.Context) {
	var request models.ResolveCatalogSummariesRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.ErrInvalidCatalogSummaryRequest.Error()})
		return
	}
	result, err := h.service.Resolve(c.Request.Context(), getTenantID(c), request.References)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrInvalidCatalogSummaryRequest) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
