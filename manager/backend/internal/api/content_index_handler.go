package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type contentIndexer interface {
	Enabled() bool
	UpsertContentDocument(context.Context, uint, commonClient.ManagerContentDocument) error
	DeleteContentDocuments(context.Context, uint, service.ContentDocumentDeleteScope) error
}

type ContentIndexHandler struct {
	search contentIndexer
}

func NewContentIndexHandler(search contentIndexer) *ContentIndexHandler {
	return &ContentIndexHandler{search: search}
}

// UpsertDocument godoc
// @Summary 写入技术内容检索投影 | Upsert technical content search projection
// @Description 仅 addp-meta 可分别覆盖当前 Tenant 的技术或正文完整快照，保留另一部分；持久登记并核验保护版本，外部任务确认成功才返回 204；超时或未决提交返回 503，不表示任务取消 | Only addp-meta may replace owned technical or extracted content snapshot fields while retaining the other snapshot; persist delivery and verify protection version, return 204 only for proven external success; timeout or uncertain submission returns 503 and does not imply cancellation
// @Tags Manager Runtime
// @Accept json
// @Param document_id path string true "DataItem fingerprint"
// @Param request body client.ManagerContentDocument true "内容文档 | Content document"
// @Success 204
// @Failure 400 {object} map[string]interface{} "请求契约无效 | Invalid request contract"
// @Failure 409 {object} map[string]interface{} "protection_version_changed：保护版本变化；manager_content_protection_required：无可执行 search_index 投影 | protection_version_changed: protection version changed; manager_content_protection_required: no executable search_index projection"
// @Failure 503 {object} map[string]interface{} "内容索引不可用 | Content index unavailable"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content_index.update"]
// @Router /runtime/content-documents/{document_id} [put]
func (h *ContentIndexHandler) UpsertDocument(c *gin.Context) {
	documentID := strings.TrimSpace(c.Param("document_id"))
	var document commonClient.ManagerContentDocument
	if documentID == "" || len(documentID) > 128 || strings.Contains(documentID, "/") || c.ShouldBindJSON(&document) != nil || document.DocumentID != documentID || document.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentDocumentInvalid), "error_code": "manager_content_document_invalid"})
		return
	}
	if h.search == nil || !h.search.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentIndexUnavailable), "error_code": "manager_content_index_unavailable"})
		return
	}
	if err := h.search.UpsertContentDocument(c.Request.Context(), commonAuth.GetTenantID(c), document); err != nil {
		if errors.Is(err, projectionstore.ErrVersionChanged) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentProtectionVersionChanged), "error_code": "protection_version_changed"})
			return
		}
		if errors.Is(err, managerprotection.ErrRequired) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentProtectionRequired), "error_code": "manager_content_protection_required"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentIndexUnavailable), "error_code": "manager_content_index_unavailable"})
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteEngineDocuments godoc
// @Summary 删除 Engine 内容检索投影 | Delete engine content search projections
// @Description 仅 addp-meta 可删除当前 Tenant 指定 Engine 的内容投影；等待已有写入及实际删除成功，超时保留持久任务，不能视为已删除 | Only addp-meta may delete engine content projections in the tenant; wait for prior writes and proven deletion, retain pending deliveries on timeout without claiming deletion
// @Tags Manager Runtime
// @Param engine_id query int true "Engine ID"
// @Param data_item_type query string false "DataItem type"
// @Param schema query string false "Database schema"
// @Param bucket query string false "Object bucket"
// @Param path_prefix query string false "Object path prefix"
// @Success 204
// @Failure 400 {object} map[string]interface{} "请求契约无效 | Invalid request contract"
// @Failure 503 {object} map[string]interface{} "索引不可用或任务未决 | Index unavailable or delivery unresolved"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content_index.update"]
// @Param document_id query string false "限定单个指纹 | Restrict to one fingerprint"
// @Router /runtime/content-documents [delete]
func (h *ContentIndexHandler) DeleteEngineDocuments(c *gin.Context) {
	engineID, err := strconv.ParseUint(strings.TrimSpace(c.Query("engine_id")), 10, 64)
	if err != nil || engineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, manageri18n.MsgInvalidEngineIDParam), "error_code": "manager_content_document_invalid"})
		return
	}
	if h.search == nil || !h.search.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentIndexUnavailable), "error_code": "manager_content_index_unavailable"})
		return
	}
	scope := service.ContentDocumentDeleteScope{
		DocumentID: strings.TrimSpace(c.Query("document_id")), EngineID: uint(engineID), DataItemType: strings.TrimSpace(c.Query("data_item_type")),
		Schema: strings.TrimSpace(c.Query("schema")), Bucket: strings.TrimSpace(c.Query("bucket")),
		PathPrefix: strings.TrimSpace(c.Query("path_prefix")),
	}
	if err := h.search.DeleteContentDocuments(c.Request.Context(), commonAuth.GetTenantID(c), scope); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, manageri18n.MsgContentIndexUnavailable), "error_code": "manager_content_index_unavailable"})
		return
	}
	c.Status(http.StatusNoContent)
}
