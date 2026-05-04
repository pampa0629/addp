package api

import (
	"net/http"
	"strconv"
	"strings"

	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

// ServiceEndpointHandler 处理服务端点查询请求
// 供其他模块通过内部 API Key 调用，根据 source_reference 返回统一端点信息
type ServiceEndpointHandler struct {
	querySvc      *svc.QueryServiceService
	registeredSvc *svc.RegisteredServiceService
	tileSvc       *svc.TileServiceService
}

func NewServiceEndpointHandler(
	q *svc.QueryServiceService,
	r *svc.RegisteredServiceService,
	t *svc.TileServiceService,
) *ServiceEndpointHandler {
	return &ServiceEndpointHandler{querySvc: q, registeredSvc: r, tileSvc: t}
}

type serviceEndpointResp struct {
	ServiceType string            `json:"service_type"`
	Title       string            `json:"title"`
	Endpoints   map[string]string `json:"endpoints"`
}

// GetEndpoints 根据 source_reference 返回统一服务端点信息
// @Summary 获取服务端点 | Get service endpoints
// @Tags Service
// @Produce json
// @Param ref query string true "服务引用 | Service reference"
// @Success 200 {object} serviceEndpointResp
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /endpoints [get]
// @Security BearerAuth
func (h *ServiceEndpointHandler) GetEndpoints(c *gin.Context) {
	ref := c.Query("ref")
	if ref == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgMissingRef)})
		return
	}

	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidRefFormat)})
		return
	}

	serviceType := parts[0]
	id64, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgInvalidRefID)})
		return
	}
	id := uint(id64)

	switch serviceType {
	case "query":
		dto, err := h.querySvc.GetService(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, serviceEndpointResp{
			ServiceType: "query",
			Title:       dto.Title,
			Endpoints:   dto.Endpoints,
		})

	case "registered":
		dto, err := h.registeredSvc.GetService(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, serviceEndpointResp{
			ServiceType: "registered",
			Title:       dto.Title,
			Endpoints:   dto.Endpoints,
		})

	case "tile":
		dto, err := h.tileSvc.GetService(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, serviceEndpointResp{
			ServiceType: "tile",
			Title:       dto.Title,
			Endpoints:   dto.Endpoints,
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgUnsupportedType) + ": " + serviceType})
	}
}
