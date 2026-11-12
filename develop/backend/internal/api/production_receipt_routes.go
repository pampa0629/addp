package api

import (
	"context"
	"errors"
	"net/http"

	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/production"
	developi18n "github.com/addp/develop/backend/i18n"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type committedProductionReader interface {
	ReadCommittedProduction(context.Context, uint, production.Reference) (*production.Receipt, error)
}

type ProductionReceiptHandler struct{ reader committedProductionReader }

func registerProductionReceiptRoutes(api *gin.RouterGroup, reader committedProductionReader) {
	routes := api.Group("/runtime/productions")
	routes.Use(commonauth.MustNewServiceClientGuard("addp-system", "addp-meta"))
	routes.GET("/:execution_id/:result_id/receipt", commonauth.MustNewPermissionGuard(developauthorization.PermissionDevelopProductionReceiptRead), (&ProductionReceiptHandler{reader}).Get)
}

// Get returns only the persisted committed result, never execution authority.
// @Summary 读取 Develop 最小成果回执 | Read minimal Develop production receipt
// @Description 仅同租户 System 或 Meta Service；结果 UUID 是原发布 operation UUID。父任务失败不抹除已提交成果；不返回配置、Runtime manifest、Provider 私有状态、连接、凭据、lease 或授权结论。 | Same-tenant System or Meta Service only. The result UUID is the original publication operation UUID. Parent failure does not erase committed results. Returns no config, Runtime manifest, private Provider state, connection, credentials, lease or authorization verdict.
// @Tags Execution authorization
// @Produce json
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Param result_id path string true "原发布 operation UUID | Original publication operation UUID"
// @Success 200 {object} production.Receipt
// @Failure 400,401,403,404,500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.production_receipt.read"]
// @Router /runtime/productions/{execution_id}/{result_id}/receipt [get]
// @Security BearerAuth
func (h *ProductionReceiptHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref := production.Reference{Owner: "develop", ExecutionID: c.Param("execution_id"), ResultID: c.Param("result_id")}
	if ref.Validate() != nil || c.Request.URL.RawQuery != "" || (c.Request.Body != nil && c.Request.Body != http.NoBody && c.Request.ContentLength != 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionParametersInvalid), "error_code": "execution_parameters_invalid"})
		return
	}
	receipt, err := h.reader.ReadCommittedProduction(c.Request.Context(), tenantIDValue(c), ref)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionNotFound), "error_code": "execution_not_found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionReadFailed), "error_code": "execution_read_failed"})
		}
		return
	}
	c.JSON(http.StatusOK, receipt)
}
