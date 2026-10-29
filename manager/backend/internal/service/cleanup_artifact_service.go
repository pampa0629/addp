package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CleanupArtifactTarget is a private frozen PG candidate, never an API DTO.
// Physical identities are read from the registered Owner row, not inferred from
// the source locator. Tokens bind the original generation of every held target.
type CleanupArtifactTarget struct {
	TaskType              string
	ID                    uint
	TenantID              uint
	TaskID                uint
	SourceExecutionID     string
	ItemFingerprint       string
	SourceEngineID        uint
	StorageRef            string
	TargetKind            string
	TargetSchema          string
	TargetTable           string
	Status                string
	UpdatedAt             time.Time
	SizeBytes             int64
	RowDigest             string
	SemanticKey           string
	UnsettledOperationIDs []string
	Tokens                []repository.CleanupTargetToken
}

type CleanupArtifactEffects struct {
	Bucket          string
	Objects         RasterCOGCleaner
	Directories     Model3DTilesArtifactCleaner
	Tables          VectorMaterializedViewDBProvider
	TileInvalidator TileCacheRuntimeCacheInvalidator
}

type CleanupArtifactService struct {
	db      *gorm.DB
	effects CleanupArtifactEffects
}

func NewCleanupArtifactService(db *gorm.DB, effects CleanupArtifactEffects) *CleanupArtifactService {
	return &CleanupArtifactService{db: db, effects: effects}
}

const (
	artifactPrepared   = "prepared"
	artifactSubmitting = "submitting"
	artifactUnknown    = "unknown"
	artifactSucceeded  = "succeeded"
	artifactFinalized  = "finalized"
)

func artifactModel(taskType string) (interface{}, error) {
	switch taskType {
	case execution.TaskTypeVectorTileCacheGeneration:
		return &models.TileCache{}, nil
	case execution.TaskTypeVectorMaterializedViewGeneration:
		return &models.VectorMaterializedView{}, nil
	case execution.TaskTypeRasterCOGGeneration:
		return &models.RasterCOG{}, nil
	case execution.TaskTypeModel3DGLBGeneration:
		return &models.Model3DGLB{}, nil
	case execution.TaskTypeModel3DTilesGeneration:
		return &models.Model3DTiles{}, nil
	case execution.TaskTypeGaussianSplatKSplatGeneration:
		return &models.GaussianSplatKSplat{}, nil
	case execution.TaskTypePointCloudCOPCGeneration:
		return &models.PointCloudCOPC{}, nil
	case execution.TaskTypePPTXPDFGeneration:
		return &models.PPTXPDF{}, nil
	default:
		return nil, commonapi.ErrConflict
	}
}

