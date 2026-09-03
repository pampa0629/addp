package api

import (
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type SensitiveFindingListResponse = models.SensitiveFindingListResponse
type SensitiveFindingResponse = models.SensitiveFindingResponse
type SensitiveDiscoveryQualitySummary = models.SensitiveDiscoveryQualitySummary

type FindingHandler struct{ discoveries *service.DiscoveryService }

func NewFindingHandler(discoveries *service.DiscoveryService) *FindingHandler {
	return &FindingHandler{discoveries: discoveries}
}

// @Summary 敏感发现列表 | List sensitive findings
// @Description 分页返回当前租户不含原始敏感值的不可变检测发现、目标资源快照、可选初审记录，以及由当前检测绑定、正式评估、默认保护规则和真实出口投影批量组装的只读解释链；可按纳管、来源快照、发现执行、当前快照、复核状态、敏感类型和识别能力版本筛选 | Return immutable, value-free detector findings with target snapshots, optional first reviews, and a read-only explanation chain batch-assembled from current control-plane facts; filter by enrollment, source snapshot, discovery execution, current snapshot, review state, sensitive type, and detector version
// @Tags Sensitive Finding
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Param enrollment_id query string false "纳管 ID | Enrollment ID"
// @Param source_snapshot_hash query string false "精确来源快照哈希 | Exact source snapshot hash"
// @Param discovery_execution_id query string false "发现执行 ID | Discovery execution ID"
// @Param snapshot_scope query string false "快照范围：all/current，默认 all | Snapshot scope: all/current, default all"
// @Param review_state query string false "复核状态：all/pending/reviewed，默认 all | Review state: all/pending/reviewed, default all"
// @Param sensitive_data_type_id query int false "敏感数据类型 ID | Sensitive data type ID"
// @Param detector_version query string false "精确识别能力版本 | Exact detector capability version"
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
	var sensitiveDataTypeID *int64
	if value := strings.TrimSpace(c.Query("sensitive_data_type_id")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			respondError(c, commonapi.ErrBadRequest)
			return
		}
		sensitiveDataTypeID = &parsed
	}
	result, err := h.discoveries.ListFindings(c.Request.Context(), getTenantID(c), service.FindingListFilter{
		EnrollmentID: c.Query("enrollment_id"), SourceSnapshotHash: c.Query("source_snapshot_hash"), DiscoveryExecutionID: c.Query("discovery_execution_id"),
		SnapshotScope: c.Query("snapshot_scope"), ReviewState: c.Query("review_state"), SensitiveDataTypeID: sensitiveDataTypeID, DetectorVersion: c.Query("detector_version"),
	}, int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 敏感发现详情 | Get sensitive finding
// @Description 返回单个不含原始敏感值的检测证据、可选初审记录，以及由当前控制面事实组装的只读保护解释链 | Return one value-free detector observation with its optional first review and a read-only protection explanation assembled from current control-plane facts
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

// @Summary 敏感发现质量摘要 | Get sensitive discovery quality summary
// @Description 即时聚合当前候选、按资源组件和能力版本去重的人工复核证据，以及人工补充线索；不读取原始数据、不持久化第二份统计事实，可按敏感数据类型过滤 | Aggregate current findings, human review evidence deduplicated by enrolled component and capability version, and manual supplement signals on demand; no raw data is read and no duplicate statistics fact is persisted; optionally filter by sensitive data type
// @Tags Sensitive Finding
// @Produce json
// @Param sensitive_data_type_id query int false "敏感数据类型 ID | Sensitive data type ID"
// @Success 200 {object} SensitiveDiscoveryQualitySummary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.finding.read"]
// @Router /discovery-quality [get]
// @Security BearerAuth
func (h *FindingHandler) Quality(c *gin.Context) {
	var sensitiveDataTypeID *int64
	if value := strings.TrimSpace(c.Query("sensitive_data_type_id")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			respondError(c, commonapi.ErrBadRequest)
			return
		}
		sensitiveDataTypeID = &parsed
	}
	result, err := h.discoveries.GetQualitySummary(c.Request.Context(), getTenantID(c), sensitiveDataTypeID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
