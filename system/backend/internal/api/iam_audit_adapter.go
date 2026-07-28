package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMAuditEventResponse struct {
	ID            string                 `json:"id"`
	PrincipalID   *string                `json:"principal_id"`
	PrincipalType *iam.PrincipalType     `json:"principal_type"`
	ContextType   *iam.ContextType       `json:"context_type"`
	TenantID      *string                `json:"tenant_id"`
	EventName     string                 `json:"event_name"`
	Result        iam.AuditResult        `json:"result"`
	RiskLevel     iam.AuditRiskLevel     `json:"risk_level"`
	ModuleName    string                 `json:"module_name"`
	HTTPMethod    *string                `json:"http_method"`
	ResourcePath  *string                `json:"resource_path"`
	HTTPStatus    *int                   `json:"http_status"`
	RequestID     *string                `json:"request_id"`
	IPAddress     *string                `json:"ip_address"`
	UserAgent     *string                `json:"user_agent"`
	EntityType    *string                `json:"entity_type"`
	EntityID      *string                `json:"entity_id"`
	Details       map[string]interface{} `json:"details"`
	CreatedAt     time.Time              `json:"created_at"`
}

type IAMAuditTrendResponse struct {
	Date      string `json:"date"`
	Total     int64  `json:"total"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
	Denied    int64  `json:"denied"`
}

type iamAuditQueryService interface {
	List(context.Context, iam.AuditQuery, int, int) ([]iam.AuditLog, int64, error)
	Get(context.Context, int64, *int64) (*iam.AuditLog, error)
	Summary(context.Context, iam.AuditQuery) (*iam.AuditSummary, error)
	Trends(context.Context, iam.AuditQuery) ([]iam.AuditTrendPoint, error)
}

type IAMAuditHandler struct {
	service iamAuditQueryService
}

func NewIAMAuditHandler(service iamAuditQueryService) (*IAMAuditHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: audit query service is required", commonapi.ErrBadRequest)
	}
	return &IAMAuditHandler{service: service}, nil
}

// PlatformList godoc
// @Summary      查询平台审计事件 | List platform audit events
// @Tags         平台审计 | Platform Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {object} object{data=[]IAMAuditEventResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.event.read"]
// @Router       /platform/audit/events [get]
func (h *IAMAuditHandler) PlatformList(c *gin.Context) { h.list(c, false) }

// TenantList godoc
// @Summary      查询当前租户审计事件 | List current tenant audit events
// @Tags         租户审计 | Tenant Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {object} object{data=[]IAMAuditEventResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.read"]
// @Router       /tenant/audit/events [get]
func (h *IAMAuditHandler) TenantList(c *gin.Context) { h.list(c, true) }

func (h *IAMAuditHandler) list(c *gin.Context, tenantScoped bool) {
	query, err := auditQueryFromRequest(c, tenantScoped)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	logs, total, err := h.service.List(c.Request.Context(), query, page, pageSize)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMAuditEventResponse, 0, len(logs))
	for _, log := range logs {
		mapped, err := mapIAMAuditLog(log)
		if err != nil {
			respondIAMError(c, err)
			return
		}
		responses = append(responses, mapped)
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// PlatformGet godoc
// @Summary      查询平台审计事件详情 | Get platform audit event
// @Tags         平台审计 | Platform Audit
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "审计事件 ID | Audit event ID"
// @Success      200 {object} IAMAuditEventResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.event.read"]
// @Router       /platform/audit/events/{id} [get]
func (h *IAMAuditHandler) PlatformGet(c *gin.Context) { h.get(c, false) }

// TenantGet godoc
// @Summary      查询当前租户审计事件详情 | Get current tenant audit event
// @Tags         租户审计 | Tenant Audit
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "审计事件 ID | Audit event ID"
// @Success      200 {object} IAMAuditEventResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.read"]
// @Router       /tenant/audit/events/{id} [get]
func (h *IAMAuditHandler) TenantGet(c *gin.Context) { h.get(c, true) }

func (h *IAMAuditHandler) get(c *gin.Context, tenantScoped bool) {
	auditID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	tenantID, err := auditTenantScope(c, tenantScoped)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	log, err := h.service.Get(c.Request.Context(), auditID, tenantID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response, err := mapIAMAuditLog(*log)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// PlatformSummary godoc
// @Summary      汇总平台审计事件 | Summarize platform audit events
// @Tags         平台审计 | Platform Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {object} iam.AuditSummary
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.event.read"]
// @Router       /platform/audit/events/summary [get]
func (h *IAMAuditHandler) PlatformSummary(c *gin.Context) { h.summary(c, false) }

// TenantSummary godoc
// @Summary      汇总当前租户审计事件 | Summarize current tenant audit events
// @Tags         租户审计 | Tenant Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {object} iam.AuditSummary
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.read"]
// @Router       /tenant/audit/events/summary [get]
func (h *IAMAuditHandler) TenantSummary(c *gin.Context) { h.summary(c, true) }

func (h *IAMAuditHandler) summary(c *gin.Context, tenantScoped bool) {
	query, err := auditQueryFromRequest(c, tenantScoped)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	summary, err := h.service.Summary(c.Request.Context(), query)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// PlatformTrends godoc
// @Summary      查询平台审计趋势 | Get platform audit trends
// @Tags         平台审计 | Platform Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {array} IAMAuditTrendResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.event.read"]
// @Router       /platform/audit/events/trends [get]
func (h *IAMAuditHandler) PlatformTrends(c *gin.Context) { h.trends(c, false) }

// TenantTrends godoc
// @Summary      查询当前租户审计趋势 | Get current tenant audit trends
// @Tags         租户审计 | Tenant Audit
// @Produce      json
// @Security     BearerAuth
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {array} IAMAuditTrendResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.read"]
// @Router       /tenant/audit/events/trends [get]
func (h *IAMAuditHandler) TenantTrends(c *gin.Context) { h.trends(c, true) }

func (h *IAMAuditHandler) trends(c *gin.Context, tenantScoped bool) {
	query, err := auditQueryFromRequest(c, tenantScoped)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	points, err := h.service.Trends(c.Request.Context(), query)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response := make([]IAMAuditTrendResponse, 0, len(points))
	for _, point := range points {
		response = append(response, IAMAuditTrendResponse{
			Date: point.Date.UTC().Format("2006-01-02"), Total: point.Total,
			Succeeded: point.Succeeded, Failed: point.Failed, Denied: point.Denied,
		})
	}
	c.JSON(http.StatusOK, response)
}

// PlatformExport godoc
// @Summary      导出平台审计事件 | Export platform audit events
// @Tags         平台审计 | Platform Audit
// @Produce      json,text/csv
// @Security     BearerAuth
// @Param        format query string false "csv 或 json | csv or json"
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {array} IAMAuditEventResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.event.export"]
// @Router       /platform/audit/events/export [get]
func (h *IAMAuditHandler) PlatformExport(c *gin.Context) { h.export(c, false) }

// TenantExport godoc
// @Summary      导出当前租户审计事件 | Export current tenant audit events
// @Tags         租户审计 | Tenant Audit
// @Produce      json,text/csv
// @Security     BearerAuth
// @Param        format query string false "csv 或 json | csv or json"
// @Param        entity_type query string false "实体类型 | Entity type"
// @Param        entity_id query string false "实体 ID | Entity ID"
// @Success      200 {array} IAMAuditEventResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["audit.tenant_event.export"]
// @Router       /tenant/audit/events/export [get]
func (h *IAMAuditHandler) TenantExport(c *gin.Context) { h.export(c, true) }

func (h *IAMAuditHandler) export(c *gin.Context, tenantScoped bool) {
	query, err := auditQueryFromRequest(c, tenantScoped)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	format := c.DefaultQuery("format", "csv")
	if format != "csv" && format != "json" {
		respondIAMError(c, fmt.Errorf("%w: export format must be csv or json", commonapi.ErrBadRequest))
		return
	}
	logs, _, err := h.service.List(c.Request.Context(), query, 1, 10000)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMAuditEventResponse, 0, len(logs))
	for _, log := range logs {
		mapped, err := mapIAMAuditLog(log)
		if err != nil {
			respondIAMError(c, err)
			return
		}
		responses = append(responses, mapped)
	}
	filename := "audit_events_" + time.Now().UTC().Format("20060102_150405") + "." + format
	c.Header("Content-Disposition", "attachment; filename="+filename)
	if format == "json" {
		c.JSON(http.StatusOK, responses)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()
	if err := writer.Write([]string{
		"id", "principal_id", "principal_type", "context_type", "tenant_id", "event_name",
		"result", "risk_level", "module_name", "request_id", "created_at",
	}); err != nil {
		return
	}
	for _, event := range responses {
		if err := writer.Write([]string{
			event.ID, stringValue(event.PrincipalID), principalTypeValue(event.PrincipalType),
			contextTypeValue(event.ContextType), stringValue(event.TenantID), event.EventName,
			string(event.Result), string(event.RiskLevel), event.ModuleName,
			stringValue(event.RequestID), event.CreatedAt.Format(time.RFC3339Nano),
		}); err != nil {
			return
		}
	}
}

func auditQueryFromRequest(c *gin.Context, tenantScoped bool) (iam.AuditQuery, error) {
	tenantID, err := auditTenantScope(c, tenantScoped)
	if err != nil {
		return iam.AuditQuery{}, err
	}
	startTime, err := parseOptionalRFC3339(c.Query("start_time"))
	if err != nil {
		return iam.AuditQuery{}, err
	}
	endTime, err := parseOptionalRFC3339(c.Query("end_time"))
	if err != nil {
		return iam.AuditQuery{}, err
	}
	var principalID *int64
	if value := strings.TrimSpace(c.Query("principal_id")); value != "" {
		parsed, err := parseIAMDecimalID(value)
		if err != nil {
			return iam.AuditQuery{}, err
		}
		principalID = &parsed
	}
	return iam.AuditQuery{
		TenantID: tenantID, StartTime: startTime, EndTime: endTime,
		EventName: strings.TrimSpace(c.Query("event_name")), Result: strings.TrimSpace(c.Query("result")),
		RiskLevel: strings.TrimSpace(c.Query("risk_level")), ModuleName: strings.TrimSpace(c.Query("module_name")),
		PrincipalID: principalID, RequestID: strings.TrimSpace(c.Query("request_id")),
		EntityType: strings.TrimSpace(c.Query("entity_type")), EntityID: strings.TrimSpace(c.Query("entity_id")),
	}, nil
}

func auditTenantScope(c *gin.Context, tenantScoped bool) (*int64, error) {
	if !tenantScoped {
		return nil, nil
	}
	if _, exists := c.GetQuery("tenant_id"); exists {
		return nil, fmt.Errorf("%w: tenant_id is derived from AuthContext", commonapi.ErrBadRequest)
	}
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		return nil, err
	}
	value := int64(tenantID)
	return &value, nil
}

func parseOptionalRFC3339(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid RFC3339 time", commonapi.ErrBadRequest)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func mapIAMAuditLog(log iam.AuditLog) (IAMAuditEventResponse, error) {
	details := map[string]interface{}{}
	if len(log.Details) > 0 {
		if err := json.Unmarshal(log.Details, &details); err != nil {
			return IAMAuditEventResponse{}, fmt.Errorf("decode audit details: %w", err)
		}
	}
	response := IAMAuditEventResponse{
		ID: strconv.FormatInt(log.ID, 10), PrincipalType: log.PrincipalType,
		ContextType: log.ContextType, EventName: log.EventName, Result: log.Result,
		RiskLevel: log.RiskLevel, ModuleName: log.ModuleName, HTTPMethod: log.HTTPMethod,
		ResourcePath: log.ResourcePath, HTTPStatus: log.HTTPStatus, RequestID: log.RequestID,
		IPAddress: log.IPAddress, UserAgent: log.UserAgent, EntityType: log.EntityType,
		EntityID: log.EntityID, Details: details, CreatedAt: log.CreatedAt.UTC(),
	}
	if log.PrincipalID != nil {
		value := strconv.FormatInt(*log.PrincipalID, 10)
		response.PrincipalID = &value
	}
	if log.TenantID != nil {
		value := strconv.FormatInt(*log.TenantID, 10)
		response.TenantID = &value
	}
	return response, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func principalTypeValue(value *iam.PrincipalType) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func contextTypeValue(value *iam.ContextType) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
