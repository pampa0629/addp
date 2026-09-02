package api

import (
	"errors"
	"net/http"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetDataItemSecurityFacts godoc
// @Summary 获取单个 DataItem 的安全技术事实 | Get security technical facts for one DataItem
// @Description 仅供 addp-security 使用，按当前 Tenant 和精确 fingerprint 返回无原值的字段结构事实 | Restricted to addp-security; returns value-free field structure facts by current tenant and exact fingerprint
// @Tags Meta Runtime
// @Produce json
// @Param fingerprint path string true "DataItem 指纹 | DataItem fingerprint"
// @Success 200 {object} dataprotection.DataItemSecurityFacts "安全技术事实 | Security technical facts"
// @Failure 404 {object} map[string]interface{} "DataItem 不存在 | DataItem not found"
// @Failure 422 {object} map[string]interface{} "结构事实不可用 | Structure facts unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.security_facts.read"]
// @Router /runtime/data-items/{fingerprint}/security-facts [get]
// @Security BearerAuth
func (h *Handler) GetDataItemSecurityFacts(c *gin.Context) {
	fingerprint := strings.TrimSpace(c.Param("fingerprint"))
	facts, err := h.metadataQueryService.GetDataItemSecurityFacts(commonAuth.GetTenantID(c), fingerprint)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "DataItem not found"})
		case strings.Contains(err.Error(), "unavailable"):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "DataItem structure facts unavailable"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read DataItem security facts"})
		}
		return
	}
	c.JSON(http.StatusOK, facts)
}
