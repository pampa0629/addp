package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CollectExecutionLineage immediately consumes one persisted execution lineage fact.
// @Summary 采集单次执行血缘 | Collect execution lineage
// @Description 采集指定成功执行中已持久化的 lineage_facts；重复调用是幂等的 | Collect persisted lineage_facts from one successful execution; repeated calls are idempotent
// @Tags Meta Lineage
// @Produce json
// @Param execution_id path string true "执行 ID | Execution ID"
// @Success 200 {object} models.LineageCollectionResult "采集结果 | Collection result"
// @Failure 400 {object} models.LineageErrorResponse "执行事实不可采集 | Execution facts cannot be collected"
// @Failure 404 {object} models.LineageErrorResponse "执行不存在 | Execution not found"
// @Failure 500 {object} models.LineageErrorResponse "采集失败 | Collection failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.lineage.create"]
// @Router /lineage/executions/{execution_id}/collect [post]
// @Security BearerAuth
func (h *Handler) CollectExecutionLineage(c *gin.Context) {
	executionID := strings.TrimSpace(c.Param("execution_id"))
	if executionID == "" {
		c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_execution_id"})
		return
	}
	result, err := h.lineageService.CollectExecution(c.Request.Context(), commonAuth.GetTenantID(c), executionID)
	if err == nil {
		c.JSON(http.StatusOK, result)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageSubjectNotFound), ErrorCode: "lineage_execution_not_found"})
		return
	}
	if strings.Contains(err.Error(), "not successful") || strings.Contains(err.Error(), "lineage_facts") {
		c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_execution_lineage"})
		return
	}
	c.JSON(http.StatusInternalServerError, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageQueryFailed), ErrorCode: "lineage_collection_failed"})
}

// RecordServicePublication receives a publication snapshot from Service.
// @Summary 记录服务发布血缘事实 | Record service publication lineage
// @Description Service 同步名称、更新时间与当前版本依赖；更新关闭旧版本，忽略过期通知 | Service publishes its name, update time and current revision dependencies; superseded revisions close and stale notifications are ignored
// @Tags Meta Lineage
// @Accept json
// @Produce json
// @Param request body models.RecordServicePublicationRequest true "服务发布血缘事实 | Service publication lineage"
// @Success 204
// @Failure 400 {object} models.LineageErrorResponse
// @Failure 500 {object} models.LineageErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.lineage.create"]
// @Router /lineage/services [post]
// @Security BearerAuth
func (h *Handler) RecordServicePublication(c *gin.Context) {
	var request models.RecordServicePublicationRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_lineage_publication"})
		return
	}
	if request.ServiceID == 0 || strings.TrimSpace(request.ServiceName) == "" || request.ServiceUpdatedAt.IsZero() || strings.TrimSpace(request.PublishedRevision) == "" {
		c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_lineage_publication"})
		return
	}
	if err := h.lineageService.RecordServicePublication(c.Request.Context(), commonAuth.GetTenantID(c), request); err != nil {
		c.JSON(http.StatusInternalServerError, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageQueryFailed), ErrorCode: "lineage_publication_failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetLineageGraph 查询数据项或已发布服务版本的血缘图。
// @Summary 查询数据血缘图 | Get lineage graph
// @Description 按数据项或已发布服务版本查询当前血缘关系 | Query current lineage for a data item or published service revision
// @Tags Meta Lineage
// @Produce json
// @Param subject_kind query string true "主体类型：data_item 或 published_service | Subject kind: data_item or published_service"
// @Param item_id query int false "数据项 ID，subject_kind=data_item 时必填 | Item ID, required for data_item"
// @Param service_id query int false "服务 ID，subject_kind=published_service 时必填 | Service ID, required for published_service"
// @Param revision query string false "服务发布版本，subject_kind=published_service 时必填 | Published revision, required for published_service"
// @Param direction query string false "方向：upstream、downstream 或两者有向遍历并集 both，不含旁系 | Direction: upstream, downstream or their directed union both; excludes sibling branches" default(both)
// @Param depth query int false "每条派生或服务依赖边计一层，0 只显示主体，范围 0-20 | Each derive or serve edge is one hop; 0 returns only the subject; range 0-20" default(2)
// @Param expand_upstream query string false "额外向上展开的 item ID，逗号分隔，最多100个 | Item IDs to expand upstream, comma separated, max 100"
// @Param expand_downstream query string false "额外向下展开的 item ID，逗号分隔，最多100个 | Item IDs to expand downstream, comma separated, max 100"
// @Param limit query int false "节点和边上限，范围 1-500 | Node and edge limit, 1-500" default(100)
// @Param as_of query string false "历史观察时间（RFC3339） | Historical observation time (RFC3339)"
// @Success 200 {object} models.LineageGraphResponse "血缘图 | Lineage graph"
// @Failure 400 {object} models.LineageErrorResponse "请求参数错误 | Bad request"
// @Failure 404 {object} models.LineageErrorResponse "主体不存在 | Subject not found"
// @Failure 500 {object} models.LineageErrorResponse "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.lineage.read"]
// @Router /lineage/graph [get]
// @Security BearerAuth
func (h *Handler) GetLineageGraph(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)
	request, err := parseLineageGraphRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_lineage_query"})
		return
	}
	graph, err := h.lineageService.GetGraph(c.Request.Context(), tenantID, request)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageSubjectNotFound), ErrorCode: "lineage_subject_not_found"})
			return
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") {
			c.JSON(http.StatusBadRequest, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageInvalidQuery), ErrorCode: "invalid_lineage_query"})
			return
		}
		c.JSON(http.StatusInternalServerError, models.LineageErrorResponse{Error: commoni18n.T(c, metai18n.MsgLineageQueryFailed), ErrorCode: "lineage_query_failed"})
		return
	}
	c.JSON(http.StatusOK, graph)
}

