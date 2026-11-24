package api

import (
	"net/http"
	"net/url"
	"strconv"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FrontendObservation godoc
// @Summary 读取前端采集摘要 | Read frontend observation
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param node_id query string true "采集节点 ID | Collection node ID"
// @Param view query string true "清单、当前页资源或单模块详情 | Inventory, page resources or single-module details" Enums(inventory,cards,detail)
// @Param modules query string false "卡片页最多 20 个模块名，详情仅一个，清单省略 | Up to 20 module names for cards, one for detail, omit for inventory"
// @Success 200 {object} service.FrontendObservation
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.host_node.read"]
// @Router /platform/frontend_observations [get]
func (h *ResourceObservationHandler) FrontendObservation(c *gin.Context) {
	principal, raw, ok := deploymentNodeObservationSubject(c, true)
	if !ok {
		return
	}
	projection, err := resourcequery.NewFrontendProjection(c.Query("view"), c.Query("modules"))
	if err != nil {
		resourceError(c, err)
		return
	}
	var result service.FrontendObservation
	result, err = h.service.FrontendObservation(c.Request.Context(), principal, targetToken(c), raw, projection)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// FrontendNodes godoc
// @Summary 查询已配置前端采集的节点 | List configured frontend collection nodes
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.FrontendNodes
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.host_node.read"]
// @Router /platform/frontend_nodes [get]
func (h *ResourceObservationHandler) FrontendNodes(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	result, err := h.service.FrontendNodes(c.Request.Context(), targetToken(c))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func deploymentNodeObservationSubject(c *gin.Context, frontend bool) (string, string, bool) {
	if len(c.Request.URL.RawQuery) > 2048 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return "", "", false
	}
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	keys := 1
	if frontend {
		keys++
		if len(q["view"]) != 1 {
			resourceError(c, resourcequery.ErrInvalid)
			return "", "", false
		}
		if q.Get("view") != "inventory" {
			keys++
			if len(q["modules"]) != 1 {
				resourceError(c, resourcequery.ErrInvalid)
				return "", "", false
			}
		}
	}
	if err != nil || len(q) != keys || len(q["node_id"]) != 1 {
		resourceError(c, resourcequery.ErrInvalid)
		return "", "", false
	}
	raw := q.Get("node_id")
	node, err := uuid.Parse(raw)
	if err != nil || node == uuid.Nil || node.String() != raw {
		resourceError(c, resourcequery.ErrInvalid)
		return "", "", false
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return "", "", false
	}
	return strconv.FormatUint(uint64(principal), 10), raw, true
}

// BusinessNFSObservation godoc
// @Summary 读取指定节点的 Business NFS 导出状态 | Read the Business NFS export state for one node
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param node_id query string true "采集节点 ID | Collection node ID"
// @Success 200 {object} service.BusinessNFSObservation
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.host_node.read","platform.engine_deployment.read"]
// @Router /platform/business_nfs_observations [get]
func (h *ResourceObservationHandler) BusinessNFSObservation(c *gin.Context) {
	principal, node, ok := deploymentNodeObservationSubject(c, false)
	if !ok {
		return
	}
	result, err := h.service.BusinessNFSObservation(c.Request.Context(), principal, targetToken(c), node)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
