package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CleanupTargetKey struct {
	TenantID uint
	Kind     string
	Identity string
}

// CleanupTargetToken must be observed before remote preparation or queueing,
// then retained with that original request/execution until its PG commit.
type CleanupTargetToken struct {
	Key        CleanupTargetKey
	Generation uint64
}

const (
	CleanupTargetPreviewState = "preview_state"
	CleanupTargetEmbedding    = "embedding"
	CleanupTargetGeneration   = "generation"
	CleanupTargetArtifact     = "artifact"
	CleanupTargetObject       = "object"
	CleanupTargetTable        = "materialized_view"
)

func (key CleanupTargetKey) digest() (string, error) {
	if key.TenantID == 0 || key.Identity == "" || key.Identity != strings.TrimSpace(key.Identity) || strings.ContainsRune(key.Identity, '\x00') {
		return "", commonapi.ErrConflict
	}
	switch key.Kind {
	case CleanupTargetPreviewState, CleanupTargetEmbedding, CleanupTargetGeneration, CleanupTargetArtifact, CleanupTargetObject, CleanupTargetTable:
	default:
		return "", commonapi.ErrConflict
	}
	value, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}

func MigrateCleanupArtifactSchema(db *gorm.DB) error {
	if db == nil {
		return commonapi.ErrConflict
	}
	return db.AutoMigrate(&models.CleanupTarget{}, &models.CleanupArtifactOperation{})
}

func cleanupTargetTransaction(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil {
		return commonapi.ErrConflict
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("Manager cleanup target write requires a transaction")
	}
	return nil
}

// ObserveCleanupTarget is read-only, including before the first Cleanup. A
// missing target has generation zero; a later Cleanup invalidates that token.
func ObserveCleanupTarget(ctx context.Context, db *gorm.DB, key CleanupTargetKey) (CleanupTargetToken, error) {
	token, held, err := ReadCleanupTargetSnapshot(ctx, db, key)
	if err != nil {
		return token, err
	}
	if held != "" {
		return token, commonapi.ErrConflict
	}
	return token, nil
}

func ReadCleanupTargetSnapshot(ctx context.Context, db *gorm.DB, key CleanupTargetKey) (CleanupTargetToken, string, error) {
	token := CleanupTargetToken{Key: key}
	if db != nil {
		db = db.Session(&gorm.Session{NewDB: true, Context: ctx}).Table(models.CleanupTarget{}.TableName())
	}
	id, err := key.digest()
	if err != nil || db == nil {
		return token, "", commonapi.ErrConflict
	}
	var target models.CleanupTarget
	if err := db.WithContext(ctx).Where("key = ?", id).Take(&target).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return token, "", nil
	} else if err != nil {
		return token, "", err
	}
	if target.TenantID != key.TenantID || target.Kind != key.Kind || target.Identity != key.Identity {
		return token, "", commonapi.ErrConflict
	}
	token.Generation = target.Generation
	return token, target.OperationID, nil
}

func lockCleanupTarget(ctx context.Context, tx *gorm.DB, token CleanupTargetToken) (*models.CleanupTarget, error) {
	if err := cleanupTargetTransaction(tx); err != nil {
		return nil, err
	}
	tx = tx.Session(&gorm.Session{NewDB: true, Context: ctx}).Table(models.CleanupTarget{}.TableName())
	id, err := token.Key.digest()
	if err != nil {
		return nil, err
	}
	initial := models.CleanupTarget{Key: id, TenantID: token.Key.TenantID, Kind: token.Key.Kind, Identity: token.Key.Identity}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return nil, err
	}
	var target models.CleanupTarget
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", id).Take(&target).Error; err != nil {
		return nil, err
	}
	if target.TenantID != token.Key.TenantID || target.Kind != token.Key.Kind || target.Identity != token.Key.Identity || target.Generation != token.Generation || target.OperationID != "" {
		return nil, commonapi.ErrConflict
	}
	return &target, nil
}