func parseLineageGraphRequest(c *gin.Context) (models.LineageGraphRequest, error) {
	request := models.LineageGraphRequest{
		SubjectKind: c.Query("subject_kind"),
		Direction:   c.DefaultQuery("direction", "both"),
		Depth:       2,
		Limit:       100,
	}
	var err error
	for key, target := range map[string]*[]uint{"expand_upstream": &request.ExpandUpstream, "expand_downstream": &request.ExpandDownstream} {
		if raw := c.Query(key); raw != "" {
			values := strings.Split(raw, ",")
			if len(values) > 100 {
				return request, fmt.Errorf("%s allows at most 100 items", key)
			}
			for _, value := range values {
				id, err := strconv.ParseUint(value, 10, 32)
				if err != nil || id == 0 {
					return request, fmt.Errorf("%s requires positive item IDs", key)
				}
				*target = append(*target, uint(id))
			}
		}
	}
	if raw := c.Query("item_id"); raw != "" {
		value, parseErr := strconv.ParseUint(raw, 10, 32)
		if parseErr != nil || value == 0 {
			return request, fmt.Errorf("item_id must be a positive integer")
		}
		id := uint(value)
		request.ItemID = &id
	}
	if raw := c.Query("service_id"); raw != "" {
		value, parseErr := strconv.ParseUint(raw, 10, 32)
		if parseErr != nil || value == 0 {
			return request, fmt.Errorf("service_id must be a positive integer")
		}
		id := uint(value)
		request.ServiceID = &id
	}
	if raw := c.Query("depth"); raw != "" {
		request.Depth, err = strconv.Atoi(raw)
		if err != nil {
			return request, fmt.Errorf("depth must be an integer")
		}
	}
	if raw := c.Query("limit"); raw != "" {
		request.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return request, fmt.Errorf("limit must be an integer")
		}
	}
	if raw := c.Query("as_of"); raw != "" {
		value, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			return request, fmt.Errorf("as_of must be RFC3339")
		}
		request.AsOf = &value
	}
	if request.SubjectKind == "published_service" {
		request.Revision = strings.TrimSpace(c.Query("revision"))
	}
	return request, nil
}
