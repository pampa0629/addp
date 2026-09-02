package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type SensitiveFindingListResponse = models.SensitiveFindingListResponse
type SensitiveFindingResponse = models.SensitiveFindingResponse

type FindingHandler struct{ discoveries *service.DiscoveryService }

func NewFindingHandler(discoveries *service.DiscoveryService) *FindingHandler {
	return &FindingHandler{discoveries: discoveries}
}

// @Summary 敏感发现列表 | List sensitive findings
// @Description 分页返回当前租户不含原始敏感值的不可变检测发现及可选初审记录，可按纳管与来源快照精确过滤 | Return immutable, value-free detector findings with optional first reviews for the current tenant, optionally filtered by enrollment and source snapshot
// @Tags Sensitive Finding
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Param enrollment_id query string false "纳管 ID | Enrollment ID"
// @Param source_snapshot_hash query string false "精确来源快照哈希 | Exact source snapshot hash"
// @Success 200 {object} SensitiveFindingListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.finding.read"]
// @Router /findings [get]
// @Security BearerAuth
func (h *FindingHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.discoveries.ListFindings(c.Request.Context(), getTenantID(c), c.Query("enrollment_id"), c.Query("source_snapshot_hash"), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 敏感发现详情 | Get sensitive finding
// @Description 返回单个不含原始敏感值的检测证据及可选初审记录 | Return one value-free detector observation with its optional first review
// @Tags Sensitive Finding
// @Produce json
// @Param id path string true "Finding ID"
// @Success 200 {object} SensitiveFindingResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.finding.read"]
// @Router /findings/{id} [get]
// @Security BearerAuth
func (h *FindingHandler) Get(c *gin.Context) {
	result, err := h.discoveries.GetFinding(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