// artifactRow reads a real registered typed Owner row, including its complete
// versioned identity. Scan inputs never supply the physical deletion address.
func artifactRow(ctx context.Context, db *gorm.DB, taskType string, id, tenant uint, lock bool) (CleanupArtifactTarget, error) {
	target := CleanupArtifactTarget{TaskType: taskType}
	if db == nil || id == 0 || tenant == 0 {
		return target, commonapi.ErrConflict
	}
	model, err := artifactModel(taskType)
	if err != nil {
		return target, err
	}
	q := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenant)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = q.First(model).Error; err != nil {
		return target, err
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return target, err
	}
	sum := sha256.Sum256(raw)
	target.RowDigest = hex.EncodeToString(sum[:])
	value := reflect.ValueOf(model).Elem()
	stringField := func(name string) string {
		f := value.FieldByName(name)
		if !f.IsValid() {
			return ""
		}
		if f.Kind() == reflect.Ptr {
			if f.IsNil() {
				return ""
			}
			f = f.Elem()
		}
		return f.String()
	}
	uintField := func(name string) uint {
		f := value.FieldByName(name)
		if !f.IsValid() {
			return 0
		}
		if f.Kind() == reflect.Ptr {
			if f.IsNil() {
				return 0
			}
			f = f.Elem()
		}
		return uint(f.Uint())
	}
	target.ID = uintField("ID")
	target.TenantID = uintField("TenantID")
	target.TaskID = uintField("TaskID")
	target.SourceExecutionID = stringField("LastExecutionID")
	target.ItemFingerprint = stringField("ItemFingerprint")
	target.SourceEngineID = uintField("SourceEngineID")
	target.StorageRef = stringField("StorageRef")
	target.TargetKind = stringField("TargetKind")
	target.TargetSchema = stringField("TargetSchema")
	target.TargetTable = stringField("TargetTable")
	target.Status = stringField("Status")
	target.SemanticKey = target.ItemFingerprint
	switch row := model.(type) {
	case *models.TileCache:
		target.SemanticKey = target.ItemFingerprint + ":" + row.ProfileHash
	case *models.VectorMaterializedView:
		target.SemanticKey = target.ItemFingerprint + ":" + row.SourceGeometryColumn + ":" + fmt.Sprint(row.TargetSRID)
	case *models.Model3DTiles:
		target.SemanticKey = target.ItemFingerprint + ":" + row.TargetFormat
	case *models.PPTXPDF:
		target.SemanticKey = target.ItemFingerprint + ":" + row.ArtifactVariant
	}
	target.UpdatedAt = value.FieldByName("UpdatedAt").Interface().(time.Time)
	if f := value.FieldByName("SizeBytes"); f.IsValid() {
		target.SizeBytes = f.Int()
	}
	if target.ItemFingerprint == "" || target.Status == "abandoned_external" {
		return target, commonapi.ErrConflict
	}
	if taskType == execution.TaskTypeModel3DGLBGeneration {
		if err := repository.ValidateModel3DGLBCleanupLifecycle(model.(*models.Model3DGLB)); err != nil {
			return target, err
		}
	}
	return target, nil
}

func (s *CleanupArtifactService) targetKeys(t CleanupArtifactTarget) ([]repository.CleanupTargetKey, error) {
	keys := []repository.CleanupTargetKey{
		{TenantID: t.TenantID, Kind: repository.CleanupTargetArtifact, Identity: fmt.Sprintf("%s:%d", t.TaskType, t.ID)},
		{TenantID: t.TenantID, Kind: repository.CleanupTargetGeneration, Identity: t.TaskType + ":" + t.SemanticKey},
	}
	if t.TaskID > 0 {
		keys = append(keys, repository.CleanupTargetKey{TenantID: t.TenantID, Kind: repository.CleanupTargetGeneration, Identity: t.TaskType + ":task:" + fmt.Sprint(t.TaskID)})
	}
	if t.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
		if t.SourceEngineID == 0 || t.TargetKind != models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView || t.TargetSchema == "" || t.TargetTable == "" {
			return nil, commonapi.ErrConflict
		}
		b, _ := json.Marshal([]interface{}{t.SourceEngineID, t.TargetSchema, t.TargetTable})
		keys = append(keys, repository.CleanupTargetKey{TenantID: t.TenantID, Kind: repository.CleanupTargetTable, Identity: string(b)})
	} else if t.StorageRef != "" {
		bucket, object, err := cog.ObjectLocation(t.StorageRef, s.effects.Bucket)
		if err != nil {
			return nil, err
		}
		if s.effects.Bucket == "" || bucket != s.effects.Bucket || !strings.HasPrefix(object, fmt.Sprintf("tenant_%d/", t.TenantID)) || path.Clean(object) != object || strings.ContainsRune(object, '\x00') {
			return nil, commonapi.ErrConflict
		}
		b, _ := json.Marshal([]string{bucket, object})
		keys = append(keys, repository.CleanupTargetKey{TenantID: t.TenantID, Kind: repository.CleanupTargetObject, Identity: string(b)})
	}
	return keys, nil
}

