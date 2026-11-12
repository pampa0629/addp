package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/production"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
)

// EnsureProductionBasicScan godoc
// @Summary 识别原已提交独立成果 | Identify an original committed result
// @Description 仅对应已注册生产者 Tenant Service；无正文和 query。反查原成果回执，仅识别完整精确目标。重复请求恢复原扫描，不复活失败执行，不授予用户权限 | Registered producer Tenant Service only; no body or query. Resolves original receipt and identifies exact committed objects. Replay recovers the original scan without reviving failed executions or granting user access
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param producer_owner path string true "生产模块 | Producer module"
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Param result_id path string true "原独立成果 UUID | Original result UUID"
// @Success 200 {object} commonclient.MetaProductionIdentification
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /runtime/productions/{producer_owner}/{execution_id}/{result_id}/basic-scan [post]
func (h *Handler) EnsureProductionBasicScan(c *gin.Context) { h.productionScan(c, "basic") }

// EnsureProductionDeepScan godoc
// @Summary 串接原已提交成果的自动深度扫描 | Chain automatic deep scanning of an original committed result
// @Description 仅原已注册生产者 Tenant Service，无正文和 query。复用唯一 basic 身份识别并持久登记依赖它的唯一 deep 执行；basic 成功后才读取精确成果。重复请求不复活失败扫描，不等待或签发授权 | Registered producer Tenant Service only; no body or query. Reuses the unique basic identification and durably queues one dependent deep execution. Reads only the exact result after basic succeeds. Replay never revives failed scans or waits for or issues Grants
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param producer_owner path string true "生产模块 | Producer module"
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Param result_id path string true "原独立成果 UUID | Original result UUID"
// @Success 200 {object} commonclient.MetaProductionDeepScan
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /runtime/productions/{producer_owner}/{execution_id}/{result_id}/deep-scan [post]
func (h *Handler) EnsureProductionDeepScan(c *gin.Context) { h.productionScan(c, "deep") }

// ReadProductionIdentification godoc
// @Summary 读取原成果的新逻辑项识别事实 | Read logical new-item identification for a result
// @Description 仅 System Tenant Service；只读既有扫描及 Meta 身份，不回源、不创建扫描或 Grant | System Tenant Service only; reads existing scan and Meta identity without source IO, scan creation or Grants
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param producer_owner path string true "生产模块 | Producer module"
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Param result_id path string true "原独立成果 UUID | Original result UUID"
// @Success 200 {object} commonclient.MetaProductionIdentification
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.task_provider.read"]
// @Router /runtime/productions/{producer_owner}/{execution_id}/{result_id}/basic-scan [get]
func (h *Handler) ReadProductionIdentification(c *gin.Context) { h.productionScan(c, "read") }

func (h *Handler) productionScan(c *gin.Context, action string) {
	c.Header("Cache-Control", "no-store")
	ref := production.Reference{Owner: c.Param("producer_owner"), ExecutionID: c.Param("execution_id"), ResultID: c.Param("result_id")}
	if ref.Validate() != nil || c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		c.JSON(400, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
		return
	}
	if action != "read" {
		principal, exists := commonauth.PrincipalFromGin(c)
		if !exists || principal.Type != "service_principal" || commonauth.GetClientID(c) != "addp-"+ref.Owner {
			c.JSON(403, gin.H{"error_code": "scan_provenance_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
			return
		}
	}
	var result interface{}
	var err error
	if h.executionService != nil {
		switch action {
		case "basic":
			var run *commonclient.MetaProductionIdentification
			run, err = h.executionService.EnsureProductionBasicScan(c.Request.Context(), commonauth.GetTenantID(c), ref)
			result = run
		case "deep":
			result, err = h.executionService.EnsureProductionDeepScan(c.Request.Context(), commonauth.GetTenantID(c), ref)
		case "read":
			result, err = h.executionService.ReadProductionIdentification(c.Request.Context(), commonauth.GetTenantID(c), ref)
		}
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
	}
	status := 503
	if errors.Is(err, commonapi.ErrConflict) {
		status = 409
	}
	if errors.Is(err, commonapi.ErrForbidden) {
		status = 403
	}
	c.JSON(status, gin.H{"error_code": "production_scan_pending", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
}
