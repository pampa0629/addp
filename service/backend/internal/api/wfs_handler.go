package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WFSHandler WFS 服务处理器
type WFSHandler struct {
	repo   *repository.InternalServiceRepository
	db     *gorm.DB
	sqlDB  *sql.DB // 用于 sql 查询
}

// NewWFSHandler 创建新的 WFS 处理器
func NewWFSHandler(repo *repository.InternalServiceRepository, db *gorm.DB, sqlDB *sql.DB) *WFSHandler {
	return &WFSHandler{
		repo:  repo,
		db:    db,
		sqlDB: sqlDB,
	}
}

// HandleWFSRequest 处理 WFS 请求的主处理器
// GET /ogc/wfs/:serviceName
func (h *WFSHandler) HandleWFSRequest(c *gin.Context) {
	serviceName := c.Param("serviceName")

	// 解析请求参数
	request := c.Query("request")
	service := c.Query("service")

	// 验证服务类型
	if service != "WFS" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service type. Expected 'WFS'"})
		return
	}

	// 验证服务是否存在且启用 WFS
	svc, err := h.repo.GetByName(serviceName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	if !svc.EnabledWFS {
		c.JSON(http.StatusForbidden, gin.H{"error": "WFS is not enabled for this service"})
		return
	}

	// 根据 request 参数分发到不同的处理器
	switch strings.ToUpper(request) {
	case "GETCAPABILITIES":
		h.GetCapabilities(c, serviceName, svc)

	case "DESCRIBEFEATURETYPE":
		h.DescribeFeatureType(c, serviceName, svc)

	case "GETFEATURE":
		h.GetFeature(c, serviceName, svc)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown or missing request parameter"})
	}
}

// GetCapabilities 处理 GetCapabilities 请求
func (h *WFSHandler) GetCapabilities(c *gin.Context, serviceName string, svc *models.InternalService) {
	// 生成 Capabilities XML
	baseURL := "http://" + c.Request.Host + c.Request.RequestURI
	baseURL = strings.Split(baseURL, "?")[0] // 移除查询参数

	logger.L().Debug("WFS GetCapabilities requested", "service", serviceName)

	// TODO: 完整实现 GenerateCapabilities
	c.JSON(http.StatusOK, gin.H{"message": "GetCapabilities not yet fully implemented"})
}

// DescribeFeatureType 处理 DescribeFeatureType 请求
func (h *WFSHandler) DescribeFeatureType(c *gin.Context, serviceName string, svc *models.InternalService) {
	typeName := c.Query("typeName")
	if typeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameter: typeName"})
		return
	}

	// TODO: 实现 DescribeFeatureType 逻辑
	c.JSON(http.StatusOK, gin.H{"message": "DescribeFeatureType not yet implemented"})
}

// GetFeature 处理 GetFeature 请求
func (h *WFSHandler) GetFeature(c *gin.Context, serviceName string, svc *models.InternalService) {
	typeName := c.Query("typeName")
	if typeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameter: typeName"})
		return
	}

	// TODO: 实现 GetFeature 逻辑
	c.JSON(http.StatusOK, gin.H{"message": "GetFeature not yet implemented"})
}

// PostGetFeature 处理 POST 方式的 GetFeature 请求
// POST /ogc/wfs/:serviceName
func (h *WFSHandler) PostGetFeature(c *gin.Context) {
	// TODO: 实现 POST GetFeature
	c.JSON(http.StatusOK, gin.H{"message": "POST GetFeature not yet implemented"})
}
