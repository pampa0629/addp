package exportartifact

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/middleware/auth"
	"github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *GormStore) GetExecutionSource(ctx context.Context, id, tenantID uint, request client.ExportExecutionSourceRequest) (*Session, error) {
	var session Session
	err := s.db.WithContext(ctx).Table(s.table).
		Where("id = ? AND tenant_id = ? AND transfer_execution_id = ? AND execution_request_digest = ? AND status = ? AND user_id > 0 AND created_at >= ?",
			id, tenantID, request.ExecutionID, request.RequestDigest, StatusPending,
			time.Now().Add(-NormalizeCleanupOptions(CleanupOptions{}).MaxRunningAge)).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

func (s *Service) ResolveExecutionSource(ctx context.Context, id, tenantID uint, request client.ExportExecutionSourceRequest) (*client.ExportExecutionSource, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("export store is unavailable")
	}
	if id == 0 || tenantID == 0 || request.Validate() != nil {
		return nil, commonAPI.ErrBadRequest
	}
	session, err := s.store.GetExecutionSource(ctx, id, tenantID, request)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	return &client.ExportExecutionSource{TenantID: session.TenantID, UserID: session.UserID}, nil
}

type ExecutionSourceResolver interface {
	ResolveExecutionSource(context.Context, uint, uint, client.ExportExecutionSourceRequest) (*client.ExportExecutionSource, error)
}

// ExecutionSourceGuards is the only authorization policy for export provenance.
func ExecutionSourceGuards(permission string) []gin.HandlerFunc {
	return []gin.HandlerFunc{auth.MustNewContextGuard("tenant"), auth.MustNewServiceClientGuard("addp-transfer"), auth.MustNewPermissionGuard(permission)}
}

// ServeExecutionSource is shared by both export owners. It never returns an artifact or credential.
func ServeExecutionSource(c *gin.Context, resolver ExecutionSourceResolver) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	var request client.ExportExecutionSourceRequest
	if err != nil || id == 0 || commonAPI.BindOptionalJSONStrict(c, &request) != nil || request.Validate() != nil {
		commonAPI.BadRequestError(c, i18n.T(c, i18n.MsgExportSourceInvalid))
		return
	}
	if resolver == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18n.T(c, i18n.MsgExportSourceUnavailable)})
		return
	}
	result, err := resolver.ResolveExecutionSource(c.Request.Context(), uint(id), auth.GetTenantID(c), request)
	if errors.Is(err, ErrSessionNotFound) {
		commonAPI.NotFoundError(c, i18n.T(c, i18n.MsgExportSourceNotFound))
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18n.T(c, i18n.MsgExportSourceUnavailable)})
		return
	}
	c.JSON(http.StatusOK, result)
}
