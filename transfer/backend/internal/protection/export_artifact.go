package protection

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type exportExecutionKey struct{}

// WithExportExecution uses only the worker's frozen, persisted execution.
// Ordinary Transfer tasks do not acquire an artifact dependency contract.
func WithExportExecution(ctx context.Context, item *commonexecution.TaskExecution) context.Context {
	if item != nil && item.Metadata[models.ExportProtectionMetadataKey] != nil {
		return context.WithValue(ctx, exportExecutionKey{}, item)
	}
	return ctx
}

func captureExportTargets(ctx context.Context, store *projectionstore.Store, tenant int64, targets []dataprotection.ResourceReference) error {
	item, export := ctx.Value(exportExecutionKey{}).(*commonexecution.TaskExecution)
	if !export {
		return nil
	}
	lease, ok := commonexecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != item.ExecutionID || int64(lease.TenantID) != tenant || targets == nil {
		return dataprotection.ErrNotReady
	}
	var receipt *models.ExportProtectionReceipt
	version, err := store.CaptureVersion(ctx, tenant, func(reader projectionstore.GateReader) error {
		var err error
		receipt, err = exportReceiptAt(reader, tenant, targets, time.Now().UTC())
		return err
	})
	if err != nil {
		return err
	}
	return store.CommitVersion(ctx, tenant, version, func(tx *gorm.DB, reader projectionstore.GateReader) error {
		if !exportReceiptCurrent(reader, tenant, receipt, time.Now().UTC()) {
			return projectionstore.ErrVersionChanged
		}
		var current commonexecution.TaskExecution
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ?", item.ExecutionID, tenant, commonexecution.ModuleTransfer).First(&current).Error; err != nil {
			return err
		}
		existing, err := models.ReadExportProtection(current.Metadata)
		if err != nil {
			return err
		}
		if existing == nil || existing.Ready || existing.Invalidated {
			return dataprotection.ErrNotReady
		}
		current.Metadata[models.ExportProtectionMetadataKey] = receipt
		return commonexecution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{"metadata": current.Metadata})
	})
}

func exportReceiptAt(reader projectionstore.GateReader, tenant int64, targets []dataprotection.ResourceReference, now time.Time) (*models.ExportProtectionReceipt, error) {
	receipt := &models.ExportProtectionReceipt{Ready: true, Targets: targets, Versions: []models.ExportProtectionVersion{}}
	for _, target := range targets {
		gate := reader.Gate(tenant, target, now)
		if gate.Err != nil {
			return nil, gate.Err
		}
		if gate.Managed && gate.State != dataprotection.ProjectionStateActive {
			return nil, dataprotection.ErrNotReady
		}
		for _, p := range gate.Projections {
			receipt.Versions = append(receipt.Versions, models.ExportProtectionVersion{ProjectionID: p.ProjectionID, Revision: p.Revision, Checksum: p.Checksum})
			if p.ExpiresAt != nil && (receipt.ValidUntil == nil || p.ExpiresAt.Before(*receipt.ValidUntil)) {
				until := *p.ExpiresAt
				receipt.ValidUntil = &until
			}
		}
	}
	sort.Slice(receipt.Versions, func(i, j int) bool { return receipt.Versions[i].ProjectionID < receipt.Versions[j].ProjectionID })
	return receipt, nil
}

func exportReceiptCurrent(reader projectionstore.GateReader, tenant int64, receipt *models.ExportProtectionReceipt, now time.Time) bool {
	if receipt == nil || !receipt.Ready || receipt.Invalidated || receipt.Targets == nil || (receipt.ValidUntil != nil && !now.Before(*receipt.ValidUntil)) {
		return false
	}
	current, err := exportReceiptAt(reader, tenant, receipt.Targets, now)
	if err != nil || len(current.Versions) != len(receipt.Versions) {
		return false
	}
	for i := range current.Versions {
		if current.Versions[i] != receipt.Versions[i] {
			return false
		}
	}
	return true
}

// ExportArtifactCurrent keeps receipts private and observes both the execution
// and the current protection facts under the same checkpoint as installation.
func (g *Gate) ExportArtifactCurrent(ctx context.Context, tenant int64, executionID string) (bool, error) {
	if g == nil || g.store == nil {
		return false, dataprotection.ErrNotReady
	}
	version, err := g.store.CaptureVersion(ctx, tenant, func(projectionstore.GateReader) error { return nil })
	if err != nil {
		return false, err
	}
	valid := false
	err = g.store.CommitVersion(ctx, tenant, version, func(tx *gorm.DB, reader projectionstore.GateReader) error {
		var item commonexecution.TaskExecution
		if err := tx.WithContext(ctx).Where("execution_id = ? AND tenant_id = ? AND module = ?", executionID, tenant, commonexecution.ModuleTransfer).First(&item).Error; err != nil {
			return err
		}
		receipt, err := models.ReadExportProtection(item.Metadata)
		if err != nil {
			return err
		}
		valid = exportReceiptCurrent(reader, tenant, receipt, time.Now().UTC())
		return nil
	})
	if errors.Is(err, projectionstore.ErrVersionChanged) {
		return false, nil
	}
	return valid, err
}

// ExportArtifactBarrier records irreversible invalidation in the same local
// transaction as the related upsert/release. Reverting rules never resurrects
// an old file, including an unmanaged -> managed -> released cycle.
type ExportArtifactBarrier struct{}

func (ExportArtifactBarrier) ApplyProjectionChanges(ctx context.Context, tx *gorm.DB, tenant int64, changes []dataprotection.ProjectionChange, _ time.Time) error {
	changed := map[dataprotection.ResourceReference][]dataprotection.ProjectionChange{}
	for _, change := range changes {
		var target dataprotection.ResourceReference
		if change.Projection != nil {
			target = change.Projection.Target
		} else if change.Release != nil {
			target = change.Release.Target
		}
		target.ComponentKey = ""
		changed[target] = append(changed[target], change)
	}
	var afterID int64
	for {
		var items []commonexecution.TaskExecution
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND module = ? AND metadata -> ? IS NOT NULL", tenant, commonexecution.ModuleTransfer, models.ExportProtectionMetadataKey).Where("id > ? AND status IN ?", afterID, []string{commonexecution.ExecutionStatusRunning, commonexecution.ExecutionStatusSuccess}).Order("id ASC").Limit(100).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		afterID = items[len(items)-1].ID
		for _, item := range items {
			receipt, err := models.ReadExportProtection(item.Metadata)
			if err != nil {
				return err
			}
			if receipt == nil || !receipt.Ready || receipt.Invalidated {
				continue
			}
			for _, target := range receipt.Targets {
				affected := false
				for _, change := range changed[target] {
					same := false
					if change.Projection != nil {
						p := change.Projection
						for _, version := range receipt.Versions {
							same = same || version == (models.ExportProtectionVersion{ProjectionID: p.ProjectionID, Revision: p.Revision, Checksum: p.Checksum})
						}
					}
					affected = affected || !same
				}
				if !affected {
					continue
				}
				receipt.Invalidated = true
				item.Metadata[models.ExportProtectionMetadataKey] = receipt
				if err := tx.Model(&commonexecution.TaskExecution{}).Where("id = ? AND tenant_id = ?", item.ID, tenant).Update("metadata", item.Metadata).Error; err != nil {
					return err
				}
				break
			}
		}
	}
}
