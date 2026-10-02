package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const logRetryAuditKey = "platform_log_retry_audit"

type logRetryAuditFacts struct {
	DeliveryID, EventID string
	DestinationID       uint
	Before, After       int
}

func logPipelineAudit(system *client.SystemServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		route := c.FullPath()
		if system == nil || !strings.HasPrefix(route, "/api/v1/monitor/platform/log-") || route == "/api/v1/monitor/platform/log-observations" {
			return
		}
		identity, ok := commonauth.AuthContextFromGin(c)
		if !ok || identity.Context.Type != "platform" || identity.Principal.Type != "user" {
			return
		}
		status := c.Writer.Status()
		result := "succeeded"
		if status >= 500 {
			result = "failed"
		} else if status >= 400 {
			result = "denied"
		}
		event := "platform.log_pipeline.manage"
		if c.Request.Method == http.MethodGet {
			event = "platform.log_pipeline.read"
		}
		path, method, id := c.Request.URL.Path, c.Request.Method, uuid.NewString()
		request := &models.AuditLogCreateRequest{EventName: event, ModuleName: "monitor", Result: result, RiskLevel: "medium", EntityType: "log_pipeline", EntityID: "platform", ResourcePath: &path, HTTPMethod: &method, HTTPStatus: &status, RequestID: &id, Details: map[string]any{"source_principal_id": identity.Principal.ID, "source_principal_type": "user"}}
		if strings.HasSuffix(route, "/log-notification-deliveries/:id/retry") {
			request.Details["operation"] = "retry"
			if parsed, err := uuid.Parse(c.Param("id")); err == nil && parsed.String() == c.Param("id") {
				request.EntityType = "platform_log_delivery"
				request.EntityID = parsed.String()
			}
			if value, ok := c.Get(logRetryAuditKey); ok {
				facts := value.(logRetryAuditFacts)
				request.Details["delivery_id"] = facts.DeliveryID
				request.Details["event_id"] = facts.EventID
				request.Details["destination_id"] = facts.DestinationID
				request.Details["manual_retry_count_before"] = facts.Before
				request.Details["manual_retry_count_after"] = facts.After
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := system.AppendPlatformAuditEvent(ctx, request); err != nil {
			log.Print("platform log operation audit append failed")
		}
	}
}
