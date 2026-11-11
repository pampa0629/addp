package engineaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type workflowGeoJSONSnapshot struct {
	ExecutionConfig            json.RawMessage
	ActorPrincipalID           int64
	ActorTenantMembershipID    int64
	IssuedAuthorizationVersion int64
}

func workflowGeoJSONExecution(db *gorm.DB, tenant int64, r client.WorkflowGeoJSONAccessRequest) *gorm.DB {
	return db.Table("common.task_executions e").
		Select("e.execution_config,e.actor_principal_id,e.actor_tenant_membership_id,e.issued_authorization_version").
		Joins("JOIN system.execution_authorizations a ON a.id=e.execution_authorization_id AND a.execution_id=e.execution_id::uuid AND a.tenant_id=e.tenant_id AND a.audience='develop' AND a.source_type='user' AND a.sealed_at IS NOT NULL AND a.revoked_at IS NULL AND a.expires_at>clock_timestamp() AND a.actor_principal_id=e.actor_principal_id AND a.tenant_membership_id=e.actor_tenant_membership_id AND a.issued_authorization_version=e.issued_authorization_version AND ((a.source_execution_attempt IS NULL AND a.source_execution_lease_token IS NULL) OR (a.source_execution_attempt=e.attempt AND a.source_execution_lease_token=e.lease_token::uuid AND e.parent_execution_id IS NOT NULL))").
		Where("e.execution_id=? AND e.tenant_id=? AND e.module='develop' AND e.task_type='workflow' AND e.status='running' AND e.execution_boundary='bounded' AND e.trigger_type='manual' AND e.attempt=? AND e.lease_token=? AND e.lease_expires_at>clock_timestamp()", r.ExecutionID, tenant, r.Attempt, r.LeaseToken).
		Where(`(e.source='develop' AND e.parent_execution_id IS NULL) OR (e.source='orchestrator' AND EXISTS(SELECT 1 FROM common.task_executions p WHERE p.execution_id=e.parent_execution_id AND p.tenant_id=e.tenant_id AND p.module='orchestrator' AND p.task_type='orchestration' AND p.status='running' AND p.lease_expires_at>clock_timestamp() AND p.trigger_type='manual' AND p.actor_principal_id=e.actor_principal_id AND p.actor_tenant_membership_id=e.actor_tenant_membership_id AND p.issued_authorization_version=e.issued_authorization_version))`)
}
func (s *Service) CheckWorkflowGeoJSONAccess(ctx context.Context, tenant int64, r client.WorkflowGeoJSONAccessRequest) (*client.ManagerFileAccessObservation, error) {
	var frozen workflowGeoJSONSnapshot
	db := s.repository.db.WithContext(ctx)
	if err := workflowGeoJSONExecution(db, tenant, r).Take(&frozen).Error; err != nil {
		return nil, commonapi.ErrForbidden
	}
	var config struct {
		Content struct {
			Workflow struct {
				Tasks []struct {
					ID       string `json:"id"`
					Operator string `json:"operator"`
					Params   struct {
						Locator string `json:"locator"`
					} `json:"params"`
				} `json:"tasks"`
			} `json:"workflow_definition"`
		} `json:"content"`
	}
	if json.Unmarshal(frozen.ExecutionConfig, &config) != nil {
		return nil, commonapi.ErrForbidden
	}
	uri := ""
	for _, node := range config.Content.Workflow.Tasks {
		if node.ID == r.NodeID && node.Operator == "load" {
			if uri != "" {
				return nil, commonapi.ErrForbidden
			}
			uri = node.Params.Locator
		}
	}
	loc, err := resourcetree.ParseURI(uri)
	if err != nil || loc.Type != resourcetree.TypeObject {
		return nil, commonapi.ErrForbidden
	}
	target, err := resourcetree.EngineCatalogPathFromLocator(plugin.ObjectCatalogModel(), loc)
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	bindings, err := s.resolveReadBindings(ctx, tenant, []plugin.EngineCatalogPath{target})
	if err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].FileComposition == nil || bindings[0].FileComposition.Format != "geojson" || len(bindings[0].FileComposition.Refs) != 1 {
		return nil, commonapi.ErrForbidden
	}
	var result client.ManagerFileAccessObservation
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := iam.CheckCurrentExecutionActorPermissions(ctx, tx, tenant, frozen.ActorPrincipalID, frozen.ActorTenantMembershipID, frozen.IssuedAuthorizationVersion, []string{"develop.task.execute", "develop.data_read.execute"}); err != nil {
			return err
		}
		var current workflowGeoJSONSnapshot
		if err := workflowGeoJSONExecution(tx, tenant, r).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "e"}}).Take(&current).Error; err != nil || !bytes.Equal(current.ExecutionConfig, frozen.ExecutionConfig) || !reflect.DeepEqual(current, frozen) {
			return commonapi.ErrForbidden
		}
		var accessCount int64
		if err := tx.Table("system.execution_authorization_engine_accesses x").Joins("JOIN system.execution_authorizations a ON a.id=x.authorization_id").
			Where("a.execution_id=? AND a.tenant_id=? AND a.audience='develop' AND x.engine_id=? AND 'read'=ANY(x.effects)", r.ExecutionID, tenant, target.EngineID).Count(&accessCount).Error; err != nil || accessCount != 1 {
			return commonapi.ErrForbidden
		}
		result.ObservedAt, err = verifyExecutionSourceRead(ctx, tx, sourceReadRequest{Action: "read", TenantID: tenant, Targets: []plugin.EngineCatalogPath{target}, Bindings: bindings, Source: userProvenance{PrincipalID: frozen.ActorPrincipalID, MembershipID: frozen.ActorTenantMembershipID, AuthorizationVersion: frozen.IssuedAuthorizationVersion}})
		if err != nil {
			return err
		}
		var engine models.Engine
		if err := tx.Table("system.engines").Where("id=? AND tenant_id=? AND engine_type='minio' AND lifecycle_state='active' AND deleted_at IS NULL", target.EngineID, tenant).Clauses(clause.Locking{Strength: "SHARE"}).Take(&engine).Error; err != nil {
			return commonapi.ErrForbidden
		}
		if s.executionEngineSnapshot == nil {
			return commonapi.ErrForbidden
		}
		result.Engine, err = s.executionEngineSnapshot(&engine)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
