package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type ontologyRuleFactsHandler struct {
	query  *QueryServiceHandler
	system *client.SystemServiceClient
}

// ReadOntologyRuleFacts godoc
// @Summary 读取已授权的单记录规则事实 | Read authorized single-record rule facts
// @Tags QueryService
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param service_name path string true "固定查询服务名称 | Pinned query service name"
// @Param request body authorization.OntologyRuleFactRequest true "已发布规则的单记录绑定 | Single-record binding from a published rule"
// @Success 200 {object} models.QueryExecutionResult
// @Failure 400,401,403,409,503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.ontology_fact.read"]
// @Router /runtime/ontology-facts/{service_name} [post]
func (h ontologyRuleFactsHandler) ReadOntologyRuleFacts(c *gin.Context) {
	commonapi.NoStore(c)
	ensureQueryRequestID(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var in authorization.OntologyRuleFactRequest
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &in) != nil || in.Call.Evaluation.Digest() == "" ||
		in.ServiceID == 0 || uint64(uint(in.ServiceID)) != in.ServiceID || len(in.ServiceVersion) != 64 || in.IDParameter == "" || len(in.Fields) < 2 || len(in.Fields) > 17 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams), "error_code": "invalid_query_request"})
		return
	}
	tenant := tenantIDValue(c)
	roles, err := h.system.WithTenantID(tenant).ConsumeOntologyRuleEvaluation(c.Request.Context(), in.Call)
	if err != nil {
		var denied *client.SystemAPIError
		if errors.As(err, &denied) && (denied.StatusCode == 400 || denied.StatusCode == 401 || denied.StatusCode == 403 || denied.StatusCode == 409) {
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		} else {
			c.Status(http.StatusServiceUnavailable)
		}
		return
	}
	queryService, err := h.query.svc.GetServiceModelByName(c.Param("service_name"), tenant)
	if err != nil || queryService.ID != uint(in.ServiceID) || queryService.Status != "active" || queryService.PublicAccess || !queryService.IsRESTAPIEnabled() {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	request := &models.QueryExecutionRequest{Parameters: map[string]any{in.IDParameter: in.Call.Evaluation.ObjectID},
		Select: in.Fields, Page: models.QueryPageRequest{Limit: 2}, Format: "json", RequireUntransformed: true,
		ExpectedServiceID: uint(in.ServiceID), ExpectedServiceVersion: in.ServiceVersion}
	audit := &queryExecutionAuditState{service: queryService, request: request, intent: "query", serviceVersion: svc.QueryServiceVersion(queryService), ontologyCall: &in.Call}
	defer h.query.writeQueryExecutionAudit(c, audit)
	ctx, cancel := context.WithDeadline(svc.WithOntologyFactRoles(c.Request.Context(), roles), roles.ExpiresAt)
	defer cancel()
	if ctx.Err() != nil {
		audit.errorCode = "permission_denied"
		c.Status(http.StatusForbidden)
		return
	}
	result, err := h.query.executorSvc.ExecuteQuery(ctx, queryService, request)
	if err != nil || !roles.ExpiresAt.After(time.Now()) {
		audit.errorCode = "query_execution_failed"
		c.Status(http.StatusServiceUnavailable)
		return
	}
	audit.result, audit.rowCount, audit.hasMore = "succeeded", len(result.Data), result.Page.HasMore
	c.JSON(http.StatusOK, result)
}
