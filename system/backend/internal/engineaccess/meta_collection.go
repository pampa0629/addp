package engineaccess

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMetaCollectionFactsUnavailable = errors.New("Meta collection facts unavailable")
var ErrMetaCollectionChanged = errors.Join(commonapi.ErrConflict, errors.New("Meta collection configuration changed or unsupported"))

type MetaScanFactsReader interface {
	ReadScanTaskFacts(context.Context, uint, string) (*execution.MetaScanTaskFacts, error)
	ReadScanPlanFacts(context.Context, uint, string) (*execution.MetaScanPlanFacts, error)
}

func (s *Service) WithMetaScanFactsReader(reader MetaScanFactsReader) *Service {
	s.metaScanFacts = reader
	return s
}

// This is a task-bound collection qualification, not a human data Grant.
type MetaCollectionQualification struct {
	ID                          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey"`
	TenantID                    int64      `json:"-"`
	EngineID                    int64      `json:"engine_id,string" swaggertype:"string"`
	TaskID                      int64      `json:"task_id,string" swaggertype:"string"`
	TaskDefinitionDigest        string     `json:"task_definition_digest"`
	EngineVersion               int64      `json:"engine_version,string" swaggertype:"string"`
	ScanDepth                   string     `json:"scan_depth"`
	ScopeKind                   string     `json:"scope_kind"`
	Purpose                     string     `json:"purpose"`
	Status                      string     `json:"status"`
	Version                     int64      `json:"version,string" swaggertype:"string"`
	GrantedAt                   time.Time  `json:"granted_at"`
	ExpiresAt                   time.Time  `json:"expires_at"`
	GrantedByPrincipalID        int64      `json:"granted_by_principal_id,string" swaggertype:"string"`
	GrantedByMembershipID       int64      `json:"-"`
	GrantedAuthorizationVersion int64      `json:"-"`
	RevokedAt                   *time.Time `json:"revoked_at,omitempty"`
	RevokedByPrincipalID        *int64     `json:"revoked_by_principal_id,string,omitempty" swaggertype:"string"`
	RevokedReason               *string    `json:"revoked_reason,omitempty"`
}

func (MetaCollectionQualification) TableName() string { return "system.meta_collection_qualifications" }

type ApproveMetaCollectionInput struct {
	Actor                        Actor
	EngineID                     int64
	TaskID                       string
	CommandID                    uuid.UUID
	ExpectedTaskDefinitionDigest string
	Purpose                      string
	ExpiresAt                    time.Time
	Audit                        iam.AuditMetadata
}

func (s *Service) ListMetaCollections(ctx context.Context, actor Actor, engineID int64, page, size int) ([]MetaCollectionQualification, int64, error) {
	if page < 1 || size < 1 || size > 100 {
		return nil, 0, commonapi.ErrBadRequest
	}
	rows := make([]MetaCollectionQualification, 0)
	var total int64
	err := s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemMetaCollectionQualificationRead, false, func(tx *Repository, check func() error) error {
		q := tx.db.WithContext(ctx).Model(&MetaCollectionQualification{}).Where("tenant_id=? AND engine_id=?", actor.TenantID, engineID)
		if err := q.Count(&total).Error; err != nil {
			return err
		}
		if err := q.Order("granted_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
			return err
		}
		return check()
	})
	return rows, total, err
}

type RevokeMetaCollectionInput struct {
	Actor    Actor
	EngineID int64
	ID       uuid.UUID
	Version  int64
	Reason   string
	Audit    iam.AuditMetadata
}

func (s *Service) GetMetaCollectionTaskFacts(ctx context.Context, actor Actor, engineID int64, taskID string) (*execution.MetaScanTaskFacts, error) {
	if _, err := collectionTaskID(taskID); err != nil {
		return nil, err
	}
	qualify := func() error {
		return s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemMetaCollectionQualificationRead, false, func(_ *Repository, check func() error) error { return check() })
	}
	if err := qualify(); err != nil {
		return nil, err
	}
	facts, err := s.readMetaCollectionTask(ctx, actor.TenantID, engineID, taskID)
	if currentErr := qualify(); currentErr != nil {
		return nil, currentErr
	}
	return facts, err
}

