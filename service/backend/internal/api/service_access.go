package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logger"
	authMiddleware "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/models"

	"github.com/gin-gonic/gin"
)

func serviceAccessStatus(c *gin.Context, publicAccess bool, serviceTenantID uint) int {
	if publicAccess {
		return 0
	}

	if _, authenticated := authMiddleware.AuthContextFromGin(c); !authenticated {
		return http.StatusUnauthorized
	}
	tenantID, exists := authMiddleware.TenantIDFromGin(c)
	if !exists || uint(tenantID) != serviceTenantID {
		return http.StatusForbidden
	}
	return 0
}

func requireJSONServiceAccess(c *gin.Context, publicAccess bool, serviceTenantID uint) bool {
	status := serviceAccessStatus(c, publicAccess, serviceTenantID)
	if status == 0 {
		return true
	}
	if status == http.StatusUnauthorized {
		c.JSON(status, gin.H{"error": "Authentication required"})
	} else {
		c.JSON(status, gin.H{"error": "Access denied"})
	}
	return false
}

type tileProtocol string

const (
	tileXYZ  tileProtocol = "xyz"
	tileWMTS tileProtocol = "wmts"
	tileOGC  tileProtocol = "ogc_tiles"
)

// publishedTileService resolves the current publication before serving metadata or bytes.
func publishedTileService(c *gin.Context, lookup tileServiceLookup, protocol tileProtocol) (*models.TileService, bool) {
	c.Header("Cache-Control", "private, no-cache")
	reject := func(status int, code, key string) (*models.TileService, bool) {
		message := commoni18n.T(c, key)
		if protocol == tileWMTS {
			c.XML(status, buildErrorXML(code, message))
		} else {
			c.JSON(status, gin.H{"error": message})
		}
		return nil, false
	}

	name := c.Param("serviceName")
	svc, err := lookup.GetServiceForConsumption(name)
	if errors.Is(err, commonapi.ErrNotFound) {
		return reject(http.StatusNotFound, "ServiceNotFound", servicei18n.MsgServiceNotFound)
	}
	if err != nil {
		logger.L().Error("读取瓦片服务失败", "service_name", name, "error", err)
		return reject(http.StatusInternalServerError, "InternalError", servicei18n.MsgServiceLookupFailed)
	}
	switch serviceAccessStatus(c, svc.PublicAccess, svc.TenantID) {
	case http.StatusUnauthorized:
		return reject(http.StatusUnauthorized, "Unauthorized", servicei18n.MsgAuthenticationRequired)
	case http.StatusForbidden:
		return reject(http.StatusForbidden, "Forbidden", commoni18n.MsgForbidden)
	}
	if svc.Status != "active" {
		return reject(http.StatusServiceUnavailable, "ServiceUnavailable", servicei18n.MsgServiceInactive)
	}
	if (protocol == tileOGC && !svc.IsOGCTilesEnabled()) || (protocol != tileOGC && !svc.IsXYZEnabled()) {
		return reject(http.StatusNotFound, "ServiceNotFound", servicei18n.MsgServiceNotFound)
	}

	return svc, true
}
