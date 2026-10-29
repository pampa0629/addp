package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/tilecache"
	"gorm.io/gorm"
)

const artifactGenerationTokensKey = "cleanup_generation_targets"

func artifactGenerationType(taskType string) bool {
	if taskType == execution.TaskTypeVectorTileCacheGeneration || taskType == execution.TaskTypeVectorMaterializedViewGeneration {
		return true
	}
	for _, spec := range ManagerManagedArtifactSpecs() {
		if spec.TaskType == taskType {
			return true
		}
	}
	return false
}

// A generation acquires its logical target before enqueue, not when an old
// worker eventually publishes its PG result. Failed/expired attempts do not
// establish that their remote writer has stopped, so they retain the hold.
func holdArtifactGeneration(ctx context.Context, tx *gorm.DB, item *execution.TaskExecution, config commonmodels.JSONMap) error {
	if !artifactGenerationType(item.TaskType) {
		return nil
	}
	tokens := []CleanupTargetToken{}
	identities := []string{item.TaskType + ":task:" + fmt.Sprint(*item.SourceTaskID)}
	if semantic := taskSemanticKey(item.TaskType, config); semantic != "" {
		identities = append(identities, item.TaskType+":"+semantic)
	}
	for _, identity := range identities {
		token, err := ObserveCleanupTarget(ctx, tx, CleanupTargetKey{TenantID: uint(item.TenantID), Kind: CleanupTargetGeneration, Identity: identity})
		if err != nil {
			return err
		}
		tokens = append(tokens, token)
	}
	result, _ := jsonMap(item.ExecutionConfig["result"])
	if result == nil {
		result, _ = jsonMap(config["result"])
	}
	ref := firstString(result, "storage_ref")
	if item.TaskType == execution.TaskTypeVectorTileCacheGeneration {
		target, _ := jsonMap(config["target"])
		fp := firstString(target, "item_fingerprint")
		profile := firstString(config, "profile_hash")
		if fp != "" && profile != "" {
			ref = tilecache.ObjectStorageRef(uint(item.TenantID), fp, profile)
		}
	}
	if ref != "" {
		bucket, object, err := cog.ObjectLocation(ref, "")
		if err != nil || bucket == "" {
			return commonapi.ErrConflict
		}
		value, _ := json.Marshal([]string{bucket, object})
		token, err := ObserveCleanupTarget(ctx, tx, CleanupTargetKey{TenantID: uint(item.TenantID), Kind: CleanupTargetObject, Identity: string(value)})
		if err != nil {
			return err
		}
		tokens = append(tokens, token)
	}
	if item.TaskType == execution.TaskTypeVectorMaterializedViewGeneration {
		target, _ := jsonMap(config["target"])
		geometry, _ := jsonMap(config["geometry"])
		storage, _ := jsonMap(config["storage"])
		fp := firstString(target, "item_fingerprint")
		engine := firstUint(target, "engine_id")
		schema := firstString(storage, "target_schema")
		column := firstString(geometry, "geometry_column")
		srid := firstUint(geometry, "target_srid")
		if fp != "" && engine > 0 && schema != "" && column != "" && srid > 0 {
			table := VectorMaterializedViewTargetTable(uint(item.TenantID), fp, column, int(srid))
			value, _ := json.Marshal([]interface{}{engine, schema, table})
			token, err := ObserveCleanupTarget(ctx, tx, CleanupTargetKey{TenantID: uint(item.TenantID), Kind: CleanupTargetTable, Identity: string(value)})
			if err != nil {
				return err
			}
			tokens = append(tokens, token)
		}
	}
	if err := HoldCleanupTargets(ctx, tx, tokens, item.ExecutionID); err != nil {
		return err
	}
	b, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	item.ExecutionConfig[artifactGenerationTokensKey] = string(b)
	return nil
}

func finishArtifactGeneration(ctx context.Context, tx *gorm.DB, item execution.TaskExecution, status string) error {
	if !artifactGenerationType(item.TaskType) {
		return nil
	}
	raw, ok := item.ExecutionConfig[artifactGenerationTokensKey].(string)
	if !ok {
		return commonapi.ErrConflict
	}
	var tokens []CleanupTargetToken
	if json.Unmarshal([]byte(raw), &tokens) != nil || len(tokens) == 0 || tokens[0].Key.TenantID != uint(item.TenantID) || tokens[0].Key.Kind != CleanupTargetGeneration {
		return commonapi.ErrConflict
	}
	var op models.CleanupArtifactOperation
	err := tx.Where("id = ? AND origin = ?", item.ExecutionID, "generation").First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FinishCleanupTargets(ctx, tx, tokens, item.ExecutionID, false)
	}
	if err != nil {
		return err
	}
	if op.BindingDigest != ArtifactOperationBinding(op) || op.ExecutionID != item.ExecutionID || op.TenantID != uint(item.TenantID) {
		return commonapi.ErrConflict
	}
	if op.State == "succeeded" {
		return FinishCleanupTargets(ctx, tx, tokens, item.ExecutionID, false)
	}
	if status == execution.ExecutionStatusSuccess {
		return commonapi.ErrConflict
	}

	for _, token := range tokens {
		var target models.CleanupTarget
		id, err := token.Key.digest()
		if err != nil {
			return err
		}
		if err := tx.Where("key = ? AND operation_id = ? AND generation = ?", id, item.ExecutionID, token.Generation).First(&target).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *TileCacheRepository) GetDB() *gorm.DB              { return r.db }
func (r *VectorMaterializedViewRepository) GetDB() *gorm.DB { return r.db }
func (r *RasterCOGRepository) GetDB() *gorm.DB              { return r.db }
func (r *Model3DGLBRepository) GetDB() *gorm.DB             { return r.db }
func (r *Model3DTilesRepository) GetDB() *gorm.DB           { return r.db }
func (r *GaussianSplatKSplatRepository) GetDB() *gorm.DB    { return r.db }
func (r *PointCloudCOPCRepository) GetDB() *gorm.DB         { return r.db }
func (r *PPTXPDFRepository) GetDB() *gorm.DB                { return r.db }

// VectorMaterializedViewTargetTable is the sole physical target-name derivation
// shared by enqueue admission and the real build plan.
func VectorMaterializedViewTargetTable(tenant uint, fp, column string, srid int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%d", tenant, fp, column, srid)))
	return "addp_vmv_" + hex.EncodeToString(sum[:])[:24]
}
