package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type EmbeddingHandler struct {
	embeddingService *service.EmbeddingService
}

func NewEmbeddingHandler(embeddingService *service.EmbeddingService) *EmbeddingHandler {
	return &EmbeddingHandler{embeddingService: embeddingService}
}

// CreateEmbeddingExecution godoc
// @Summary 创建一次性向量化执行 | Create ad-hoc embedding execution
// @Description 从资源树 item 或 node 触发一次性向量化执行，返回统一 execution_id，不创建向量化任务定义。| Create an ad-hoc embedding execution from a resource tree item or node and return the unified execution_id.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body service.EmbeddingExecutionRequest true "一次性向量化执行请求 | Ad-hoc embedding execution request"
// @Success 200 {object} service.EmbeddingExecutionResponse "执行已创建 | Execution created"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "向量化服务不可用 | Embedding service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /embedding_executions [post]
// @Security BearerAuth
func (h *EmbeddingHandler) CreateEmbeddingExecution(c *gin.Context) {
	if h.embeddingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量化服务不可用"})
		return
	}

	req, err := decodeEmbeddingExecutionRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.embeddingService.CreateAdhocExecution(c.Request.Context(), tenantIDValue(c), userIDValue(c), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func decodeEmbeddingExecutionRequest(c *gin.Context) (service.EmbeddingExecutionRequest, error) {
	var req service.EmbeddingExecutionRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	return req, nil
}

// ListEmbeddings godoc
// @Summary 查询向量化结果 | List embedding results
// @Description 查询 Manager 向量化结果 artifact state。| List Manager embedding artifact states.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number"
// @Param page_size query int false "每页数量，默认20 | Page size"
// @Param engine_id query int false "引擎ID | Engine ID"
// @Param node_id query int false "节点ID | Node ID"
// @Param item_id query int false "数据项ID | Item ID"
// @Param status query string false "结果状态 | Result status"
// @Param q query string false "关键词 | Query"
// @Success 200 {object} map[string]interface{} "向量化结果列表 | Embedding result list"
// @Failure 503 {object} map[string]interface{} "向量化服务不可用 | Embedding service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /embeddings [get]
// @Security BearerAuth
func (h *EmbeddingHandler) ListEmbeddings(c *gin.Context) {
	if h.embeddingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量化服务不可用"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	engineID, _ := strconv.ParseUint(c.DefaultQuery("engine_id", "0"), 10, 64)
	nodeID, _ := strconv.ParseUint(c.DefaultQuery("node_id", "0"), 10, 64)
	itemID, _ := strconv.ParseUint(c.DefaultQuery("item_id", "0"), 10, 64)
	items, total, err := h.embeddingService.ListEmbeddings(c.Request.Context(), repository.EmbeddingListFilter{
		TenantID: tenantIDValue(c),
		EngineID: uint(engineID),
		NodeID:   uint(nodeID),
		ItemID:   uint(itemID),
		Status:   c.Query("status"),
		Query:    c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// GetItemEmbedding godoc
// @Summary 查询 item 向量化状态 | Get item embedding state
// @Description 查询单个 data item 的当前向量化结果状态。| Get current embedding state for a data item.
// @Tags Manager
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} map[string]interface{} "item 向量化状态 | Item embedding state"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "向量化服务不可用 | Embedding service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /items/{item_id}/embedding [get]
// @Security BearerAuth
func (h *EmbeddingHandler) GetItemEmbedding(c *gin.Context) {
	if h.embeddingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量化服务不可用"})
		return
	}
	itemID, err := strconv.ParseUint(c.Param("item_id"), 10, 64)
	if err != nil || itemID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的数据项ID"})
		return
	}
	state, itemFingerprint, err := h.embeddingService.GetItemEmbeddingState(c.Request.Context(), tenantIDValue(c), uint(itemID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{
		"item_id":          uint(itemID),
		"item_fingerprint": itemFingerprint,
		"has_embedding":    state != nil,
		"embedding":        nil,
	}
	if state != nil {
		resp["embedding"] = gin.H{
			"result_id":        state.ID,
			"status":           state.Status,
			"status_reason":    state.StatusReason,
			"model_profile_id": state.ModelProfileID,
			"profile_version":  state.ProfileVersion,
			"deployment_id":    state.DeploymentID,
			"dimension":        state.Dimension,
			"vectorized_at":    state.VectorizedAt,
		}
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteEmbedding godoc
// @Summary 删除向量化结果 | Delete embedding result
// @Description 删除 Manager 向量化结果，不删除源 data item。| Delete an embedding result without deleting the source data item.
// @Tags Manager
// @Produce json
// @Param id path int true "向量化结果ID | Embedding result ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "向量化服务不可用 | Embedding service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /embeddings/{id} [delete]
// @Security BearerAuth
func (h *EmbeddingHandler) DeleteEmbedding(c *gin.Context) {
	if h.embeddingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "向量化服务不可用"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的向量化结果ID"})
		return
	}
	if err := h.embeddingService.DeleteEmbedding(c.Request.Context(), tenantIDValue(c), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