func (s *CleanupArtifactService) Freeze(ctx context.Context, tenantID uint, tiles []*models.TileCache, vectors []*models.VectorMaterializedView, artifacts []repository.CleanupManagedArtifact) ([]CleanupArtifactTarget, error) {
	targets := []CleanupArtifactTarget{}
	seen := map[string]bool{}
	add := func(taskType string, id, tenant uint) error {
		if tenant != tenantID {
			return commonapi.ErrConflict
		}
		key := fmt.Sprintf("%s:%d", taskType, id)
		if seen[key] {
			return commonapi.ErrConflict
		}
		seen[key] = true
		t, err := artifactRow(ctx, s.db, taskType, id, tenant, false)
		if err != nil {
			return err
		}
		keys, err := s.targetKeys(t)
		if err != nil {
			return err
		}
		for _, key := range keys {
			token, held, err := repository.ReadCleanupTargetSnapshot(ctx, s.db, key)
			if err != nil {
				return err
			}
			t.Tokens = append(t.Tokens, token)
			if held != "" {
				t.UnsettledOperationIDs = append(t.UnsettledOperationIDs, held)
			}
		}
		targets = append(targets, t)
		return nil
	}
	for _, v := range tiles {
		if v == nil {
			return nil, commonapi.ErrConflict
		}
		if err := add(execution.TaskTypeVectorTileCacheGeneration, v.ID, v.TenantID); err != nil {
			return nil, err
		}
	}
	for _, v := range vectors {
		if v == nil {
			return nil, commonapi.ErrConflict
		}
		if err := add(execution.TaskTypeVectorMaterializedViewGeneration, v.ID, v.TenantID); err != nil {
			return nil, err
		}
	}
	for _, v := range artifacts {
		if err := add(v.TaskType, v.ID, v.TenantID); err != nil {
			return nil, err
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].TaskType != targets[j].TaskType {
			return targets[i].TaskType < targets[j].TaskType
		}
		return targets[i].ID < targets[j].ID
	})
	return targets, nil
}

func lockArtifactSource(ctx context.Context, tx *gorm.DB, t CleanupArtifactTarget) error {
	if t.Status == "building" || t.Status == "generating" {
		return commonapi.ErrConflict
	}
	if t.TaskID > 0 {
		var task models.TaskDefinition
		if err := tx.WithContext(ctx).Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND task_type = ?", t.TaskID, t.TenantID, t.TaskType).First(&task).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	fresh, err := artifactRow(ctx, tx, t.TaskType, t.ID, t.TenantID, true)
	if err != nil {
		return err
	}
	if fresh.RowDigest != t.RowDigest {
		return commonapi.ErrConflict
	}
	if t.SourceExecutionID != "" {
		var source execution.TaskExecution
		if err := tx.Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ?", t.SourceExecutionID, t.TenantID, execution.ModuleManager, t.TaskType).First(&source).Error; err != nil {
			return err
		}
		if source.Status != execution.ExecutionStatusSuccess {
			var writer models.CleanupArtifactOperation
			if err := tx.Where("id = ? AND execution_id = ? AND tenant_id = ? AND origin = ? AND state = ?", source.ExecutionID, source.ExecutionID, t.TenantID, "generation", "succeeded").First(&writer).Error; err != nil {
				return commonapi.ErrConflict
			}
			if writer.BindingDigest != repository.ArtifactOperationBinding(writer) {
				return commonapi.ErrConflict
			}
		}
	}
	var active int64
	q := tx.WithContext(ctx).Model(&execution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND status IN ?", int(t.TenantID), execution.ModuleManager, t.TaskType, []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning})
	if t.TaskID > 0 {
		q = q.Where("source_task_id = ?", fmt.Sprint(t.TaskID))
	} else if t.SourceExecutionID != "" {
		q = q.Where("execution_id = ?", t.SourceExecutionID)
	} else {
		return nil
	}
	if err := q.Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return commonapi.ErrConflict
	}
	return nil
}

