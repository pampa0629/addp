package repository

import (
	"fmt"
	"log"

	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	commonModels "github.com/addp/common/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresDB,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 设置默认 schema
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s, public", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to set search_path: %w", err)
	}

	// AutoMigrate - 确保表结构最新
	// 所有表由 GORM AutoMigrate 管理（符合统一的数据库表创建策略）
	if err := db.AutoMigrate(
		&models.DevTask{},                     // 开发项（查询、工作流、Notebook 等）
		&commonModels.TaskExecution{},          // 统一执行记录表（common.task_executions）
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	// 手动创建 GIN 索引（AutoMigrate 不支持）
	ginIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_task_executions_result_gin ON common.task_executions USING GIN(result)",
		"CREATE INDEX IF NOT EXISTS idx_task_executions_step_results_gin ON common.task_executions USING GIN(step_results)",
	}
	for _, sql := range ginIndexes {
		if err := db.Exec(sql).Error; err != nil {
			log.Printf("⚠️ Failed to create GIN index: %v", err)
		}
	}

	log.Println("✅ Database connected successfully (AutoMigrate 完成)")

	return db, nil
}
