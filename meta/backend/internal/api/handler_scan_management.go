package api

import (
	"context"
	"fmt"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h *Handler) scanManagementContext(c *gin.Context) context.Context {
	return service.WithScanManagementCheck(scanUserOriginContext(c), func(ctx context.Context, task *models.ScanTask, operation string) error {
		auth, ok := commonAuth.AuthContextFromGin(c)
		if !ok || auth.Principal.Type != "user" || auth.Delegation != nil || auth.Context.TenantMembershipID == nil || task.TenantID != commonAuth.GetTenantID(c) {
			return commonapi.ErrForbidden
		}
		if h.engineService == nil || h.scanManagementScope == nil {
			return service.ErrScanPlanFactsUnavailable
		}
		engine, err := h.engineService.GetResourceByID(task.EngineID, task.TenantID)
		if err != nil {
			return err
		}
		if engine.EngineType != "postgresql" {
			return nil
		} // Outside this PG-only change; retain its existing path.
		if operation != "delete" && operation != "inspect-update" {
			if task.Parameters["scan_depth"] != "basic" || task.Scope["type"] != "engine" {
				return commonapi.ErrForbidden
			}
			if v, ok := task.Scope["engine_id"]; ok && fmt.Sprint(v) != strconv.FormatUint(uint64(task.EngineID), 10) {
				return commonapi.ErrForbidden
			}
			if v, ok := task.Parameters["force"]; ok {
				if _, valid := v.(bool); !valid {
					return commonapi.ErrForbidden
				}
			}
			for key := range task.Scope {
				if key != "type" && key != "engine_id" {
					return commonapi.ErrForbidden
				}
			}
			for key := range task.Parameters {
				if key != "scan_depth" && key != "force" {
					return commonapi.ErrForbidden
				}
			}
		}
		if operation == "inspect-update" {
			operation = "update"
		}
		token := commonAuth.CanonicalBearerToken(c.GetHeader("Authorization"))
		observed, err := h.scanManagementScope.GetMetaScanManagementScope(ctx, int64(task.EngineID), operation, token)
		if err != nil {
			return err
		}
		if observed == nil || observed.TenantID != int64(task.TenantID) || observed.EngineID != int64(task.EngineID) || strconv.FormatInt(observed.Operator.PrincipalID, 10) != auth.Principal.ID || strconv.FormatInt(observed.Operator.MembershipID, 10) != *auth.Context.TenantMembershipID || strconv.FormatInt(observed.Operator.AuthorizationVersion, 10) != auth.Authorization.AuthorizationVersion {
			return commonapi.ErrForbidden
		}
		return nil
	})
}
