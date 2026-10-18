package engineaccess

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var errProductionReceiptUnavailable = errors.New("Transfer production evidence unavailable")
var errProductionTargetChanged = errors.Join(commonapi.ErrConflict, errors.New("PG production engine changed or unavailable"))

// The implementation is the shared TransferClient with System's own Tenant
// Service credential. No User token, metadata reader or cross-schema access.
type TransferProductionReceiptReader interface {
	ReadProductionReceipt(context.Context, uint, string) (*client.TransferProductionReceipt, error)
}

func (s *Service) WithTransferProductionReceiptReader(reader TransferProductionReceiptReader) *Service {
	s.productionReceipts = reader
	return s
}

// Internal transient evidence only. It contains no expiry, Allow or eligibility
// flag. Formal issuance must recheck this engine version and ALL Grant gates.
type preparedTransferProduction struct {
	receipt       client.TransferProductionReceipt
	binding       authorizationfacts.Binding
	engineVersion int64
}

// prepareTransferProduction consumes an owner receipt and a complete Meta
// boundary, never a browser target or a native ACL. It does not scan or grant.
// Tenant must come from the future trusted Runtime consumer, not an HTTP body.
func (s *Service) prepareTransferProduction(ctx context.Context, tenantID int64, executionID string) (*preparedTransferProduction, error) {
	id, err := uuid.Parse(executionID)
	if tenantID <= 0 || err != nil || id == uuid.Nil || id.String() != executionID {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil || s.productionReceipts == nil {
		return nil, errProductionReceiptUnavailable
	}
	// No caller-owned transaction may remain open during cross-owner IO or
	// supply its own uncommitted Engine state as authority.
	if _, transactional := s.repository.db.Statement.ConnPool.(gorm.TxCommitter); transactional {
		return nil, errProductionReceiptUnavailable
	}
	receipt, err := s.productionReceipts.ReadProductionReceipt(ctx, uint(tenantID), executionID)
	if err != nil {
		if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
			return nil, commonapi.ErrNotFound
		}
		return nil, errProductionReceiptUnavailable
	}
	if receipt == nil || receipt.Validate() != nil || receipt.TenantID != tenantID || receipt.ExecutionID != executionID || int64(receipt.Target.EngineID) <= 0 {
		return nil, errProductionReceiptUnavailable
	}
	// The reader's pointers must not alias evidence retained during Meta IO.
	copy := *receipt
	copy.Target.Segments = append([]plugin.EngineCatalogSegment(nil), receipt.Target.Segments...)
	if receipt.UserOrigin != nil {
		origin := *receipt.UserOrigin
		copy.UserOrigin = &origin
	}
	if receipt.RetryOfExecutionID != nil {
		retry := *receipt.RetryOfExecutionID
		copy.RetryOfExecutionID = &retry
	}
	engine, err := s.repository.productionEngine(ctx, tenantID, int64(copy.Target.EngineID))
	if err != nil {
		return nil, err
	}
	bindings, readErr := s.resolveReadBindings(ctx, tenantID, []plugin.EngineCatalogPath{copy.Target})
	current, err := s.repository.productionEngine(ctx, tenantID, int64(copy.Target.EngineID))
	if err != nil {
		return nil, err
	}
	if current.Version != engine.Version {
		return nil, errProductionTargetChanged
	}
	if readErr != nil {
		return nil, readErr
	}
	if len(bindings) != 1 {
		return nil, ErrAuthorizationBoundaryChanged
	}
	return &preparedTransferProduction{receipt: copy, binding: bindings[0], engineVersion: engine.Version}, nil
}

// Only select non-secret local Engine facts; no Provider connection or sample
// is needed to consume the already committed producer and Meta evidence.
func (r *Repository) productionEngine(ctx context.Context, tenantID, engineID int64) (*models.Engine, error) {
	var engine models.Engine
	if err := r.db.WithContext(ctx).Table("system.engines").Select("id", "tenant_id", "engine_type", "version", "lifecycle_state").
		Where("tenant_id = ? AND id = ?", tenantID, engineID).Take(&engine).Error; err != nil {
		return nil, mapError(err)
	}
	if engine.EngineType != "postgresql" || engine.LifecycleState != models.EngineLifecycleActive || engine.Version <= 0 {
		return nil, errProductionTargetChanged
	}
	return &engine, nil
}
