package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MaterializationGroupRepository struct{ db *gorm.DB }

func NewMaterializationGroupRepository(db *gorm.DB) *MaterializationGroupRepository {
	return &MaterializationGroupRepository{db: db}
}

func (r *MaterializationGroupRepository) Create(
	ctx context.Context,
	group *models.MaterializationGroup,
	logicalTableVersions map[int64]int64,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMaterializationGroupDefinitions(tx, group.TenantID, logicalTableVersions); err != nil {
			return err
		}
		members := group.Members
		group.Members = nil
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		for i := range members {
			members[i].GroupID = group.ID
			members[i].TenantID = group.TenantID
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		group.Members = members
		return nil
	})
}

func (r *MaterializationGroupRepository) GetByID(ctx context.Context, tenantID, id int64) (*models.MaterializationGroup, error) {
	var group models.MaterializationGroup
	err := r.db.WithContext(ctx).Preload("Members", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).
		Where("tenant_id = ? AND id = ?", tenantID, id).First(&group).Error
	return &group, err
}

func (r *MaterializationGroupRepository) List(ctx context.Context, tenantID int64, page, pageSize int) ([]models.MaterializationGroup, int64, error) {
	var total int64
	base := r.db.WithContext(ctx).Model(&models.MaterializationGroup{}).Where("tenant_id = ?", tenantID)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var groups []models.MaterializationGroup
	err := base.Preload("Members", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).
		Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error
	return groups, total, err
}

func (r *MaterializationGroupRepository) ListAll(ctx context.Context, tenantID int64) ([]models.MaterializationGroup, error) {
	var groups []models.MaterializationGroup
	err := r.db.WithContext(ctx).Preload("Members", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).
		Where("tenant_id = ?", tenantID).Order("updated_at DESC, id DESC").Find(&groups).Error
	return groups, err
}

func (r *MaterializationGroupRepository) Update(
	ctx context.Context,
	group *models.MaterializationGroup,
	expectedVersion int64,
	logicalTableVersions map[int64]int64,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.MaterializationGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", group.TenantID, group.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return fmt.Errorf("%w: materialization group version changed", commonAPI.ErrConflict)
		}
		if err := lockMaterializationGroupDefinitions(tx, group.TenantID, logicalTableVersions); err != nil {
			return err
		}
		now := time.Now().UTC()
		result := tx.Model(&models.MaterializationGroup{}).
			Where("tenant_id = ? AND id = ? AND version = ?", group.TenantID, group.ID, expectedVersion).
			Updates(map[string]interface{}{
				"name": group.Name, "description": group.Description, "updated_by": group.UpdatedBy,
				"version": expectedVersion + 1, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization group version changed", commonAPI.ErrConflict)
		}
		if err := tx.Where("group_id = ? AND tenant_id = ?", group.ID, group.TenantID).
			Delete(&models.MaterializationGroupMember{}).Error; err != nil {
			return err
		}
		members := group.Members
		for i := range members {
			members[i].GroupID = group.ID
			members[i].TenantID = group.TenantID
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		group.Version = expectedVersion + 1
		group.UpdatedAt = now
		group.Members = members
		return nil
	})
}

func (r *MaterializationGroupRepository) Delete(ctx context.Context, tenantID, id, version int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group models.MaterializationGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", tenantID, id).First(&group).Error; err != nil {
			return err
		}
		if group.Version != version {
			return fmt.Errorf("%w: materialization group version changed", commonAPI.ErrConflict)
		}
		return tx.Delete(&group).Error
	})
}

func (r *MaterializationGroupRepository) ContainsLogicalTable(ctx context.Context, tenantID, logicalTableID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.MaterializationGroupMember{}).
		Where("tenant_id = ? AND logical_table_id = ?", tenantID, logicalTableID).Count(&count).Error
	return count > 0, err
}

func lockMaterializationGroupDefinitions(tx *gorm.DB, tenantID int64, expectedVersions map[int64]int64) error {
	if len(expectedVersions) == 0 {
		return fmt.Errorf("%w: materialization group has no logical tables", commonAPI.ErrConflict)
	}
	ids := make([]int64, 0, len(expectedVersions))
	for id := range expectedVersions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var tables []models.LogicalTable
	if err := tx.Select("id", "tenant_id", "status", "version").
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND id IN ?", tenantID, ids).Order("id ASC").Find(&tables).Error; err != nil {
		return err
	}
	if len(tables) != len(ids) {
		return gorm.ErrRecordNotFound
	}
	for _, table := range tables {
		if table.Status != "approved" || table.Version != expectedVersions[table.ID] {
			return fmt.Errorf("%w: materialization group logical table changed", commonAPI.ErrConflict)
		}
	}
	return nil
}

func IsMaterializationGroupNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
