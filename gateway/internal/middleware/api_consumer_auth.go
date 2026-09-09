package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/addp/gateway/internal/cache"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
)

type APIConsumerAuthMiddleware struct {
	systemClient *client.SystemClient
	localCache   *cache.LocalCache
}

func NewAPIConsumerAuthMiddleware(
	systemClient *client.SystemClient,
	localCache *cache.LocalCache,
) *APIConsumerAuthMiddleware {
	return &APIConsumerAuthMiddleware{systemClient: systemClient, localCache: localCache}
}

func (m *APIConsumerAuthMiddleware) Handler() gin.HandlerFunc {
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
		if !strings.HasPrefix(credential, "addp_api_") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid API consumer credential", "error_code": "api_consumer_credential_invalid",
			})
			return
		}
		digest := sha256.Sum256([]byte(credential))
		keyHash := hex.EncodeToString(digest[:])
		info, err := m.validateWithCache(keyHash)
		if err != nil {
			log.Printf("API consumer credential validation failed: %v", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "API consumer authentication unavailable", "error_code": "api_consumer_auth_unavailable",
			})
			return
		}
		if !info.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "API consumer credential is invalid or revoked", "error_code": "api_consumer_credential_invalid",
			})
			return
		}
		c.Set("api_consumer_info", info)
		c.Set("api_consumer_id", info.APIConsumerID)
		c.Set("api_consumer_name", info.APIConsumerName)
		c.Set("api_credential_prefix", apiCredentialLogPrefix(credential))
		c.Next()
	}
}

func (m *APIConsumerAuthMiddleware) validateWithCache(
	keyHash string,
) (*client.APIConsumerCredentialValidationResponse, error) {
	if info := m.localCache.Get(keyHash); info != nil {
		return info, nil
	}
	info, err := m.systemClient.ValidateAPIConsumerCredential(keyHash)
	if err != nil {
		return nil, fmt.Errorf("validate with System: %w", err)
	}
	if info.Valid {
		m.localCache.Set(keyHash, info)
	}
	return info, nil
}

func apiCredentialLogPrefix(credential string) string {
	const maxPrefixLength = 12
	if len(credential) <= maxPrefixLength {
		return ""
	}
	return credential[:maxPrefixLength]
}

func RejectAPIConsumerCredentialOnControlPlane() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader("X-API-Key")) != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":      "API consumer credentials cannot access control-plane APIs",
				"error_code": "api_consumer_control_plane_denied",
			})
			return
		}
		c.Next()
	}
}
