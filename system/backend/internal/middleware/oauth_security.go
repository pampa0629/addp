package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonratelimit "github.com/addp/common/middleware/ratelimit"
	"github.com/gin-gonic/gin"
	redisv9 "github.com/redis/go-redis/v9"
)

const oauthSecurityAuditKey = "oauth_security_audit"

type OAuthSecurityAudit struct {
	Event     string `json:"event"`
	Result    string `json:"result"`
	ClientID  string `json:"client_id,omitempty"`
	GrantType string `json:"grant_type,omitempty"`
	Decision  string `json:"decision,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Error     string `json:"error,omitempty"`
}

func SetOAuthSecurityAudit(c *gin.Context, event, result, clientID, grantType, decision, scope, errorCode string) {
	c.Set(oauthSecurityAuditKey, OAuthSecurityAudit{
		Event: event, Result: result, ClientID: strings.TrimSpace(clientID), GrantType: strings.TrimSpace(grantType),
		Decision: strings.TrimSpace(decision), Scope: strings.TrimSpace(scope), Error: strings.TrimSpace(errorCode),
	})
}

func OAuthSecurityAuditFromContext(c *gin.Context) (OAuthSecurityAudit, bool) {
	value, ok := c.Get(oauthSecurityAuditKey)
	if !ok {
		return OAuthSecurityAudit{}, false
	}
	audit, ok := value.(OAuthSecurityAudit)
	return audit, ok
}

func OAuthSecurityAuditJSON(c *gin.Context) string {
	audit := ResolveOAuthSecurityAudit(c)
	body, _ := json.Marshal(audit)
	return string(body)
}

func ResolveOAuthSecurityAudit(c *gin.Context) OAuthSecurityAudit {
	if audit, ok := OAuthSecurityAuditFromContext(c); ok {
		return audit
	}
	audit := OAuthSecurityAudit{Event: oauthFailureEvent(c.Request.URL.Path), Result: "failed"}
	if c.Writer.Status() == http.StatusUnauthorized {
		audit.Error = "authentication_required"
	}
	return audit
}

func IsOAuthSecurityPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/system/oauth/")
}

func oauthFailureEvent(path string) string {
	switch {
	case strings.HasSuffix(path, "/authorizations") && strings.Contains(path, "/device/"):
		return "oauth.device.authorization.failed"
	case strings.HasSuffix(path, "/authorizations"):
		return "oauth.authorization.failed"
	case strings.HasSuffix(path, "/device/code"):
		return "oauth.device.code.failed"
	case strings.HasSuffix(path, "/token"):
		return "oauth.token.failed"
	case strings.HasSuffix(path, "/revoke"):
		return "oauth.token.revoke_ignored"
	default:
		return "oauth.request.failed"
	}
}

func OAuthPublicRateLimitMiddleware(client redisv9.Scripter, limit int64) gin.HandlerFunc {
	return newOAuthRateLimitMiddleware(client, limit, func(c *gin.Context) string {
		return c.FullPath() + "|" + c.ClientIP() + "|" + oauthRequestClientID(c)
	})
}

func OAuthUserRateLimitMiddleware(client redisv9.Scripter, limit int64) gin.HandlerFunc {
	return newOAuthRateLimitMiddleware(client, limit, func(c *gin.Context) string {
		return c.FullPath() + "|" + strconv.FormatUint(uint64(c.GetUint("user_id")), 10) + "|" + oauthRequestClientID(c)
	})
}

func newOAuthRateLimitMiddleware(client redisv9.Scripter, limit int64, keyGetter func(*gin.Context) string) gin.HandlerFunc {
	middleware, err := commonratelimit.RedisFixedWindowMiddleware(client, commonratelimit.RedisFixedWindowOptions{
		Prefix: "addp:oauth:rate", Period: time.Minute, Limit: limit, KeyGetter: keyGetter,
		OnLimitReached: func(c *gin.Context, _ int64) {
			SetOAuthSecurityAudit(c, "oauth.rate_limit.exceeded", "rejected", oauthRequestClientID(c), c.PostForm("grant_type"), "", c.PostForm("scope"), "temporarily_unavailable")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "temporarily_unavailable"})
			c.Abort()
		},
		OnUnavailable: func(c *gin.Context, _ error) {
			SetOAuthSecurityAudit(c, "oauth.rate_limit.unavailable", "failed", oauthRequestClientID(c), c.PostForm("grant_type"), "", c.PostForm("scope"), "temporarily_unavailable")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
			c.Abort()
		},
	})
	if err != nil {
		panic(err)
	}
	return middleware
}

func oauthRequestClientID(c *gin.Context) string {
	if clientID := strings.TrimSpace(c.PostForm("client_id")); clientID != "" {
		return clientID
	}
	if c.Request.Body == nil || !strings.Contains(c.ContentType(), "json") {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var request struct {
		ClientID string `json:"client_id"`
	}
	if json.Unmarshal(body, &request) != nil {
		return ""
	}
	return strings.TrimSpace(request.ClientID)
}
