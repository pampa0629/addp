package api

import (
	"context"
	"errors"
	"net/http"

	commonclient "github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fileExecutionReader interface {
	Read(context.Context, uint, string) (*commonclient.DevelopFileExecutionFacts, error)
}

type FileExecutionHandler struct{ repository fileExecutionReader }

func NewFileExecutionHandler(repository fileExecutionReader) *FileExecutionHandler {
	return &FileExecutionHandler{repository: repository}
}

func registerFileExecutionRoutes(api *gin.RouterGroup, reader fileExecutionReader) {
	routes := api.Group("/runtime/file-executions")
	routes.Use(commonauth.MustNewServiceClientGuard("addp-system"))
	routes.GET("/:execution_id", commonauth.MustNewPermissionGuard(developauthorization.PermissionDevelopFileExecutionRead), NewFileExecutionHandler(reader).Get)
}

// Get returns the immutable Owner projection; System reads lifecycle from the shared store.
// @Summary 读取 Develop 冻结文件执行事实 | Read frozen Develop file execution facts
// @Description 仅同租户 addp-system Service；返回原始配置、Intent、主体、绑定范围及已登记发布操作的最小事实。不返回连接、凭据、租约、Provider 私有回执或授权结论。 | Same-tenant addp-system Service only. Returns the original config, intent, actor, bound scope and minimal registered publication facts without connections, credentials, leases, private Provider receipts or permission decisions.
// @Tags Execution authorization
// @Produce json
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Success 200 {object} commonclient.DevelopFileExecutionFacts
// @Failure 400,401,403,404,500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.file_execution.read"]
// @Router /runtime/file-executions/{execution_id} [get]
// @Security BearerAuth
func (h *FileExecutionHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("execution_id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id || c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionParametersInvalid), "error_code": "execution_parameters_invalid"})
		return
	}
	facts, err := h.repository.Read(c.Request.Context(), tenantIDValue(c), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionNotFound), "error_code": "execution_not_found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgExecutionReadFailed), "error_code": "execution_read_failed"})
		}
		return
	}
	c.JSON(http.StatusOK, facts)
}
