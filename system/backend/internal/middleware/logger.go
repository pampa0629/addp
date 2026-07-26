package middleware

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type RequestAuditWriter interface {
	Write(context.Context, iam.AuditEvent) error
}

func LoggerMiddleware(writer RequestAuditWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.Contains(c.Request.URL.Path, "/internal/") || c.Request.Method == "GET" {
			c.Next()
			return
		}

		startedAt := time.Now()
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = uuid.NewString()
			c.Set("request_id", requestID)
		}
		c.Next()
		if writer == nil || (IsOAuthSecurityPath(c.Request.URL.Path) && OAuthSecurityAuditWasPersisted(c)) {
			return
		}

		event := requestAuditEvent(c, requestID, time.Since(startedAt))
		if err := writer.Write(c.Request.Context(), event); err != nil {
			logger.L().Error("写入请求审计事件失败", "error", err, "event", event.EventName, "request_id", requestID)
		}
	}
}

func requestAuditEvent(c *gin.Context, requestID string, duration time.Duration) iam.AuditEvent {
	method := c.Request.Method
	path := c.Request.URL.Path
	status := c.Writer.Status()
	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()
	metadata := iam.AuditMetadata{
		HTTPMethod: &method, ResourcePath: &path, HTTPStatus: &status, RequestID: &requestID,
		IPAddress: &ipAddress, UserAgent: &userAgent,
	}
	if authContext, exists := IAMAuthContextFromGin(c); exists {
		if principalID, err := strconv.ParseInt(authContext.Principal.ID, 10, 64); err == nil && principalID > 0 {
			principalType := iam.PrincipalType(authContext.Principal.Type)
			metadata.PrincipalID, metadata.PrincipalType = &principalID, &principalType
		}
		contextType := iam.ContextType(authContext.Context.Type)
		metadata.ContextType = &contextType
		if authContext.Context.TenantID != nil {
			if tenantID, err := strconv.ParseInt(*authContext.Context.TenantID, 10, 64); err == nil && tenantID > 0 {
				metadata.TenantID = &tenantID
			}
		}
	}

	result := iam.AuditResultSucceeded
	risk := iam.AuditRiskMedium
	if status >= 500 {
		result, risk = iam.AuditResultFailed, iam.AuditRiskHigh
	} else if status >= 400 {
		result = iam.AuditResultDenied
	}
	event := iam.AuditEvent{
		Metadata: metadata, EventName: "http.request.completed", Result: result, RiskLevel: risk,
		ModuleName: "system", EntityType: "http_request", EntityID: requestID,
		Details: map[string]any{"duration_ms": duration.Milliseconds()},
	}
	if IsOAuthSecurityPath(path) {
		oauthAudit := ResolveOAuthSecurityAudit(c)
		event.EventName = oauthAudit.Event
		event.EntityType = "oauth_security_event"
		event.EntityID = oauthAudit.Event
		event.Result = oauthAuditResult(oauthAudit.Result)
		event.Details = map[string]any{}
		for key, value := range map[string]string{
			"client_id": oauthAudit.ClientID, "grant_type": oauthAudit.GrantType,
			"decision": oauthAudit.Decision, "scope": oauthAudit.Scope, "error_code": oauthAudit.Error,
		} {
			if value != "" {
				event.Details[key] = value
			}
		}
		if oauthAudit.Event == "oauth.token.refresh_reuse_detected" {
			event.RiskLevel = iam.AuditRiskHigh
		}
	}
	return event
}

func oauthAuditResult(result string) iam.AuditResult {
	switch result {
	case "succeeded":
		return iam.AuditResultSucceeded
	case "ignored":
		return iam.AuditResultIgnored
	case "denied", "rejected":
		return iam.AuditResultDenied
	default:
		return iam.AuditResultFailed
	}
}
