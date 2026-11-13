package engineaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type segmentationExecution struct {
	ExecutionConfig            json.RawMessage
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
}

func imageSegmentationExecution(db *gorm.DB, tenant int64, req client.ImageSegmentationAccessRequest) *gorm.DB {
	return db.Table("common.task_executions e").
		Select("e.execution_config, e.actor_principal_id, e.actor_tenant_membership_id, e.issued_authorization_version").
		Where("e.tenant_id=? AND e.execution_id=? AND e.module='manager' AND e.task_type=? AND e.status='running' AND e.execution_boundary='bounded' AND e.trigger_type='manual'", tenant, req.ExecutionID, execution.TaskTypeImageSegmentation).
		Where("e.source_task_id IS NOT NULL AND e.attempt=? AND e.lease_token=? AND e.lease_expires_at>clock_timestamp()", req.Attempt, req.LeaseToken).
		Where(`(e.source='manager' AND e.parent_execution_id IS NULL) OR (e.source='orchestrator' AND EXISTS (
			SELECT 1 FROM common.task_executions p
			WHERE p.execution_id=e.parent_execution_id AND p.tenant_id=e.tenant_id AND p.module='orchestrator'
			AND p.task_type='orchestration' AND p.status='running' AND p.trigger_type='manual'
			AND p.actor_principal_id=e.actor_principal_id AND p.actor_tenant_membership_id=e.actor_tenant_membership_id
			AND p.issued_authorization_version=e.issued_authorization_version
		))`)
}

// CheckImageSegmentationAccess reads the frozen task plan from the authoritative
// running execution. The service request supplies no user or data-resource identity.
func (s *Service) CheckImageSegmentationAccess(ctx context.Context, tenant int64, req client.ImageSegmentationAccessRequest) (*client.ImageSegmentationAccessObservation, error) {
	if s.executionEngineSnapshot == nil || req.Attempt != 1 {
		return nil, commonapi.ErrForbidden
	}
	db := s.repository.db.WithContext(ctx)
	var snapshot segmentationExecution
	if err := imageSegmentationExecution(db, tenant, req).Take(&snapshot).Error; err != nil {
		return nil, commonapi.ErrForbidden
	}
	var cfg execution.ImageSegmentationConfig
	if json.Unmarshal(snapshot.ExecutionConfig, &cfg) != nil || cfg.Validate() != nil {
		return nil, commonapi.ErrForbidden
	}
	primary, _ := cfg.Source.Composition.PrimaryTarget()
	targets := []plugin.EngineCatalogPath{primary}
	bindings, err := s.resolveReadBindings(ctx, tenant, targets)
	if err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].ItemFingerprint != cfg.Source.ItemFingerprint || !reflect.DeepEqual(bindings[0].FileComposition, &cfg.Source.Composition) {
		return nil, commonapi.ErrForbidden
	}
	var result client.ImageSegmentationAccessObservation
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := iam.CheckImageSegmentationActor(ctx, tx, tenant, snapshot.ActorPrincipalID, snapshot.ActorTenantMembershipID, snapshot.IssuedAuthorizationVersion); err != nil {
			return err
		}
		var current segmentationExecution
		if err := imageSegmentationExecution(tx, tenant, req).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "e"}}).Take(&current).Error; err != nil ||
			current.ActorPrincipalID != snapshot.ActorPrincipalID || current.ActorTenantMembershipID != snapshot.ActorTenantMembershipID ||
			current.IssuedAuthorizationVersion != snapshot.IssuedAuthorizationVersion || !bytes.Equal(current.ExecutionConfig, snapshot.ExecutionConfig) {
			return commonapi.ErrForbidden
		}
		read := sourceReadRequest{Action: "read", TenantID: tenant, Targets: targets, Bindings: bindings, Source: userProvenance{
			PrincipalID: current.ActorPrincipalID, MembershipID: current.ActorTenantMembershipID, AuthorizationVersion: current.IssuedAuthorizationVersion,
		}}
		observed, err := verifyExecutionSourceRead(ctx, tx, read)
		if err != nil {
			return err
		}
		// Restrict the first task to tenant MinIO sources and destinations.
		var engines []models.Engine
		if err := tx.Table("system.engines").Where("id IN ? AND tenant_id=? AND engine_type='minio' AND lifecycle_state='active' AND deleted_at IS NULL",
			[]uint{cfg.Source.EngineID, cfg.Target.EngineID}, tenant).Order("id").Clauses(clause.Locking{Strength: "SHARE"}).Find(&engines).Error; err != nil {
			return err
		}
		for i := range engines {
			engine := &engines[i]
			switch {
			case engine.ID == cfg.Source.EngineID && engine.Version == cfg.Source.EngineVersion:
				result.SourceEngine, err = s.executionEngineSnapshot(engine)
				if err != nil {
					return err
				}
			}
			if engine.ID == cfg.Target.EngineID && engine.Version == cfg.Target.EngineVersion && !engine.IsBuiltin {
				result.TargetEngine, err = s.executionEngineSnapshot(engine)
				if err != nil {
					return err
				}
			}
		}
		if result.SourceEngine == nil || result.TargetEngine == nil {
			return commonapi.ErrForbidden
		}
		result.ObservedAt = observed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
