package engineaccess

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

var errProductionReceiptUnavailable = errors.New("production evidence unavailable")
var errProductionTargetChanged = errors.Join(commonapi.ErrConflict, errors.New("production engine changed or unavailable"))

func (s *Service) WithProductionRegistry(registry *production.Registry) *Service {
	s.productionRegistry = registry
	return s
}

type ProductionIdentificationReader interface {
	ReadProductionIdentification(context.Context, uint, production.Reference) (*client.MetaProductionIdentification, error)
}

func (s *Service) WithProductionIdentificationReader(reader ProductionIdentificationReader) *Service {
	s.productionIdentification = reader
	return s
}

// Internal transient evidence only. It contains no expiry, Allow or eligibility
// flag. Formal issuance must recheck this engine version and ALL Grant gates.
type preparedProduction struct {
	receipt       production.Receipt
	binding       authorizationfacts.Binding
	engineVersion int64
}

// prepareProduction consumes an owner receipt and a complete Meta
// boundary, never a browser target or a native ACL. It does not scan or grant.
// Tenant must come from the future trusted Runtime consumer, not an HTTP body.
func (s *Service) prepareProduction(ctx context.Context, tenantID int64, ref production.Reference) (*preparedProduction, error) {
	if tenantID <= 0 || ref.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.repository.db == nil || s.productionRegistry == nil || s.productionIdentification == nil {
		return nil, errProductionReceiptUnavailable
	}
	// No caller-owned transaction may remain open during cross-owner IO or
	// supply its own uncommitted Engine state as authority.
	if _, transactional := s.repository.db.Statement.ConnPool.(gorm.TxCommitter); transactional {
		return nil, errProductionReceiptUnavailable
	}
	receipt, err := s.productionRegistry.Read(ctx, uint(tenantID), ref)
	if err != nil {
		if status, ok := client.TenantAPIStatusCode(err); ok && status == http.StatusNotFound {
			return nil, commonapi.ErrNotFound
		}
		return nil, errProductionReceiptUnavailable
	}
	copy := *receipt
	engine, err := s.repository.productionEngine(ctx, tenantID, int64(copy.Primary.EngineID))
	if err != nil {
		return nil, err
	}
	if engine.Version != copy.TargetEngineVersion {
		return nil, errProductionTargetChanged
	}
	identified, err := s.productionIdentification.ReadProductionIdentification(ctx, uint(tenantID), ref)
	if err != nil || identified == nil || identified.Validate() != nil || identified.Reference != ref || identified.TenantID != tenantID || identified.Status != "success" {
		return nil, errProductionReceiptUnavailable
	}
	if identified.EngineVersion != copy.TargetEngineVersion {
		return nil, errProductionTargetChanged
	}
	if identified.LogicalItemState != "new" {
		return nil, errInitialProductionEvidence
	}
	bindings, readErr := s.resolveReadBindings(ctx, tenantID, []plugin.EngineCatalogPath{copy.Primary})
	current, err := s.repository.productionEngine(ctx, tenantID, int64(copy.Primary.EngineID))
	if err != nil {
		return nil, err
	}
	if current.Version != engine.Version {
		return nil, errProductionTargetChanged
	}
	if readErr != nil {
		return nil, readErr
	}
	if len(bindings) != 1 || !reflect.DeepEqual(bindings[0], *identified.Binding) {
		return nil, ErrAuthorizationBoundaryChanged
	}
	return &preparedProduction{receipt: copy, binding: bindings[0], engineVersion: engine.Version}, nil
}

// Only select non-secret local Engine facts; no Provider connection or sample
// is needed to consume the already committed producer and Meta evidence.
func (r *Repository) productionEngine(ctx context.Context, tenantID, engineID int64) (*models.Engine, error) {
	var engine models.Engine
	if err := r.db.WithContext(ctx).Table("system.engines").Select("id", "tenant_id", "engine_type", "version", "lifecycle_state").
		Where("tenant_id = ? AND id = ?", tenantID, engineID).Take(&engine).Error; err != nil {
		return nil, mapError(err)
	}
	if (engine.EngineType != "postgresql" && engine.EngineType != "nfs") || engine.LifecycleState != models.EngineLifecycleActive || engine.Version <= 0 {
		return nil, errProductionTargetChanged
	}
	return &engine, nil
}
