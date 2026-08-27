package api

import (
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ContentIndexHandler struct {
	search *service.HybridSearchService
}

func NewContentIndexHandler(search *service.HybridSearchService) *ContentIndexHandler {
	return &ContentIndexHandler{search: search}
}

// UpsertDocument godoc
// @Summary 写入技术内容检索投影 | Upsert technical content search projection
// @Description 仅 addp-meta 可按当前 Tenant 幂等覆盖一个 DataItem 内容文档 | Only addp-meta may idempotently replace a DataItem content document in the current tenant
// @Tags Manager Runtime
// @Accept json
// @Param document_id path string true "DataItem fingerprint"
// @Param request body client.ManagerContentDocument true "内容文档 | Content document"
// @Success 204
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content_index.update"]
// @Router /runtime/content-documents/{document_id} [put]
func (h *ContentIndexHandler) UpsertDocument(c *gin.Context) {
	documentID := strings.TrimSpace(c.Param("document_id"))
	var document commonClient.ManagerContentDocument
	if documentID == "" || len(documentID) > 128 || strings.Contains(documentID, "/") || c.ShouldBindJSON(&document) != nil || document.DocumentID != documentID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content document"})
		return
	}
	if h.search == nil || !h.search.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Manager content index is unavailable"})
		return
	}
	if err := h.search.UpsertContentDocument(c.Request.Context(), commonAuth.GetTenantID(c), document); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteEngineDocuments godoc
// @Summary 删除 Engine 内容检索投影 | Delete engine content search projections
// @Description 仅 addp-meta 可删除当前 Tenant 指定 Engine 的全部内容文档 | Only addp-meta may delete all content documents for an engine in the current tenant
// @Tags Manager Runtime
// @Param engine_id query int true "Engine ID"
// @Param data_item_type query string false "DataItem type"
// @Param schema query string false "Database schema"
// @Param bucket query string false "Object bucket"
// @Param path_prefix query string false "Object path prefix"
// @Success 204
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content_index.update"]
// @Router /runtime/content-documents [delete]
func (h *ContentIndexHandler) DeleteEngineDocuments(c *gin.Context) {
	engineID, err := strconv.ParseUint(strings.TrimSpace(c.Query("engine_id")), 10, 64)
	if err != nil || engineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	if h.search == nil || !h.search.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Manager content index is unavailable"})
		return
	}
	scope := service.ContentDocumentDeleteScope{
		EngineID: uint(engineID), DataItemType: strings.TrimSpace(c.Query("data_item_type")),
		Schema: strings.TrimSpace(c.Query("schema")), Bucket: strings.TrimSpace(c.Query("bucket")),
		PathPrefix: strings.TrimSpace(c.Query("path_prefix")),
	}
	if err := h.search.DeleteContentDocuments(c.Request.Context(), commonAuth.GetTenantID(c), scope); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
