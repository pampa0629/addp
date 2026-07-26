package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/logger"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMOAuthAuditWriter interface {
	Write(context.Context, iam.AuditEvent) error
}

type iamOAuthRequestAuditFacts struct {
	ClientID  string
	GrantType string
	Decision  string
	Scope     string
}

func NewIAMOAuthFailureAuditMiddleware(writer IAMOAuthAuditWriter) (gin.HandlerFunc, error) {
	if writer == nil {
		return nil, errors.New("IAM OAuth Audit Writer 不能为空")
	}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || !IsOAuthSecurityPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		facts := captureIAMOAuthAuditFacts(c)
		c.Next()
		if OAuthSecurityAuditWasPersisted(c) {
			return
		}

		audit := ResolveOAuthSecurityAudit(c)
		mergeIAMOAuthAuditFacts(&audit, facts)
		event := iam.AuditEvent{
			Metadata:   iamOAuthFailureMetadata(c),
			EventName:  audit.Event,
			Result:     iamOAuthAuditResult(audit.Result, c.Writer.Status()),
			RiskLevel:  iamOAuthAuditRisk(audit.Event),
			ModuleName: "system",
			EntityType: "oauth_security_event",
			EntityID:   audit.Event,
			Details:    iamOAuthAuditDetails(audit),
		}
		if err := writer.Write(c.Request.Context(), event); err != nil {
			logger.L().Error("写入 IAM OAuth 失败审计失败", "event", event.EventName, "error", err)
			return
		}
		MarkOAuthSecurityAuditPersisted(c)
	}, nil
}

func captureIAMOAuthAuditFacts(c *gin.Context) iamOAuthRequestAuditFacts {
	facts := iamOAuthRequestAuditFacts{}
	if strings.Contains(c.ContentType(), "application/x-www-form-urlencoded") ||
		strings.Contains(c.ContentType(), "multipart/form-data") {
		_ = c.Request.ParseForm()
		facts.ClientID = strings.TrimSpace(c.Request.PostForm.Get("client_id"))
		facts.GrantType = strings.TrimSpace(c.Request.PostForm.Get("grant_type"))
		facts.Scope = strings.TrimSpace(c.Request.PostForm.Get("scope"))
		return facts
	}
	if c.Request.Body == nil || !strings.Contains(c.ContentType(), "json") {
		return facts
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return facts
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var request struct {
		ClientID  string `json:"client_id"`
		GrantType string `json:"grant_type"`
		Decision  string `json:"decision"`
		Scope     string `json:"scope"`
		Approve   *bool  `json:"approve"`
	}
	if json.Unmarshal(body, &request) != nil {
		return facts
	}
	facts.ClientID = strings.TrimSpace(request.ClientID)
	facts.GrantType = strings.TrimSpace(request.GrantType)
	facts.Decision = strings.TrimSpace(request.Decision)
	facts.Scope = strings.TrimSpace(request.Scope)
	if facts.Decision == "" && request.Approve != nil {
		if *request.Approve {
			facts.Decision = "approve"
		} else {
			facts.Decision = "reject"
		}
	}
	return facts
}

func mergeIAMOAuthAuditFacts(audit *OAuthSecurityAudit, facts iamOAuthRequestAuditFacts) {
	if audit.ClientID == "" {
		audit.ClientID = facts.ClientID
	}
	if audit.GrantType == "" {
		audit.GrantType = facts.GrantType
	}
	if audit.Decision == "" {
		audit.Decision = facts.Decision
	}
	if audit.Scope == "" {
		audit.Scope = facts.Scope
	}
}

func iamOAuthFailureMetadata(c *gin.Context) iam.AuditMetadata {
	method := c.Request.Method
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	status := c.Writer.Status()
	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()
	metadata := iam.AuditMetadata{
		HTTPMethod:   &method,
		ResourcePath: &path,
		HTTPStatus:   &status,
		IPAddress:    &ipAddress,
		UserAgent:    &userAgent,
	}
	if requestID := requestidmiddleware.FromGinContext(c); requestID != "" {
		metadata.RequestID = &requestID
	}
	if authContext, exists := IAMAuthContextFromGin(c); exists {
		principalID, principalErr := strconv.ParseInt(authContext.Principal.ID, 10, 64)
		if principalErr == nil && principalID > 0 {
			principalType := iam.PrincipalType(authContext.Principal.Type)
			contextType := iam.ContextType(authContext.Context.Type)
			metadata.PrincipalID = &principalID
			metadata.PrincipalType = &principalType
			metadata.ContextType = &contextType
			if authContext.Context.TenantID != nil {
				if tenantID, err := strconv.ParseInt(*authContext.Context.TenantID, 10, 64); err == nil && tenantID > 0 {
					metadata.TenantID = &tenantID
				}
			}
		}
	}
	return metadata
}

func iamOAuthAuditResult(result string, status int) iam.AuditResult {
	switch result {
	case "denied", "rejected":
		return iam.AuditResultDenied
	case "ignored":
		return iam.AuditResultIgnored
	case "succeeded":
		return iam.AuditResultSucceeded
	default:
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return iam.AuditResultDenied
		}
		return iam.AuditResultFailed
	}
}

func iamOAuthAuditRisk(eventName string) iam.AuditRiskLevel {
	if eventName == "oauth.token.refresh_reuse_detected" {
		return iam.AuditRiskHigh
	}
	return iam.AuditRiskMedium
}

func iamOAuthAuditDetails(audit OAuthSecurityAudit) map[string]any {
	details := make(map[string]any)
	for key, value := range map[string]string{
		"client_id":  audit.ClientID,
		"grant_type": audit.GrantType,
		"decision":   audit.Decision,
		"scope":      audit.Scope,
		"error_code": audit.Error,
	} {
		if value != "" {
			details[key] = value
		}
	}
	return details
}
