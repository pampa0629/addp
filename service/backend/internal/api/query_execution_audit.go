package api

import (
	"context"
	"log"
	"net/http"
	"strconv"

	commonclient "github.com/addp/common/client"
	authmiddleware "github.com/addp/common/middleware/auth"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	commonmodels "github.com/addp/common/models"
	servicemodels "github.com/addp/service/internal/models"
	serviceinternal "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type QueryExecutionAuditWriter interface {
	WriteQueryExecutionAudit(ctx context.Context, tenantID uint, event *commonmodels.AuditLogCreateRequest) error
}

type systemQueryExecutionAuditWriter struct {
	client *commonclient.SystemServiceClient
}

func NewQueryExecutionAuditWriter(client *commonclient.SystemServiceClient) QueryExecutionAuditWriter {
	if client == nil {
		return nil
	}
	return &systemQueryExecutionAuditWriter{client: client}
}

func (writer *systemQueryExecutionAuditWriter) WriteQueryExecutionAudit(
	ctx context.Context,
	tenantID uint,
	event *commonmodels.AuditLogCreateRequest,
) error {
	return writer.client.WithTenantID(tenantID).AppendTenantAuditEvent(ctx, event)
}

type queryExecutionAuditState struct {
	service        *servicemodels.QueryService
	request        *servicemodels.QueryExecutionRequest
	intent         string
	result         string
	errorCode      string
	serviceVersion string
	rowCount       int
	hasMore        bool
}

func (h *QueryServiceHandler) writeQueryExecutionAudit(c *gin.Context, state *queryExecutionAuditState) {
	if h.executionAuditWriter == nil || state == nil || state.service == nil {
		return
	}
	requestID := requestidmiddleware.FromGinContext(c)
	if requestID == "" {
		requestID = c.GetHeader(requestidmiddleware.RequestIDHeader)
	}
	if requestID == "" {
		requestID = uuid.NewString()
		c.Set("request_id", requestID)
		c.Header(requestidmiddleware.RequestIDHeader, requestID)
	}
	method, path, status := c.Request.Method, c.Request.URL.Path, c.Writer.Status()
	ipAddress, userAgent := c.ClientIP(), c.Request.UserAgent()
	result := state.result
	if result == "" {
		if status >= http.StatusInternalServerError {
			result = "failed"
		} else if status >= http.StatusBadRequest {
			result = "denied"
		} else {
			result = "succeeded"
		}
	}
	shapeFingerprint, err := serviceinternal.QueryShapeFingerprint(state.request)
	if err != nil {
		log.Printf("[service] failed to build query shape audit fingerprint: %v", err)
	}
	details := map[string]any{
		"service_type": "query", "service_id": state.service.ID,
		"service_version": state.serviceVersion, "query_intent": state.intent,
		"result_format": queryAuditFormat(state.request), "returned_count": state.rowCount,
		"has_more": state.hasMore, "query_shape_fingerprint": shapeFingerprint,
		"error_code": state.errorCode,
	}
	if authContext, exists := authmiddleware.AuthContextFromGin(c); exists {
		details["source_principal_id"] = authContext.Principal.ID
		details["source_principal_type"] = authContext.Principal.Type
		if authContext.Context.TenantID != nil {
			details["source_tenant_id"] = *authContext.Context.TenantID
		}
	} else {
		details["source_principal_type"] = "anonymous"
	}
	eventName := "service.query.executed"
	if state.intent == "export" {
		eventName = "service.query.exported"
	}
	event := &commonmodels.AuditLogCreateRequest{
		EventName: eventName, Result: result, RiskLevel: "medium", ModuleName: "service",
		HTTPMethod: &method, ResourcePath: &path, HTTPStatus: &status, RequestID: &requestID,
		IPAddress: &ipAddress, UserAgent: &userAgent,
		EntityType: "query_service", EntityID: strconv.FormatUint(uint64(state.service.ID), 10),
		Details: details,
	}
	if err := h.executionAuditWriter.WriteQueryExecutionAudit(c.Request.Context(), state.service.TenantID, event); err != nil {
		log.Printf("[service] failed to append query execution audit event: %v", err)
	}
}

func ensureQueryRequestID(c *gin.Context) string {
	requestID := requestidmiddleware.FromGinContext(c)
	if requestID == "" {
		requestID = c.GetHeader(requestidmiddleware.RequestIDHeader)
	}
	if requestID == "" {
		requestID = uuid.NewString()
		c.Set("request_id", requestID)
	}
	c.Header(requestidmiddleware.RequestIDHeader, requestID)
	return requestID
}

func queryAuditFormat(request *servicemodels.QueryExecutionRequest) string {
	if request == nil {
		return ""
	}
	return request.Format
}
