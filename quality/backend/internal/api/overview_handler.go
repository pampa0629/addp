package api

import (
	"net/http"
	"strconv"

	qualityi18n "github.com/addp/quality/i18n"
	_ "github.com/addp/quality/internal/models" // Swagger response model.
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type OverviewHandler struct{ service *service.OverviewService }

func NewOverviewHandler(s *service.OverviewService) *OverviewHandler {
	return &OverviewHandler{service: s}
}

// Get 质量概览。
// @Summary 质量概览 | Quality overview
// @Description 当前方案按实际目标范围统计；历史按执行快照和 UTC 日期统计，规则通过率按检查项加权。| Current plans grouped by actual target scope; history uses execution snapshots and UTC days, weighted by checks.
// @Tags QualityOverview
// @Produce json
// @Param owner_domain_id query int false "归属域，0 为租户公共 | Owner domain; 0 means tenant-public" minimum(0)
// @Param days query int false "历史天数 | History days" Enums(7,30,90) default(30)
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} models.QualityOverview
// @Failure 400 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.plan.read","quality.issue.read","monitor.execution.read"]
// @Router /overview [get]
// @Security BearerAuth
func (h *OverviewHandler) Get(c *gin.Context) {
	domain, err := ownerDomainFilter(c)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	days := 30
	if c.Query("days") != "" {
		days, err = strconv.Atoi(c.Query("days"))
		if err != nil || (days != 7 && days != 30 && days != 90) {
			respondInvalidRequest(c, "")
			return
		}
	}
	page, size := pageParams(c.Query("page"), c.Query("page_size"))
	result, err := h.service.Get(c.Request.Context(), getTenantID(c), domain, days, page, size)
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}