func (s *CleanupArtifactService) Prepare(ctx context.Context, tx *gorm.DB, lease execution.Lease, target CleanupArtifactTarget, cleanupMode, externalPolicy string) (string, error) {
	child, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return "", err
	}
	if child.Module != execution.ModuleManager || child.TaskType != execution.TaskTypeCleanupExecutor || child.TenantID != int(target.TenantID) {
		return "", commonapi.ErrConflict
	}
	input, inputErr := execution.CleanupRequestFromExecution(*child)
	if inputErr != nil || input.Action != events.CleanupActionExecute || input.CleanupMode != cleanupMode || input.Context.ExternalArtifactPolicy != externalPolicy {
		return "", commonapi.ErrConflict
	}
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return "", err
	}
	action := "logical"
	if cleanupMode == events.CleanupModePhysical {
		if externalPolicy != commonModels.ExternalArtifactPolicyDelete && externalPolicy != commonModels.ExternalArtifactPolicyAbandon && !(externalPolicy == "" && input.Context.EngineID == 0) {
			return "", commonapi.ErrConflict
		}
		action = "delete"
		if target.TaskType == execution.TaskTypeVectorMaterializedViewGeneration && externalPolicy == commonModels.ExternalArtifactPolicyAbandon {
			action = "abandon"
		}
	}
	return s.prepareArtifactOperation(ctx, tx, lease, target, action, "cleanup")
}

func (s *CleanupArtifactService) prepareArtifactOperation(ctx context.Context, tx *gorm.DB, lease execution.Lease, target CleanupArtifactTarget, action, origin string) (string, error) {
	if err := lockArtifactSource(ctx, tx, target); err != nil {
		return "", err
	}
	keys, err := s.targetKeys(target)
	if err != nil {
		return "", err
	}
	if len(keys) != len(target.Tokens) {
		return "", commonapi.ErrConflict
	}
	for i, key := range keys {
		if key != target.Tokens[i].Key {
			return "", commonapi.ErrConflict
		}
	}
	id := uuid.NewString()
	if err := repository.HoldCleanupTargets(ctx, tx, target.Tokens, id); err != nil {
		return "", err
	}
	if action == "delete" {
		if err := s.requireExclusivePhysicalReference(ctx, tx, target); err != nil {
			return "", err
		}
	}
	if origin == "cleanup" {
		if _, err := execution.LockActiveLease(ctx, tx, lease); err != nil {
			return "", err
		}
	}
	snapshot, _ := json.Marshal(target)
	tokens, _ := json.Marshal(target.Tokens)
	op := models.CleanupArtifactOperation{ID: id, TenantID: target.TenantID, Origin: origin, ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, LeaseOwner: lease.Owner, Action: action, Snapshot: string(snapshot), TargetKeys: string(tokens), State: artifactPrepared}
	op.BindingDigest = repository.ArtifactOperationBinding(op)
	if action != "delete" || target.StorageRef == "" && target.TaskType != execution.TaskTypeVectorMaterializedViewGeneration {
		op.State = artifactSucceeded
	}
	if err := tx.WithContext(ctx).Create(&op).Error; err != nil {
		return "", err
	}
	return id, nil
}

func readArtifactOperation(ctx context.Context, db *gorm.DB, id string, lock bool) (models.CleanupArtifactOperation, CleanupArtifactTarget, error) {
	var op models.CleanupArtifactOperation
	var t CleanupArtifactTarget
	q := db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Where("id = ?", id).First(&op).Error; err != nil {
		return op, t, err
	}
	if repository.ArtifactOperationBinding(op) != op.BindingDigest || json.Unmarshal([]byte(op.Snapshot), &t) != nil || t.TenantID != op.TenantID {
		return op, t, commonapi.ErrConflict
	}
	b, _ := json.Marshal(t.Tokens)
	if string(b) != op.TargetKeys {
		return op, t, commonapi.ErrConflict
	}
	return op, t, nil
}

// Effect never holds a PG transaction across the provider request. The CAS is
// intentionally one-shot: a crash in submitting, timeout or partial response
// keeps the durable hold and cannot start a second DELETE after restart.
func (s *CleanupArtifactService) Effect(ctx context.Context, id string) error {
	return s.effectArtifactOperation(ctx, id, "")
}

