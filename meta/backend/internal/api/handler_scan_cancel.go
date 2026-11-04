package api

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
)

type scanManagementScopeClient interface {
	GetMetaScanManagementScope(context.Context, int64, string, string) (*authorization.EngineAccessHandlingScope, error)
}

// CancelScanExecution godoc
// @Summary 请求取消 PG basic 集中扫描 | Request cancellation of a centralized PG basic scan
// @Description 需要独立取消权限及当前引擎管理范围。已放行枚举允许结束，下次枚举停止；返回当前执行状态，受理不代表运行体已停止，不关闭定时 | Requires independent cancel permission and current engine management scope. An admitted enumeration may finish, but the next is blocked. Acceptance does not mean the runner has stopped and does not disable scheduling
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "执行 ID | Execution ID"
// @Success 202 {object} models.ScanExecutionResponse "取消已受理或执行已结束 | Cancellation accepted or execution already ended"
// @Failure 400,401,404,409,503 {object} map[string]interface{} "请求失败 | Request failed"
// @Failure 403 {object} map[string]interface{} "权限或管理范围拒绝，error_code=permission_denied | Permission or management scope denied, error_code=permission_denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.cancel"]
// @Router /executions/{execution_id}/cancel [post]
func (h *Handler) CancelScanExecution(c *gin.Context) {
	commonapi.NoStore(c)
	auth, ok := commonAuth.AuthContextFromGin(c)
	if !ok || auth.Principal.Type != "user" || auth.Delegation != nil || auth.Context.TenantMembershipID == nil {
		respondScanManagementError(c, commonapi.ErrForbidden)
		return
	}
	principal, pErr := strconv.ParseInt(auth.Principal.ID, 10, 64)
	membership, mErr := strconv.ParseInt(*auth.Context.TenantMembershipID, 10, 64)
	version, vErr := strconv.ParseInt(auth.Authorization.AuthorizationVersion, 10, 64)
	if pErr != nil || mErr != nil || vErr != nil || principal <= 0 || membership <= 0 || version <= 0 {
		respondScanManagementError(c, commonapi.ErrForbidden)
		return
	}
	if h.executionService == nil || h.scanManagementScope == nil {
		commonapi.RespondError(c, http.StatusServiceUnavailable, commoni18n.T(c, commoni18n.MsgAuthorizationServiceUnavailable))
		return
	}
	// There are no caller-controlled engine, tenant, task or status overrides.
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if err != nil || len(body) != 0 || c.Request.URL.RawQuery != "" {
		respondScanManagementError(c, commonapi.ErrBadRequest)
		return
	}
	token := commonAuth.CanonicalBearerToken(c.GetHeader("Authorization"))
	input := service.ScanCancellationActor{PrincipalID: principal, MembershipID: &membership}
	tenant := commonAuth.GetTenantID(c)
	run, err := h.executionService.CancelPGCollection(c.Request.Context(), tenant, c.Param("execution_id"), input, func(ctx context.Context, engineID uint) error {
		observed, err := h.scanManagementScope.GetMetaScanManagementScope(ctx, int64(engineID), "cancel", token)
		if err != nil {
			return err
		}
		if observed == nil || observed.TenantID != int64(tenant) || observed.EngineID != int64(engineID) || observed.Operator.PrincipalID != principal || observed.Operator.MembershipID != membership || observed.Operator.AuthorizationVersion != version {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err != nil {
		respondScanManagementError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, models.NewScanExecutionResponse(run))
}

// Do not expose upstream payloads or connection details to the caller.
func respondScanManagementError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	if errors.Is(err, service.ErrScanPlanFactsUnavailable) {
		status = http.StatusServiceUnavailable
	}
	var remote *client.SystemAPIError
	if errors.As(err, &remote) {
		switch remote.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
			status = remote.StatusCode
		default:
			status = http.StatusServiceUnavailable
		}
	}
	key := metai18n.MsgScanManagementFailed
	switch status {
	case http.StatusUnauthorized:
		key = commoni18n.MsgUnauthorized
	case http.StatusForbidden:
		c.JSON(status, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden), "error_code": "permission_denied"})
		return
	case http.StatusBadRequest:
		key = commoni18n.MsgInvalidParams
	case http.StatusNotFound:
		key = metai18n.MsgScanExecutionNotFound
	case http.StatusConflict:
		key = metai18n.MsgScanExecutionChanged
	case http.StatusServiceUnavailable:
		key = commoni18n.MsgAuthorizationServiceUnavailable
	}
	commonapi.RespondError(c, status, commoni18n.T(c, key))
}
