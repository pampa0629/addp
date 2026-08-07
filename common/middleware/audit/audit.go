package audit

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/client"
	sharedauth "github.com/addp/common/middleware/auth"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ServiceAuditMiddleware appends tenant request audit events with the caller's
// service identity. System derives Principal and Tenant facts from the Service
// Access Token and ignores identity fields supplied by this middleware.
func ServiceAuditMiddleware(moduleName string, systemClient *client.SystemServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		requestID := requestidmiddleware.FromGinContext(c)
		if requestID == "" {
			requestID = uuid.NewString()
			c.Set("request_id", requestID)
		}
		c.Next()
		if c.Request.Method == http.MethodGet || systemClient == nil {
			return
		}
		authContext, exists := sharedauth.AuthContextFromGin(c)
		if !exists || authContext.Context.Type != "tenant" || authContext.Context.TenantID == nil {
			return
		}
		tenantID64, err := strconv.ParseUint(*authContext.Context.TenantID, 10, 0)
		if err != nil || tenantID64 == 0 {
			return
		}

		method := c.Request.Method
		path := c.Request.URL.Path
		status := c.Writer.Status()
		ipAddress := c.ClientIP()
		userAgent := c.Request.UserAgent()
		result, risk := requestOutcome(status)
		request := &models.AuditLogCreateRequest{
			EventName: "http.request.completed", Result: result, RiskLevel: risk, ModuleName: moduleName,
			HTTPMethod: &method, ResourcePath: &path, HTTPStatus: &status, RequestID: &requestID,
			IPAddress: &ipAddress, UserAgent: &userAgent,
			EntityType: "http_request", EntityID: requestID,
			Details: map[string]any{"duration_ms": time.Since(startedAt).Milliseconds()},
		}
		request.Details["source_principal_id"] = authContext.Principal.ID
		request.Details["source_principal_type"] = authContext.Principal.Type
		if authContext.Context.TenantMembershipID != nil {
			request.Details["source_tenant_membership_id"] = *authContext.Context.TenantMembershipID
		}
		if entity := ParseEntityFromPath(method, path); entity != nil {
			request.EntityType, request.EntityID = entity.Type, entity.ID
		}

		go func(tenantID uint, logData *models.AuditLogCreateRequest) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := systemClient.WithTenantID(tenantID).AppendTenantAuditEvent(ctx, logData); err != nil {
				log.Printf("[%s] failed to append service audit event %s: %v", moduleName, requestID, err)
			}
		}(uint(tenantID64), request)
	}
}

func requestOutcome(httpStatus int) (string, string) {
	switch {
	case httpStatus >= 500:
		return "failed", "high"
	case httpStatus >= 400:
		return "denied", "medium"
	default:
		return "succeeded", "medium"
	}
}