func (s *CleanupArtifactService) effectArtifactOperation(ctx context.Context, id, manualID string) error {
	var op models.CleanupArtifactOperation
	var t CleanupArtifactTarget
	skip := false
	original, _, err := readArtifactOperation(ctx, s.db, id, false)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if manualID != "" {
			if original.Origin != "manual" || manualID != id {
				return commonapi.ErrConflict
			}
		} else {
			lease, ok := execution.LeaseFromContext(ctx)
			if !ok || original.Origin != "cleanup" || lease.ExecutionID != original.ExecutionID || lease.TenantID != int(original.TenantID) || lease.Attempt != original.Attempt || lease.Token != original.LeaseToken || lease.Owner != original.LeaseOwner {
				return commonapi.ErrConflict
			}
			if _, err := execution.LockActiveLease(ctx, tx, lease); err != nil {
				return err
			}
		}

		var err error
		op, t, err = readArtifactOperation(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if op.State == artifactSucceeded || op.State == artifactFinalized {
			skip = true
			return nil
		}
		if op.State != artifactPrepared || op.BindingDigest != original.BindingDigest {
			return commonapi.ErrConflict
		}

		return tx.Model(&op).Update("state", artifactSubmitting).Error
	})
	if err != nil || skip {
		return err
	}
	effectErr := s.deletePhysical(ctx, t)
	state := artifactSucceeded
	if effectErr != nil {
		state = artifactUnknown
		if artifactDeleteRejected(effectErr) {
			state = "rejected"
		}
	}
	// Use an independent short context so loss of the caller deadline cannot erase
	// a provider's exact response. This only appends the bound operation fact.
	factCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	result := s.db.WithContext(factCtx).Model(&models.CleanupArtifactOperation{}).Where("id = ? AND binding_digest = ? AND state = ?", id, op.BindingDigest, artifactSubmitting).Updates(map[string]interface{}{"state": state, "effect_at": now})
	if result.Error != nil {
		return errors.Join(effectErr, result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Join(effectErr, commonapi.ErrConflict)
	}
	if state == "rejected" && op.Origin == "manual" {
		releaseErr := s.db.WithContext(factCtx).Transaction(func(tx *gorm.DB) error { return repository.FinishCleanupTargets(factCtx, tx, t.Tokens, op.ID, false) })
		return errors.Join(effectErr, releaseErr)
	}
	return effectErr
}

func (s *CleanupArtifactService) deletePhysical(ctx context.Context, t CleanupArtifactTarget) error {
	if t.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
		if s.effects.Tables == nil {
			return errors.New("materialized view provider unavailable")
		}
		db, err := s.effects.Tables.GetPostGISDB(ctx, &t.TenantID, t.SourceEngineID)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+spatial.QualifiedPostGISTable(t.TargetSchema, t.TargetTable))
		if err != nil {
			return fmt.Errorf("drop vector materialized view target: %w", err)
		}
		return nil
	}
	if t.StorageRef == "" {
		return nil
	}
	if t.TaskType == execution.TaskTypeVectorTileCacheGeneration {
		if s.effects.TileInvalidator == nil {
			return errors.New("tile runtime invalidator unavailable")
		}
		if err := s.effects.TileInvalidator.InvalidateTileCacheRuntimeCache(ctx, t.TenantID, t.ID); err != nil {
			return err
		}
	}
	if t.TaskType == execution.TaskTypeModel3DTilesGeneration {
		if s.effects.Directories == nil {
			return errors.New("managed directory cleaner unavailable")
		}
		return s.effects.Directories.DeleteByStorageRef(ctx, t.StorageRef)
	}
	if s.effects.Objects == nil {
		return errors.New("managed object cleaner unavailable")
	}
	return s.effects.Objects.DeleteByStorageRef(ctx, t.StorageRef)
}