// GuardCleanupTargetWrite must share the transaction of the actual upsert or
// enqueue. Re-observing after HTTP returns would admit the stale request again.
func GuardCleanupTargetWrite(ctx context.Context, tx *gorm.DB, token CleanupTargetToken) error {
	_, err := lockCleanupTarget(ctx, tx, token)
	return err
}

// HoldCleanupTargets serializes a durable operation with every original writer.
// The caller holds its C/task/result locks first. No external request belongs in
// this transaction. A hold is not cleared merely because an execution expires.
func HoldCleanupTargets(ctx context.Context, tx *gorm.DB, tokens []CleanupTargetToken, operationID string) error {
	if operationID == "" || len(tokens) == 0 {
		return commonapi.ErrConflict
	}
	if err := lockPhysicalTargetRegistry(ctx, tx, tokens); err != nil {
		return err
	}
	ordered := append([]CleanupTargetToken(nil), tokens...)
	sort.Slice(ordered, func(i, j int) bool { a, _ := ordered[i].Key.digest(); b, _ := ordered[j].Key.digest(); return a < b })
	last := ""
	for _, token := range ordered {
		id, err := token.Key.digest()
		if err != nil || id == last {
			return commonapi.ErrConflict
		}
		last = id
		target, err := lockCleanupTarget(ctx, tx, token)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Model(target).Update("operation_id", operationID).Error; err != nil {
			return err
		}
	}
	return nil
}

// FinishCleanupTargets is atomic with the result publication/deletion. The
// operation's durable effect fact is deliberately outside this transaction.
func FinishCleanupTargets(ctx context.Context, tx *gorm.DB, tokens []CleanupTargetToken, operationID string, advance bool) error {
	if tx != nil {
		tx = tx.Session(&gorm.Session{NewDB: true, Context: ctx}).Table(models.CleanupTarget{}.TableName())
	}
	if err := cleanupTargetTransaction(tx); err != nil {
		return err
	}
	if operationID == "" || len(tokens) == 0 {
		return commonapi.ErrConflict
	}
	ordered := append([]CleanupTargetToken(nil), tokens...)
	sort.Slice(ordered, func(i, j int) bool { a, _ := ordered[i].Key.digest(); b, _ := ordered[j].Key.digest(); return a < b })
	for _, token := range ordered {
		id, err := token.Key.digest()
		if err != nil {
			return err
		}
		var target models.CleanupTarget
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", id).Take(&target).Error; err != nil {
			return err
		}
		if target.TenantID != token.Key.TenantID || target.Kind != token.Key.Kind || target.Identity != token.Key.Identity || target.Generation != token.Generation || target.OperationID != operationID || target.Generation >= math.MaxInt64 {
			return commonapi.ErrConflict
		}
		generation := target.Generation
		if advance {
			generation++
		}
		if err := tx.WithContext(ctx).Model(&target).Updates(map[string]interface{}{"operation_id": "", "generation": generation}).Error; err != nil {
			return err
		}
	}
	return nil
}

// FenceCleanupTarget is for the PG-only Cleanup portion. Its generation change
// rolls back together with the exact projection deletion and final C receipt.
func FenceCleanupTarget(ctx context.Context, tx *gorm.DB, lease execution.Lease, token CleanupTargetToken) error {
	if err := cleanupTargetTransaction(tx); err != nil {
		return err
	}
	child, err := execution.LockActiveLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if child.Module != execution.ModuleManager || child.TaskType != execution.TaskTypeCleanupExecutor || child.TenantID <= 0 || uint(child.TenantID) != token.Key.TenantID {
		return commonapi.ErrConflict
	}
	target, err := lockCleanupTarget(ctx, tx, token)
	if err != nil {
		return err
	}
	if target.Generation >= math.MaxInt64 {
		return commonapi.ErrConflict
	}
	if _, err := execution.LockActiveLease(ctx, tx, lease); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(target).Update("generation", target.Generation+1).Error
}
