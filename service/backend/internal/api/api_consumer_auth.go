package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	commonclient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

const apiConsumerContextKey = "api_consumer_info"

func optionalAPIConsumerAuth(systemClient *commonclient.SystemServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		credential := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if credential == "" {
			c.Next()
			return
		}
		if strings.TrimSpace(c.GetHeader("Authorization")) != "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":      "Authorization and X-API-Key cannot be used together",
				"error_code": "ambiguous_credentials",
			})
			return
		}
		if systemClient == nil || !strings.HasPrefix(credential, "addp_api_") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":      "Invalid API consumer credential",
				"error_code": "api_consumer_credential_invalid",
			})
			return
		}
		digest := sha256.Sum256([]byte(credential))
		info, err := systemClient.ValidateAPIConsumerCredential(c.Request.Context(), hex.EncodeToString(digest[:]))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":      "API consumer authentication unavailable",
				"error_code": "api_consumer_auth_unavailable",
			})
			return
		}
		if !info.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":      "API consumer credential is invalid or revoked",
				"error_code": "api_consumer_credential_invalid",
			})
			return
		}
		c.Set(apiConsumerContextKey, info)
		c.Next()
	}
}

func apiConsumerServiceAccessStatus(
	c *gin.Context,
	serviceTenantID uint,
	serviceType string,
	serviceID uint,
) (int, bool) {
	raw, exists := c.Get(apiConsumerContextKey)
	if !exists {
		return 0, false
	}
	info, ok := raw.(*commonclient.APIConsumerCredentialValidationResponse)
	if !ok || info == nil || !info.Valid {
		return http.StatusUnauthorized, true
	}
	if info.TenantID != serviceTenantID {
		return http.StatusForbidden, true
	}
	for _, grant := range info.ServiceGrants {
		if grant.ServiceType == serviceType && grant.ServiceID == serviceID {
			return 0, true
		}
	}
	return http.StatusForbidden, true
}
