package api

import (
	"context"
	commonauth "github.com/addp/common/middleware/auth"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Diagnostic reads are audited independently from the evidence being read.
// Neither request query strings nor response bodies enter the audit payload.
func auditExecutionRead(c *gin.Context, system *client.SystemServiceClient, tenantID int, principalID int64) {
	if system == nil {
		return
	}
	status := c.Writer.Status()
	result := "succeeded"
	if status >= 500 {
		result = "failed"
	} else if status >= 400 {
		result = "denied"
	}
	path, method := c.Request.URL.Path, c.Request.Method
	id := c.Param("execution_id")
	if id == "" {
		id = c.Param("id")
	}
	if id == "" {
		id = "collection"
	}
	request := &models.AuditLogCreateRequest{EventName: "execution.diagnostics.read", ModuleName: "monitor", Result: result, RiskLevel: "medium",
		EntityType: "task_execution", EntityID: id, HTTPMethod: &method, ResourcePath: &path, HTTPStatus: &status,
		Details: map[string]interface{}{"source_principal_id": strconv.FormatInt(principalID, 10), "source_principal_type": "user"}}
	requestID := uuid.NewString()
	request.RequestID = &requestID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := system.WithTenantID(uint(tenantID)).AppendTenantAuditEvent(ctx, request); err != nil {
			log.Printf("monitor execution read audit append failed")
		}
	}()
}

func executionReadAuditMiddleware(system *client.SystemServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		identity, valid := commonauth.AuthContextFromGin(c)
		tenant, tenantOK := commonauth.TenantIDFromGin(c)
		principal, principalOK := commonauth.PrincipalIDFromGin(c)
		if c.Request.Method == http.MethodGet && valid && identity.Principal.Type == "user" && tenantOK && principalOK && (strings.HasPrefix(route, "/api/v1/monitor/executions") || route == "/api/v1/monitor/alerts") {
			defer auditExecutionRead(c, system, int(tenant), principal)
		}
		c.Next()
	}
}
