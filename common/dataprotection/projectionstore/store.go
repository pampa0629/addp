package projectionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/dataprotection"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var schemaNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type GateResult struct {
	Managed     bool
	State       string
	Projections []dataprotection.Projection
	Err         error
}

type GateMatch struct {
	Target dataprotection.ResourceReference
	Gate   GateResult
}

type ManagedTarget struct {
	TenantID int64
	Target   dataprotection.ResourceReference
}

// ProjectionChangeBarrier lets an owner converge derived local data in the
// same transaction as projection rows and the consumer checkpoint.
type ProjectionChangeBarrier interface {
	ApplyProjectionChanges(context.Context, *gorm.DB, int64, []dataprotection.ProjectionChange, time.Time) error
}

type projectionRow struct {
	TenantID          int64     `gorm:"column:tenant_id"`
	ProjectionID      string    `gorm:"column:projection_id"`
	ConsumerOwner     string    `gorm:"column:consumer_owner"`
	TargetOwner       string    `gorm:"column:target_owner_module"`
	TargetType        string    `gorm:"column:target_resource_type"`
	TargetIdentity    string    `gorm:"column:target_resource_identity"`
	TargetComponent   string    `gorm:"column:target_component_key"`
	State             string    `gorm:"column:state"`
	Revision          string    `gorm:"column:revision"`
	ProjectionPayload string    `gorm:"column:projection_payload"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

type checkpointRow struct {
	TenantID  int64     `gorm:"column:tenant_id;primaryKey"`
	Cursor    string    `gorm:"column:cursor"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

type resourceKey struct {
	tenantID int64
	owner    string
	typeName string
	identity string
}

type Store struct {
	db              *gorm.DB
	schema          string
	consumerOwner   string
	entriesTable    string
	checkpointTable string
	migrationsTable string
	changeBarrier   ProjectionChangeBarrier
	mu              sync.RWMutex
	byResource      map[resourceKey][]dataprotection.Projection
	localCursors    map[int64]string
}

func New(db *gorm.DB, schema, consumerOwner string, changeBarrier ProjectionChangeBarrier) (*Store, error) {
	schema = strings.TrimSpace(schema)
	consumerOwner = strings.TrimSpace(consumerOwner)
	if db == nil || !schemaNamePattern.MatchString(schema) || !schemaNamePattern.MatchString(consumerOwner) {
		return nil, errors.New("protection projection store requires database, schema and consumer owner")
	}
	store := &Store{
		db: db, schema: schema, consumerOwner: consumerOwner,
		entriesTable:    schema + ".protection_projection_entries",
		checkpointTable: schema + ".protection_projection_checkpoints",
		migrationsTable: schema + ".protection_projection_store_migrations",
		changeBarrier:   changeBarrier,
		byResource:      make(map[resourceKey][]dataprotection.Projection),
		localCursors:    make(map[int64]string),
	}
	if err := store.ensureSchema(); err != nil {
		return nil, err
	}
	if err := store.reload(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) CurrentCursor(ctx context.Context, tenantID int64) (string, error) {
	if s == nil || tenantID <= 0 {
		return "", errors.New("protection projection checkpoint requires tenant")
	}
	var row checkpointRow
	err := s.db.WithContext(ctx).Table(s.checkpointTable).Where("tenant_id = ?", tenantID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read protection projection checkpoint: %w", err)
	}
	return row.Cursor, nil
}

func (s *Store) ApplyBatch(ctx context.Context, tenantID int64, expectedCursor string, batch *dataprotection.ProjectionChangesResponse, now time.Time) error {
	if s == nil || tenantID <= 0 || batch == nil || batch.SchemaVersion != dataprotection.ProjectionChangesSchemaV1 {
		return errors.New("invalid protection projection change batch")
	}
	if batch.NextCursor == "" && (expectedCursor != "" || len(batch.Changes) > 0) {
		return errors.New("invalid protection projection next cursor")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := s.lockCheckpoint(tx, tenantID)
		if err != nil {
			return err
		}
		if checkpoint.Cursor != expectedCursor {
			return errors.New("protection projection checkpoint changed concurrently")
		}
		for _, change := range batch.Changes {
			if err := s.applyChange(tx, tenantID, change, now); err != nil {
				return err
			}
		}
		if s.changeBarrier != nil && len(batch.Changes) > 0 {
			if err := s.changeBarrier.ApplyProjectionChanges(ctx, tx, tenantID, batch.Changes, now); err != nil {
				return fmt.Errorf("apply owner protection projection change barrier: %w", err)
			}
		}
		checkpoint.Cursor = batch.NextCursor
		checkpoint.UpdatedAt = now
		return tx.Table(s.checkpointTable).Save(checkpoint).Error
	})
	if err != nil {
		return err
	}
	if err := s.reloadTenant(ctx, tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	s.localCursors[tenantID] = batch.NextCursor
	s.mu.Unlock()
	return nil
}

// EnsureCurrent refreshes this process's in-memory index when another process
// of the same owner has advanced the shared durable checkpoint. Data-plane
// processes call it once per execution before using Gate or GateAny; this
// keeps request-time decisions local to the owner database and closes the
// acknowledgement-to-worker cache race without a Security request.
func (s *Store) EnsureCurrent(ctx context.Context, tenantID int64) error {
	if s == nil || tenantID <= 0 {
		return errors.New("protection projection refresh requires tenant")
	}
	durableCursor, err := s.CurrentCursor(ctx, tenantID)
	if err != nil {
		return err
	}
	s.mu.RLock()
	localCursor, known := s.localCursors[tenantID]
	s.mu.RUnlock()
	if known && localCursor == durableCursor {
		return nil
	}
	if err := s.reloadTenant(ctx, tenantID); err != nil {
		return err
	}
	s.mu.Lock()
	s.localCursors[tenantID] = durableCursor
	s.mu.Unlock()
	return nil
}

// RequireUnmanaged denies the entire read while an owner has no component-
// level executor. Unmanaged resources continue after one durable checkpoint
// freshness check and in-memory target lookups; no Security call is made.
func (s *Store) RequireUnmanaged(ctx context.Context, tenantID int64, targets []dataprotection.ResourceReference, now time.Time) error {
	if err := s.EnsureCurrent(ctx, tenantID); err != nil {
		return err
	}
	if s.GateAny(tenantID, targets, now) != nil {
		return dataprotection.ErrDenied
	}
	return nil
}

// ManagedTargets returns the locally installed managed-resource index. Owners
// use it before serving requests to replay derived-result convergence after an
// executor upgrade.
func (s *Store) ManagedTargets() []ManagedTarget {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	targets := make([]ManagedTarget, 0, len(s.byResource))
	for key := range s.byResource {
		targets = append(targets, ManagedTarget{
			TenantID: key.tenantID,
			Target: dataprotection.ResourceReference{
				OwnerModule:      key.owner,
				ResourceType:     key.typeName,
				ResourceIdentity: key.identity,
			},
		})
	}
	s.mu.RUnlock()
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].TenantID != targets[j].TenantID {
			return targets[i].TenantID < targets[j].TenantID
		}
		if targets[i].Target.OwnerModule != targets[j].Target.OwnerModule {
			return targets[i].Target.OwnerModule < targets[j].Target.OwnerModule
		}
		if targets[i].Target.ResourceType != targets[j].Target.ResourceType {
			return targets[i].Target.ResourceType < targets[j].Target.ResourceType
		}
		return targets[i].Target.ResourceIdentity < targets[j].Target.ResourceIdentity
	})
	return targets
}

