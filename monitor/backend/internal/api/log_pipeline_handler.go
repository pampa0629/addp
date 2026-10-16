package api

import (
	"encoding/json"
	"errors"
	"github.com/addp/common/logpipeline"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	monitori18n "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"time"
)

type LogPipelineHandler struct {
	pipeline *service.LogPipelineService
}

func NewLogPipelineHandler(p *service.LogPipelineService) *LogPipelineHandler {
	return &LogPipelineHandler{pipeline: p}
}

// Summary godoc
// @Summary 获取平台日志链路状态 | Get platform log pipeline state
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.LogPipelineSummary
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.read"]
// @Router /platform/log-pipeline [get]
func (h *LogPipelineHandler) Summary(c *gin.Context) {
	result, err := h.pipeline.Summary(c.Request.Context(), time.Now().UTC())
	logRespond(c, result, err)
}

// Policy godoc
// @Summary 获取平台日志链路策略 | Get platform log pipeline policy
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.LogPipelinePolicy
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.read"]
// @Router /platform/log-pipeline/policy [get]
func (h *LogPipelineHandler) Policy(c *gin.Context) {
	result, err := h.pipeline.Policy(c.Request.Context())
	logRespond(c, result, err)
}

// UpdatePolicy godoc
// @Summary 更新平台日志链路策略 | Update platform log pipeline policy
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body models.LogPipelinePolicy true "请求 | Request"
// @Success 200 {object} models.LogPipelinePolicy
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_pipeline.update"]
// @Router /platform/log-pipeline/policy [put]
func (h *LogPipelineHandler) UpdatePolicy(c *gin.Context) {
	var input models.LogPipelinePolicy
	if !logBind(c, &input) {
		return
	}
	result, err := h.pipeline.UpdatePolicy(c.Request.Context(), input)
	logRespond(c, result, err)
}

// Observe godoc
// @Summary 上报平台日志链路观测 | Report platform log observation
// @Tags 平台日志链路 | Platform Log Pipeline
// @Produce json
// @Security BearerAuth
// @Accept json
// @Param request body logpipeline.Observation true "请求 | Request"
// @Success 200 {object} object
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.log_observation.create"]
// @Router /platform/log-observations [post]
func (h *LogPipelineHandler) Observe(c *gin.Context) {
	identity, ok := commonAuth.AuthContextFromGin(c)
	if !ok || identity.Token.Type != "service_access_token" || identity.Delegation != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		return
	}
	var input logpipeline.Observation
	if !logBind(c, &input) {
		return
	}
	err := h.pipeline.Ingest(c.Request.Context(), input, time.Now().UTC())
	logRespond(c, gin.H{"accepted": err == nil}, err)
}

func logBind(c *gin.Context, v any) bool { return bindPlatformRequest(c, v, logRespond) }
func bindPlatformRequest(c *gin.Context, v any, respond func(*gin.Context, any, error)) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil {
		respond(c, nil, service.ErrLogInvalid)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		respond(c, nil, service.ErrLogInvalid)
		return false
	}
	return true
}
func logRespond(c *gin.Context, v any, err error) {
	platformOperationRespond(c, v, err, "platform_log")
}

func platformOperationRespond(c *gin.Context, v any, err error, prefix string) {
	if err == nil {
		c.JSON(200, v)
		return
	}
	status := 500
	key := monitori18n.MsgDiagnosticQueryFailed
	code := prefix + "_operation_failed"
	switch {
	case errors.Is(err, service.ErrLogDisabled):
		status = 409
		key = monitori18n.MsgLogDisabled
		code = "observability_capability_disabled"
	case errors.Is(err, service.ErrLogUnconfigured):
		status = 503
		key = monitori18n.MsgLogUnconfigured
		code = "observability_capability_unconfigured"
	case errors.Is(err, service.ErrLogRetryUnavailable):
		status = 409
		key = monitori18n.MsgLogRetryUnavailable
		code = prefix + "_retry_unavailable"
	case errors.Is(err, service.ErrLogInvalid):
		status = 400
		key = monitori18n.MsgConfigurationInvalid
		code = prefix + "_invalid"
	case errors.Is(err, service.ErrLogConflict):
		status = 409
		key = monitori18n.MsgConfigurationConflict
		code = "resource_version_conflict"
	case errors.Is(err, service.ErrLogNotFound):
		status = 404
		key = monitori18n.MsgInvalidAlertID
		code = prefix + "_not_found"
	case errors.Is(err, service.ErrWeComRejected), errors.Is(err, service.ErrWeComRateLimited), errors.Is(err, service.ErrWeComResponse), errors.Is(err, service.ErrWeComNetwork), errors.Is(err, service.ErrWeComHTTP):
		status = 502
		key = monitori18n.MsgWeComTestFailed
		code = "wecom_test_failed"
	case errors.Is(err, service.ErrEmailSenderUnavailable):
		status = 503
		key = monitori18n.MsgEmailSenderUnavailable
		code = "smtp_unconfigured"
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key), "error_code": code})
}
