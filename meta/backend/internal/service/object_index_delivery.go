package service

import (
	"context"
	"errors"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ScanService) RunObjectIndexDelivery(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.deliverObjectIndexChanges(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("对象索引投影仍有待确认操作", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ScanService) deliverObjectIndexChanges(ctx context.Context) error {
	db := s.db.WithContext(ctx)
	client := s.indexerService.contentIndex
	if client == nil {
		return nil
	}
	var operations []string
	if err := db.Model(&models.ObjectIndexDeletion{}).Where("confirmed = ?", false).
		Distinct("operation_id").Pluck("operation_id", &operations).Error; err != nil {
		return err
	}
	var failures error
	for _, operation := range operations {
		var batch []models.ObjectIndexDeletion
		if err := db.Where("operation_id = ? AND confirmed = ?", operation, false).Order("fingerprint").Find(&batch).Error; err != nil {
			return err
		}
		if len(batch) == 0 {
			continue
		}
		first := batch[0]
		scope := commonclient.ManagerContentDeleteScope{EngineID: first.EngineID, OperationID: operation}
		for _, member := range batch {
			scope.DocumentIDs = append(scope.DocumentIDs, member.Fingerprint)
		}
		if err := client.WithTenantID(first.TenantID).DeleteDocuments(ctx, scope); err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if err := db.Model(&models.ObjectIndexDeletion{}).Where("operation_id = ?", operation).Update("confirmed", true).Error; err != nil {
			return err
		}
	}

	var itemIDs []uint
	if err := db.Model(&models.MetaItem{}).Joins("JOIN meta.object_index_deletions AS d ON d.fingerprint = meta_item.fingerprint AND d.operation_id = meta_item.index_delete_operation_id").
		Where("d.confirmed = ? AND d.restored = ?", true, false).Pluck("meta_item.id", &itemIDs).Error; err != nil {
		return errors.Join(failures, err)
	}
	for _, itemID := range itemIDs {
		if ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
		failures = errors.Join(failures, s.restoreObjectIndexDocument(ctx, itemID))
	}
	return failures
}

func (s *ScanService) restoreObjectIndexDocument(ctx context.Context, itemID uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.MetaItem
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Take(&item, itemID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var deletion models.ObjectIndexDeletion
		err = tx.Where("fingerprint = ? AND operation_id = ? AND confirmed = ? AND restored = ?", item.Fingerprint, item.IndexDeleteOperationID, true, false).
			Take(&deletion).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		resource, err := s.engineService.GetResourceByIDWithContext(ctx, item.EngineID, item.TenantID)
		if err != nil {
			return err
		}
		doc := technicalMetadataDocument(resource, nil, &item)
		if err := s.indexerService.contentIndex.WithTenantID(item.TenantID).UpsertDocuments(ctx, []commonclient.ManagerContentDocument{doc}); err != nil {
			return err
		}
		return tx.Model(&deletion).Update("restored", true).Error
	})
}

func (s *IndexerService) objectIndexWriteReady(ctx context.Context, item *models.MetaItem) bool {
	if item.IndexDeleteOperationID == "" {
		return true
	}
	var ready int64
	err := s.db.WithContext(ctx).Model(&models.MetaItem{}).
		Joins("JOIN meta.object_index_deletions AS d ON d.fingerprint = meta_item.fingerprint AND d.operation_id = meta_item.index_delete_operation_id").
		Where("meta_item.id = ? AND d.operation_id = ? AND d.confirmed = ? AND d.restored = ?", item.ID, item.IndexDeleteOperationID, true, true).
		Count(&ready).Error
	if err != nil {
		s.log.Warn("查询对象索引恢复状态失败", "error", err)
	}
	return err == nil && ready == 1
}
