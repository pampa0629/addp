package audit

import (
	"log"
	"net/http"
	"time"

	"github.com/addp/common/client"
	sharedauth "github.com/addp/common/middleware/auth"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditMiddleware appends structured request events to System. Domain security
// facts remain the responsibility of the owning service transaction.
func AuditMiddleware(moduleName string, systemClient *client.SystemClient) gin.HandlerFunc {
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
		if entity := ParseEntityFromPath(method, path); entity != nil {
			request.EntityType, request.EntityID = entity.Type, entity.ID
		}
		if authContext, exists := sharedauth.AuthContextFromGin(c); exists {
			principalID, principalType := authContext.Principal.ID, authContext.Principal.Type
			contextType := authContext.Context.Type
			request.PrincipalID, request.PrincipalType, request.ContextType = &principalID, &principalType, &contextType
			request.TenantID = authContext.Context.TenantID
		}

		go func(logData *models.AuditLogCreateRequest) {
			if err := systemClient.CreateAuditLog(logData); err != nil {
				log.Printf("[%s] failed to append audit event %s: %v", moduleName, requestID, err)
			}
		}(request)
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