func (s *Service) readMetaCollectionTask(ctx context.Context, tenantID, engineID int64, taskID string) (*execution.MetaScanTaskFacts, error) {
	if s.metaScanFacts == nil {
		return nil, ErrMetaCollectionFactsUnavailable
	}
	facts, err := s.metaScanFacts.ReadScanTaskFacts(ctx, uint(tenantID), taskID)
	if err != nil {
		var remote *client.TenantAPIError
		if errors.As(err, &remote) {
			if remote.StatusCode == http.StatusNotFound {
				return nil, commonapi.ErrNotFound
			}
			if remote.StatusCode == http.StatusConflict {
				return nil, ErrMetaCollectionChanged
			}
		}
		return nil, ErrMetaCollectionFactsUnavailable
	}
	if facts == nil || facts.Validate() != nil || facts.TaskID != taskID {
		return nil, ErrMetaCollectionFactsUnavailable
	}
	if int64(facts.EngineID) != engineID {
		return nil, commonapi.ErrNotFound
	}
	return facts, nil
}

func (s *Service) ApproveMetaCollection(ctx context.Context, in ApproveMetaCollectionInput) (*MetaCollectionQualification, error) {
	taskID, err := collectionTaskID(in.TaskID)
	if err != nil || in.CommandID == uuid.Nil || !authorizationfacts.ValidItemFingerprint(in.ExpectedTaskDefinitionDigest) || !validReason(in.Purpose) || in.ExpiresAt.IsZero() || in.ExpiresAt.Year() > 9999 {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func(op func(*Repository, func() error) error) error {
		return s.withEngineManagementScope(ctx, in.Actor, in.EngineID, authorization.PermissionSystemMetaCollectionQualificationApprove, false, op)
	}
	var recovered *MetaCollectionQualification
	if err := qualify(func(tx *Repository, check func() error) error {
		var prior MetaCollectionQualification
		if err := tx.db.WithContext(ctx).Where("id=?", in.CommandID).Take(&prior).Error; err == nil {
			if !sameMetaCollectionCommand(&prior, in, taskID) {
				return ErrMetaCollectionChanged
			}
			recovered = &prior
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return check()
	}); err != nil {
		return nil, err
	}
	if recovered != nil {
		return recovered, nil
	}
	facts, err := s.readMetaCollectionTask(ctx, in.Actor.TenantID, in.EngineID, in.TaskID)
	if err != nil {
		return nil, err
	}
	if facts.TaskDefinitionDigest != in.ExpectedTaskDefinitionDigest || facts.ScopeKind != "engine" || facts.ScanDepth != "basic" || facts.ReadOptions.Encoding != "" || facts.ReadOptions.HasHeader != nil {
		return nil, ErrMetaCollectionChanged
	}
	var result *MetaCollectionQualification
	err = qualify(func(tx *Repository, check func() error) error {
		engine, err := tx.engine(ctx, in.Actor.TenantID, in.EngineID, false)
		if err != nil {
			return err
		}
		if !collectionEngineAvailable(engine) {
			return ErrMetaCollectionChanged
		}
		if err := tx.lockMetaCollectionTask(ctx, in.Actor.TenantID, in.EngineID, taskID); err != nil {
			return err
		}
		var prior MetaCollectionQualification
		err = tx.db.WithContext(ctx).Where("id=?", in.CommandID).Take(&prior).Error
		if err == nil {
			if !sameMetaCollectionCommand(&prior, in, taskID) {
				return ErrMetaCollectionChanged
			}
			result = &prior
			return check()
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.db.WithContext(ctx).Model(&MetaCollectionQualification{}).Where("tenant_id=? AND engine_id=? AND task_id=? AND status='active' AND expires_at>clock_timestamp()", in.Actor.TenantID, in.EngineID, taskID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrMetaCollectionChanged
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !in.ExpiresAt.After(now) {
			return ErrExpiry
		}
		row := &MetaCollectionQualification{ID: in.CommandID, TenantID: in.Actor.TenantID, EngineID: in.EngineID, TaskID: taskID, TaskDefinitionDigest: facts.TaskDefinitionDigest, EngineVersion: engine.Version, ScanDepth: "basic", ScopeKind: "engine", Purpose: strings.TrimSpace(in.Purpose), Status: "active", Version: 1, GrantedAt: now, ExpiresAt: in.ExpiresAt.UTC(), GrantedByPrincipalID: in.Actor.PrincipalID, GrantedByMembershipID: in.Actor.MembershipID, GrantedAuthorizationVersion: in.Actor.AuthorizationVersion}
		if err := tx.db.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		if err := tx.auditMetaCollection(ctx, in.Actor, in.Audit, row, "approved", row.Purpose); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		finalNow, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if !row.ExpiresAt.After(finalNow) {
			return ErrExpiry
		}
		result = row
		return nil
	})
	return result, err
}

func (s *Service) RevokeMetaCollection(ctx context.Context, in RevokeMetaCollectionInput) (*MetaCollectionQualification, error) {
	if in.ID == uuid.Nil || in.Version <= 0 || !validReason(in.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	var result *MetaCollectionQualification
	err := s.withEngineManagementScope(ctx, in.Actor, in.EngineID, authorization.PermissionSystemMetaCollectionQualificationRevoke, false, func(tx *Repository, check func() error) error {
		var row MetaCollectionQualification
		if err := tx.db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND id=?", in.Actor.TenantID, in.EngineID, in.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return mapError(err)
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if row.Version != in.Version || row.Status != "active" || !row.ExpiresAt.After(now) {
			return ErrVersionConflict
		}
		reason := strings.TrimSpace(in.Reason)
		updates := map[string]any{"status": "revoked", "version": row.Version + 1, "revoked_at": now, "revoked_by_principal_id": in.Actor.PrincipalID, "revoked_reason": reason}
		if err := tx.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.auditMetaCollection(ctx, in.Actor, in.Audit, &row, "revoked", reason); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		result = &row
		return nil
	})
	return result, err
}

func collectionTaskID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, commonapi.ErrBadRequest
	}
	return id, nil
}

func sameMetaCollectionCommand(prior *MetaCollectionQualification, in ApproveMetaCollectionInput, taskID int64) bool {
	return prior.TenantID == in.Actor.TenantID && prior.EngineID == in.EngineID && prior.TaskID == taskID && prior.TaskDefinitionDigest == in.ExpectedTaskDefinitionDigest && prior.Purpose == strings.TrimSpace(in.Purpose) && prior.ExpiresAt.Equal(in.ExpiresAt) && prior.GrantedByPrincipalID == in.Actor.PrincipalID
}
func collectionEngineAvailable(engine *models.Engine) bool {
	return engine != nil && engine.EngineType == "postgresql" && engine.Version > 0 && engine.LifecycleState == models.EngineLifecycleActive
}
func (r *Repository) lockMetaCollectionTask(ctx context.Context, tenantID, engineID, taskID int64) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "meta-collection|"+strconv.FormatInt(tenantID, 10)+"|"+strconv.FormatInt(engineID, 10)+"|"+strconv.FormatInt(taskID, 10)).Error
}
func (r *Repository) auditMetaCollection(ctx context.Context, actor Actor, audit iam.AuditMetadata, row *MetaCollectionQualification, action, reason string) error {
	principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
	audit.PrincipalID = &actor.PrincipalID
	audit.PrincipalType = &principalType
	audit.ContextType = &contextType
	audit.TenantID = &actor.TenantID
	return iam.NewAuditWriter(r.identity()).Write(ctx, iam.AuditEvent{Metadata: audit, EventName: "system.meta_collection_qualification." + action, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "meta_collection_qualification", EntityID: row.ID.String(), Details: map[string]any{"engine_id": row.EngineID, "task_id": row.TaskID, "task_definition_digest": row.TaskDefinitionDigest, "engine_version": row.EngineVersion, "version": row.Version, "scan_depth": row.ScanDepth, "scope_kind": row.ScopeKind, "expires_at": row.ExpiresAt, "reason": reason}})
}