func (s *Store) Gate(tenantID int64, target dataprotection.ResourceReference, now time.Time) GateResult {
	key := resourceKey{tenantID: tenantID, owner: target.OwnerModule, typeName: target.ResourceType, identity: target.ResourceIdentity}
	s.mu.RLock()
	projections := append([]dataprotection.Projection(nil), s.byResource[key]...)
	s.mu.RUnlock()
	if len(projections) == 0 {
		return GateResult{}
	}
	result := GateResult{Managed: true, State: dataprotection.ProjectionStateActive, Projections: projections}
	for _, projection := range projections {
		if projection.State == dataprotection.ProjectionStateEnrolling {
			result.State = dataprotection.ProjectionStateEnrolling
			return result
		}
		if err := projection.Validate(now); err != nil {
			result.Err = err
			return result
		}
	}
	return result
}

// GateAny returns the first managed target in caller-provided canonical order.
// An all-unmanaged request performs only local index lookups and returns nil.
func (s *Store) GateAny(tenantID int64, targets []dataprotection.ResourceReference, now time.Time) *GateMatch {
	if s == nil || tenantID <= 0 {
		return nil
	}
	for _, target := range targets {
		gate := s.Gate(tenantID, target, now)
		if gate.Managed {
			return &GateMatch{Target: target, Gate: gate}
		}
	}
	return nil
}

// HasManagedTargets reports whether any resource in the tenant is installed
// in this owner's local index. Query owners use it to avoid resolving a
// PreparedQuery ReadSet for tenants with no protected resources at all.
func (s *Store) HasManagedTargets(tenantID int64) bool {
	if s == nil || tenantID <= 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key := range s.byResource {
		if key.tenantID == tenantID {
			return true
		}
	}
	return false
}

func (s *Store) lockCheckpoint(tx *gorm.DB, tenantID int64) (*checkpointRow, error) {
	var row checkpointRow
	query := tx.Table(s.checkpointTable)
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("tenant_id = ?", tenantID).First(&row).Error
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lock protection projection checkpoint: %w", err)
	}
	row = checkpointRow{TenantID: tenantID, Cursor: "", UpdatedAt: time.Now().UTC()}
	if err := tx.Table(s.checkpointTable).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create protection projection checkpoint: %w", err)
	}
	return &row, nil
}

