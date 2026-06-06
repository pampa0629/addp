package repository

import (
	"errors"

	"github.com/addp/common/logger"
	"gorm.io/gorm"
)

// applyDatabaseConstraints 应用数据库约束（GORM AutoMigrate 无法创建的约束）
// 这些约束确保数据完整性和租户隔离
func applyDatabaseConstraints(db *gorm.DB) error {
	// 1. 添加唯一性约束（防止重复节点）
	// 已在 008_add_unique_constraint_meta_node.sql 中定义
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_with_parent
		ON meta.meta_node (tenant_id, engine_id, parent_node_id, name, node_type)
		WHERE parent_node_id IS NOT NULL AND deleted_at IS NULL;
	`).Error; err != nil {
		logger.L().Warn("创建唯一索引 idx_meta_node_unique_with_parent 失败（可能已存在）", "error", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_without_parent
		ON meta.meta_node (tenant_id, engine_id, name, node_type)
		WHERE parent_node_id IS NULL AND deleted_at IS NULL;
	`).Error; err != nil {
		logger.L().Warn("创建唯一索引 idx_meta_node_unique_without_parent 失败（可能已存在）", "error", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_full_name
		ON meta.meta_node (tenant_id, engine_id, node_type, full_name)
		WHERE deleted_at IS NULL AND full_name <> '';
	`).Error; err != nil {
		logger.L().Warn("创建唯一索引 idx_meta_node_unique_full_name 失败（可能已存在）", "error", err)
	}

	// 2. 添加租户-引擎外键约束（确保元数据 tenant_id 与引擎一致）
	// 已在 009_add_tenant_engine_fk.sql 中定义

	// 2.1 在 system.engines 表上创建复合唯一索引
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_engines_tenant_engine_unique
		ON system.engines (tenant_id, id);
	`).Error; err != nil {
		logger.L().Warn("创建唯一索引 idx_engines_tenant_engine_unique 失败（可能已存在）", "error", err)
	}

	// 2.2 为 meta_node 添加外键约束
	if err := db.Exec(`
		ALTER TABLE meta.meta_node
		ADD CONSTRAINT fk_meta_node_tenant_engine
		FOREIGN KEY (tenant_id, engine_id)
		REFERENCES system.engines (tenant_id, id)
		ON DELETE CASCADE;
	`).Error; err != nil {
		// 忽略 "already exists" 错误
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			logger.L().Warn("添加外键约束 fk_meta_node_tenant_engine 失败（可能已存在）", "error", err)
		}
	}

	// 2.3 为 meta_item 添加外键约束
	if err := db.Exec(`
		ALTER TABLE meta.meta_item
		ADD CONSTRAINT fk_meta_item_tenant_engine
		FOREIGN KEY (tenant_id, engine_id)
		REFERENCES system.engines (tenant_id, id)
		ON DELETE CASCADE;
	`).Error; err != nil {
		// 忽略 "already exists" 错误
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			logger.L().Warn("添加外键约束 fk_meta_item_tenant_engine 失败（可能已存在）", "error", err)
		}
	}

	logger.L().Info("数据库约束应用完成")
	return nil
}