func (s *CleanupArtifactService) Finalize(ctx context.Context, tx *gorm.DB, lease execution.Lease, id string) (ManagerCleanupStats, error) {
	stats := ManagerCleanupStats{}
	child, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return stats, err
	}
	op, t, err := readArtifactOperation(ctx, tx, id, true)
	if err != nil {
		return stats, err
	}
	if child.Module != execution.ModuleManager || child.TaskType != execution.TaskTypeCleanupExecutor || op.Origin != "cleanup" || op.ExecutionID != lease.ExecutionID || op.TenantID != uint(lease.TenantID) || op.Attempt != lease.Attempt || op.LeaseToken != lease.Token || op.LeaseOwner != lease.Owner || op.State != artifactSucceeded {
		return stats, commonapi.ErrConflict
	}
	return s.finalizeArtifactOperation(ctx, tx, lease, child, op, t)
}

func (s *CleanupArtifactService) finalizeArtifactOperation(ctx context.Context, tx *gorm.DB, lease execution.Lease, child *execution.TaskExecution, op models.CleanupArtifactOperation, t CleanupArtifactTarget) (ManagerCleanupStats, error) {
	stats := ManagerCleanupStats{}
	if err := lockArtifactSource(ctx, tx, t); err != nil {
		return stats, err
	}
	model, _ := artifactModel(t.TaskType)
	updates := map[string]interface{}{"status": "deleted", "error_message": "resource reclaim: missing source", "updated_at": time.Now().UTC()}
	if op.Action == "abandon" {
		var row models.VectorMaterializedView
		if err := tx.Where("id = ? AND tenant_id = ?", t.ID, t.TenantID).First(&row).Error; err != nil {
			return stats, err
		}
		metadata := row.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["external_artifact_policy"] = commonModels.ExternalArtifactPolicyAbandon
		metadata["abandoned_external_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		metadata["abandoned_external_execution_id"] = lease.ExecutionID
		input, err := execution.CleanupRequestFromExecution(*child)
		if err != nil {
			return stats, err
		}
		metadata["abandoned_external_by"] = input.RequestedBy
		metadata["last_cleanup_error"] = row.ErrorMessage
		updates["status"] = models.VectorMaterializedViewStatusAbandonedExternal
		updates["metadata"] = metadata
		stats.AbandonedExternal = 1
	} else if op.Action == "delete" {
		updates["deleted_at"] = time.Now().UTC()
		if t.StorageRef != "" || t.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
			stats.DeletedPhysicalArtifacts = 1
			stats.FreedBytes = t.SizeBytes
		}
	} else {
		stats.MarkedMissingSource = 1
		if t.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
			stats.SkippedExternalTargets = 1
		}
	}
	r := tx.WithContext(ctx).Model(model).Where("id = ? AND tenant_id = ?", t.ID, t.TenantID).Updates(updates)
	if r.Error != nil {
		return stats, r.Error
	}
	if r.RowsAffected != 1 {
		return stats, commonapi.ErrConflict
	}
	if err := repository.FinishCleanupTargets(ctx, tx, t.Tokens, op.ID, true); err != nil {
		return stats, err
	}
	if child != nil {
		if _, err := execution.LockActiveLease(ctx, tx, lease); err != nil {
			return stats, err
		}
	}
	now := time.Now().UTC()
	if err := tx.Model(&op).Updates(map[string]interface{}{"state": artifactFinalized, "finalized_at": now}).Error; err != nil {
		return stats, err
	}
	switch t.TaskType {
	case execution.TaskTypeVectorTileCacheGeneration:
		stats.TileCaches = 1
	case execution.TaskTypeVectorMaterializedViewGeneration:
		stats.VectorMaterializedViews = 1
	default:
		stats.ManagedArtifacts = 1
	}
	return stats, nil
}