func (s *Store) applyChange(tx *gorm.DB, tenantID int64, change dataprotection.ProjectionChange, now time.Time) error {
	if strings.TrimSpace(change.ChangeID) == "" {
		return errors.New("protection projection change ID is required")
	}
	switch change.Operation {
	case dataprotection.ChangeOperationUpsert:
		if change.Projection == nil || change.Release != nil || change.Projection.ConsumerOwner != s.consumerOwner {
			return errors.New("invalid protection projection upsert change")
		}
		validationTime := now
		if change.Projection.State == dataprotection.ProjectionStateEnrolling {
			validationTime = time.Time{}
		}
		if err := change.Projection.Validate(validationTime); err != nil {
			return fmt.Errorf("validate protection projection: %w", err)
		}
		payload, err := json.Marshal(change.Projection)
		if err != nil {
			return fmt.Errorf("encode protection projection: %w", err)
		}
		var current projectionRow
		err = tx.Table(s.entriesTable).Where("tenant_id = ? AND projection_id = ?", tenantID, change.Projection.ProjectionID).First(&current).Error
		if err == nil && current.Revision >= change.Projection.Revision {
			if current.Revision == change.Projection.Revision && current.ProjectionPayload == string(payload) {
				return nil
			}
			return errors.New("protection projection revision conflict")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read local protection projection: %w", err)
		}
		row := projectionRow{
			TenantID: tenantID, ProjectionID: change.Projection.ProjectionID, ConsumerOwner: s.consumerOwner,
			TargetOwner: change.Projection.Target.OwnerModule, TargetType: change.Projection.Target.ResourceType,
			TargetIdentity: change.Projection.Target.ResourceIdentity, TargetComponent: change.Projection.Target.ComponentKey,
			State: change.Projection.State, Revision: change.Projection.Revision,
			ProjectionPayload: string(payload), UpdatedAt: now,
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Table(s.entriesTable).Create(&row).Error
		}
		return tx.Table(s.entriesTable).Where("tenant_id = ? AND projection_id = ?", tenantID, row.ProjectionID).Updates(&row).Error
	case dataprotection.ChangeOperationRelease:
		if change.Release == nil || change.Projection != nil {
			return errors.New("invalid protection projection release change")
		}
		if err := change.Release.Validate(); err != nil {
			return err
		}
		var current projectionRow
		if err := tx.Table(s.entriesTable).Where("tenant_id = ? AND projection_id = ?", tenantID, change.Release.ProjectionID).First(&current).Error; err != nil {
			return fmt.Errorf("release local protection projection: %w", err)
		}
		if current.Revision >= change.Release.Revision || current.TargetOwner != change.Release.Target.OwnerModule || current.TargetType != change.Release.Target.ResourceType || current.TargetIdentity != change.Release.Target.ResourceIdentity || current.TargetComponent != change.Release.Target.ComponentKey {
			return errors.New("protection projection release conflict")
		}
		return tx.Table(s.entriesTable).Where("tenant_id = ? AND projection_id = ?", tenantID, change.Release.ProjectionID).Delete(&projectionRow{}).Error
	default:
		return errors.New("unsupported protection projection change operation")
	}
}

func (s *Store) reload(ctx context.Context) error {
	var rows []projectionRow
	if err := s.db.WithContext(ctx).Table(s.entriesTable).Find(&rows).Error; err != nil {
		return fmt.Errorf("load protection projections: %w", err)
	}
	if err := s.replaceRows(rows, 0); err != nil {
		return err
	}
	var checkpoints []checkpointRow
	if err := s.db.WithContext(ctx).Table(s.checkpointTable).Find(&checkpoints).Error; err != nil {
		return fmt.Errorf("load protection projection checkpoints: %w", err)
	}
	s.mu.Lock()
	for _, checkpoint := range checkpoints {
		s.localCursors[checkpoint.TenantID] = checkpoint.Cursor
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) reloadTenant(ctx context.Context, tenantID int64) error {
	var rows []projectionRow
	if err := s.db.WithContext(ctx).Table(s.entriesTable).Where("tenant_id = ?", tenantID).Find(&rows).Error; err != nil {
		return fmt.Errorf("reload tenant protection projections: %w", err)
	}
	return s.replaceRows(rows, tenantID)
}

func (s *Store) replaceRows(rows []projectionRow, tenantID int64) error {
	loaded := make(map[resourceKey][]dataprotection.Projection)
	for _, row := range rows {
		var projection dataprotection.Projection
		if err := json.Unmarshal([]byte(row.ProjectionPayload), &projection); err != nil {
			return fmt.Errorf("decode local protection projection: %w", err)
		}
		key := resourceKey{tenantID: row.TenantID, owner: row.TargetOwner, typeName: row.TargetType, identity: row.TargetIdentity}
		loaded[key] = append(loaded[key], projection)
	}
	for key := range loaded {
		sort.Slice(loaded[key], func(i, j int) bool { return loaded[key][i].ProjectionID < loaded[key][j].ProjectionID })
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if tenantID == 0 {
		s.byResource = loaded
		return nil
	}
	for key := range s.byResource {
		if key.tenantID == tenantID {
			delete(s.byResource, key)
		}
	}
	for key, projections := range loaded {
		s.byResource[key] = projections
	}
	return nil
}
