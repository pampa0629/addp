package engineaccess

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm/clause"
)

// Only fixed Meta Runtime can consume an already persisted User refresh plan.
// A historical actor is requalified; the Service identity supplies no data right.
func (s *Service) checkMetaRefreshExecution(ctx context.Context, actor Actor, request execution.MetaCollectionAccessRequest, facts *execution.MetaScanPlanFacts) (*execution.MetaCollectionAccessObservation, error) {
	if facts == nil || facts.Validate() != nil || facts.ExecutionID != request.ExecutionID || facts.ExecutionPlanDigest != request.ExecutionPlanDigest || facts.RefreshScope == nil || facts.RefreshScope.TenantID != actor.TenantID {
		return nil, commonapi.ErrForbidden
	}
	scope := facts.RefreshScope
	source := userProvenance{PrincipalID: scope.Operator.PrincipalID, MembershipID: scope.Operator.MembershipID, AuthorizationVersion: scope.Operator.AuthorizationVersion}
	var result *execution.MetaCollectionAccessObservation
	err := s.withSourceRuntime(ctx, actor, "addp-meta", authorization.PermissionSystemMetaCollectionExecute, func(tx *Repository, check func() error) error {
		origin, err := tx.lockUserProvenance(ctx, actor.TenantID, source)
		if err != nil {
			return err
		}
		engine, err := tx.engine(ctx, actor.TenantID, int64(facts.EngineID), false)
		if err != nil {
			return err
		}
		if engine.Version <= 0 || strconv.FormatInt(engine.Version, 10) != facts.EngineVersion || engine.LifecycleState != "active" || engine.DeletedAt != nil || (engine.EngineType != "postgresql" && engine.EngineType != "nfs") ||
			(engine.EngineType == "postgresql") != (scope.Query.Target.Segments[0].Term == "server") {
			return commonapi.ErrForbidden
		}
		var run struct {
			ExecutionConfig            string
			SourceTaskID               *string
			ParentExecutionID          *string
			ActorPrincipalID           *int64
			ActorTenantMembershipID    *int64
			IssuedAuthorizationVersion *int64
			Attempt                    int
			LeaseToken                 string
			LeaseExpiresAt             *time.Time
			Status                     string
			Source                     string
			TriggerType                string
			Metadata                   string
		}
		if err := tx.db.WithContext(ctx).Table((execution.TaskExecution{}).TableName()).Select("execution_config,source_task_id,parent_execution_id,actor_principal_id,actor_tenant_membership_id,issued_authorization_version,attempt,lease_token,lease_expires_at,status,source,trigger_type,metadata").
			Where("tenant_id=? AND execution_id=? AND module='meta' AND task_type='scan' AND execution_boundary='bounded'", actor.TenantID, request.ExecutionID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&run).Error; err != nil {
			return mapError(err)
		}
		digest, err := execution.ComputeMetaScanExecutionPlanDigest(json.RawMessage(run.ExecutionConfig))
		if err != nil {
			return commonapi.ErrForbidden
		}
		var saved struct {
			RefreshScope *shared.MetadataRefreshScope `json:"refresh_scope"`
		}
		if json.Unmarshal([]byte(run.ExecutionConfig), &saved) != nil || !reflect.DeepEqual(saved.RefreshScope, scope) {
			return commonapi.ErrForbidden
		}
		var metadata map[string]json.RawMessage
		if run.Metadata != "" && json.Unmarshal([]byte(run.Metadata), &metadata) != nil {
			return commonapi.ErrForbidden
		}
		_, cancelled := metadata["scan_cancellation"]
		validate := func() error {
			if err := check(); err != nil {
				return err
			}
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if origin.check(now) != nil || run.ActorPrincipalID == nil || *run.ActorPrincipalID != source.PrincipalID ||
				run.ActorTenantMembershipID == nil || *run.ActorTenantMembershipID != source.MembershipID || run.IssuedAuthorizationVersion == nil || *run.IssuedAuthorizationVersion != source.AuthorizationVersion ||
				run.SourceTaskID != nil || run.ParentExecutionID != nil || run.TriggerType != "manual" || (run.Source != "meta" && run.Source != "manager") ||
				cancelled || digest != request.ExecutionPlanDigest || run.Status != execution.ExecutionStatusRunning || run.Attempt != request.Attempt ||
				subtle.ConstantTimeCompare([]byte(run.LeaseToken), []byte(request.LeaseToken)) != 1 || run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.After(now) {
				return commonapi.ErrForbidden
			}
			permissions, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, source.PrincipalID, iam.PrincipalTypeUser, iam.ContextTypeTenant, &actor.TenantID, &source.MembershipID, now)
			if err != nil {
				return err
			}
			if !hasCurrentTenantPermission(permissions, actor.TenantID, metadataRefreshPermission, now) {
				return commonapi.ErrForbidden
			}
			return nil
		}
		if err := validate(); err != nil {
			return err
		}
		if scope.Query.Kind == "engine" {
			management, err := tx.lockManagementScope(ctx, actor.TenantID, int64(facts.EngineID), source.MembershipID)
			if err != nil {
				return err
			}
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if err := management.check(now); err != nil {
				return err
			}
		} else {
			live, err := tx.metadataRefreshRanges(ctx, actor.TenantID, source, scope.Query)
			if err != nil {
				return err
			}
			if !shared.MetadataRefreshScopeStillCovered(scope.Ranges, live, facts.EngineID) {
				return commonapi.ErrForbidden
			}
		}
		if err := validate(); err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		result = &execution.MetaCollectionAccessObservation{ExecutionID: request.ExecutionID, ExecutionPlanDigest: digest, ObservedAt: now}
		return nil
	}, source.PrincipalID)
	return result, err
}