// DeleteManual shares the durable target hold and exactly-once physical effect
// with Cleanup. The caller has already passed the existing Owner authorization.
func (s *CleanupArtifactService) DeleteManual(ctx context.Context, taskType string, id, tenant uint) error {
	t, err := artifactRow(ctx, s.db, taskType, id, tenant, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if err != nil {
		return err
	}
	if t.StorageRef != "" {
		if t.TaskType == execution.TaskTypeModel3DTilesGeneration {
			if s.effects.Directories == nil {
				return errors.New("managed directory cleaner is not configured")
			}
		} else {
			if s.effects.Objects == nil {
				return errors.New("managed object cleaner is not configured")
			}
			if c, ok := s.effects.Objects.(*MinIOManagedObjectCleaner); ok && (c == nil || c.minioClient == nil) {
				return errors.New("managed object cleaner is not configured")
			}
		}
	}
	keys, err := s.targetKeys(t)
	if err != nil {
		return err
	}
	for _, key := range keys {
		token, err := repository.ObserveCleanupTarget(ctx, s.db, key)
		if err != nil {
			return err
		}
		t.Tokens = append(t.Tokens, token)
	}
	var operationID string
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		operationID, err = s.prepareArtifactOperation(ctx, tx, execution.Lease{}, t, "delete", "manual")
		return err
	}); err != nil {
		return err
	}
	if err := s.effectArtifactOperation(ctx, operationID, operationID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		op, current, err := readArtifactOperation(ctx, tx, operationID, true)
		if err != nil {
			return err
		}
		if op.Origin != "manual" || op.State != artifactSucceeded || op.TenantID != tenant {
			return commonapi.ErrConflict
		}
		_, err = s.finalizeArtifactOperation(ctx, tx, execution.Lease{}, nil, op, current)
		return err
	})
}

// S3's explicit authorization rejection is a known non-dispatch response. A
// timeout, transport failure or generic provider error remains unknown.
func artifactDeleteRejected(err error) bool {
	var response minio.ErrorResponse
	return errors.As(err, &response) && response.StatusCode == 403 && response.Code == "AccessDenied"
}

// Shared physical addresses (including directory/object overlap) are ownership
// conflicts, not a reason to expand this confirmed deletion to another row.
func (s *CleanupArtifactService) requireExclusivePhysicalReference(ctx context.Context, tx *gorm.DB, t CleanupArtifactTarget) error {
	if t.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
		var count int64
		if err := tx.Model(&models.VectorMaterializedView{}).Where("tenant_id = ? AND id <> ? AND source_engine_id = ? AND target_schema = ? AND target_table = ?", t.TenantID, t.ID, t.SourceEngineID, t.TargetSchema, t.TargetTable).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return commonapi.ErrConflict
		}
		return nil
	}
	if t.StorageRef == "" {
		return nil
	}
	bucket, object, err := cog.ObjectLocation(t.StorageRef, s.effects.Bucket)
	if err != nil {
		return err
	}
	for _, taskType := range []string{execution.TaskTypeVectorTileCacheGeneration, execution.TaskTypeRasterCOGGeneration, execution.TaskTypeModel3DGLBGeneration, execution.TaskTypeModel3DTilesGeneration, execution.TaskTypeGaussianSplatKSplatGeneration, execution.TaskTypePointCloudCOPCGeneration, execution.TaskTypePPTXPDFGeneration} {
		model, _ := artifactModel(taskType)
		var rows []struct {
			ID         uint
			StorageRef string
		}
		if err := tx.Model(model).Select("id", "storage_ref").Where("tenant_id = ? AND storage_ref <> ''", t.TenantID).Limit(100001).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 100000 {
			return errors.New("physical ownership inventory exceeds complete verification budget")
		}
		for _, row := range rows {
			if taskType == t.TaskType && row.ID == t.ID {
				continue
			}
			otherBucket, otherObject, err := cog.ObjectLocation(row.StorageRef, s.effects.Bucket)
			if err != nil || otherBucket != bucket {
				continue
			}
			if otherObject == object || t.TaskType == execution.TaskTypeModel3DTilesGeneration && strings.HasPrefix(otherObject, strings.TrimSuffix(object, "/")+"/") || taskType == execution.TaskTypeModel3DTilesGeneration && strings.HasPrefix(object, strings.TrimSuffix(otherObject, "/")+"/") {
				return commonapi.ErrConflict
			}
		}
	}
	return nil
}
