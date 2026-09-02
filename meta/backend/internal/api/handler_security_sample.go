package api

import (
	"net/http"
	"strings"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
)

// GetDataItemSecuritySample godoc
// @Summary 读取 DataItem 受控安全样本 | Read controlled DataItem security sample
// @Description 仅 addp-security 可按当前 Tenant 和确定 fingerprint 临时读取源文档的有界文本样本；样本不写入 Meta attributes | Only addp-security may transiently read a bounded source-document text sample for an exact fingerprint in the current tenant; the sample is not persisted in Meta attributes
// @Tags Meta Runtime
// @Produce json
// @Param fingerprint path string true "DataItem fingerprint"
// @Success 200 {object} dataprotection.DataItemSecuritySample "受控样本 | Controlled sample"
// @Failure 400 {object} map[string]interface{} "fingerprint 无效 | Invalid fingerprint"
// @Failure 503 {object} map[string]interface{} "样本读取不可用 | Sample read unavailable"
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.security_facts.read"]
// @Router /runtime/data-items/{fingerprint}/security-sample [get]
func (h *Handler) GetDataItemSecuritySample(c *gin.Context) {
	if h.scanService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, metai18n.MsgSecuritySampleUnavailable), "error_code": "meta_security_sample_unavailable"})
		return
	}
	fingerprint := strings.TrimSpace(c.Param("fingerprint"))
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, metai18n.MsgSecuritySampleInvalid), "error_code": "meta_security_sample_invalid"})
		return
	}
	sample, err := h.scanService.ReadDataItemSecuritySample(c.Request.Context(), commonAuth.GetTenantID(c), fingerprint)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, metai18n.MsgSecuritySampleUnavailable), "error_code": "meta_security_sample_unavailable"})
		return
	}
	c.JSON(http.StatusOK, sample)
}
