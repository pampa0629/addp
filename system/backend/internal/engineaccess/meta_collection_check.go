package engineaccess

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) CheckMetaCollection(ctx context.Context, actor Actor, request execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
	if request.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func(operation func(*Repository, func() error) error) error {
		return s.withSourceRuntime(ctx, actor, "addp-meta", authorization.PermissionSystemMetaCollectionQualificationExecute, operation)
	}
	if err := qualify(func(_ *Repository, check func() error) error { return check() }); err != nil {
		return nil, err
	}
	if s.metaScanFacts == nil {
		return nil, ErrMetaCollectionFactsUnavailable
	}
	facts, err := s.metaScanFacts.ReadScanPlanFacts(ctx, uint(actor.TenantID), request.ExecutionID)
	if err != nil {
		var remote *client.TenantAPIError
		if errors.As(err, &remote) && (remote.StatusCode == http.StatusNotFound || remote.StatusCode == http.StatusConflict) {
			return nil, commonapi.ErrForbidden
		}
		return nil, ErrMetaCollectionFactsUnavailable
	}
	if facts == nil || facts.Validate() != nil || facts.ExecutionID != request.ExecutionID || facts.ExecutionPlanDigest != request.ExecutionPlanDigest || facts.TaskID == nil || facts.TaskDefinitionDigest == nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.ScanDepth != "basic" || facts.SourceScope == nil || facts.SourceScope.Mode != "full" || len(facts.SourceScope.Anchors) != 1 {
		return nil, commonapi.ErrForbidden
	}
	taskID, err := collectionTaskID(*facts.TaskID)
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), facts.EngineID)
	if !reflect.DeepEqual(facts.SourceScope.Anchors[0], root) || facts.ReadOptions.Encoding != "" || facts.ReadOptions.HasHeader != nil {
		return nil, commonapi.ErrForbidden
	}
	var result *execution.MetaCollectionAccessObservation
	err = qualify(func(tx *Repository, check func() error) error {
		var engine models.Engine
		if err := tx.db.WithContext(ctx).Table("system.engines").Where("tenant_id=? AND id=?", actor.TenantID, facts.EngineID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&engine).Error; err != nil {
			return mapError(err)
		}
		if !collectionEngineAvailable(&engine) || strconv.FormatInt(engine.Version, 10) != facts.EngineVersion {
			return commonapi.ErrForbidden
		}
		var qualification MetaCollectionQualification
		if err := tx.db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND task_id=? AND task_definition_digest=? AND engine_version=? AND scan_depth='basic' AND scope_kind='engine' AND status='active' AND expires_at>clock_timestamp()", actor.TenantID, engine.ID, taskID, *facts.TaskDefinitionDigest, engine.Version).
			Clauses(clause.Locking{Strength: "SHARE"}).Take(&qualification).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrForbidden
			}
			return mapError(err)
		}
		// Independently bind the canonical raw Common row and current lease.
		var run struct {
			SourceTaskID    *string
			ExecutionConfig string
			Attempt         int
			LeaseToken      string
			LeaseExpiresAt  *time.Time
			Status          string
		}
		if err := tx.db.WithContext(ctx).Table((execution.TaskExecution{}).TableName()).Select("source_task_id, execution_config, attempt, lease_token, lease_expires_at, status").Where("tenant_id=? AND execution_id=? AND module='meta' AND task_type='scan' AND execution_boundary='bounded'", actor.TenantID, request.ExecutionID).
			Clauses(clause.Locking{Strength: "SHARE"}).Take(&run).Error; err != nil {
			return mapError(err)
		}
		digest, err := execution.ComputeMetaScanExecutionPlanDigest(json.RawMessage(run.ExecutionConfig))
		if err != nil {
			return commonapi.ErrForbidden
		}
		validate := func() error {
			if err := check(); err != nil {
				return err
			}
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if digest != request.ExecutionPlanDigest || run.SourceTaskID == nil || *run.SourceTaskID != *facts.TaskID || run.Status != execution.ExecutionStatusRunning || run.Attempt != request.Attempt || subtle.ConstantTimeCompare([]byte(run.LeaseToken), []byte(request.LeaseToken)) != 1 || run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.After(now) || !qualification.ExpiresAt.After(now) {
				return commonapi.ErrForbidden
			}
			return nil
		}
		if err := validate(); err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		result = &execution.MetaCollectionAccessObservation{ExecutionID: request.ExecutionID, ExecutionPlanDigest: digest, QualificationID: qualification.ID.String(), ObservedAt: now}
		return validate()
	})
	return result, err
}
