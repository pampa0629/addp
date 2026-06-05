package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/common/utils"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	if err := PrepareSchema(cfg); err != nil {
		return nil, err
	}

	// Use common repository InitDatabase
	dbConfig := commonRepo.DatabaseConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	// Initialize database with auto-migration
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.MetaNode{}, // 元数据节点（schema/prefix）
		&models.MetaItem{}, // 元数据条目（table/object）
		&models.ScanTask{}, // 扫描任务定义
	)
	if err != nil {
		return nil, err
	}

	// Apply custom settings for Meta module
	// Set connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// Apply custom logger (optional, overwrites common logger)
	dbLogger := newGormLogger(logger.With("component", "gorm"), gormLogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  gormLogger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	db.Logger = dbLogger

	if err := applySQLMigrations(db); err != nil {
		return nil, err
	}

	// 运行数据库约束迁移（用于新部署）
	// 注意：这些约束 GORM AutoMigrate 无法创建，需要手动执行 SQL
	if err := applyDatabaseConstraints(db); err != nil {
		logger.L().Warn("数据库约束应用失败（可能已存在）", "error", err)
		// 不返回错误，因为约束可能已存在
	}

	DB = db
	logger.L().Info("数据库连接成功", "host", cfg.DBHost, "schema", cfg.DBSchema)
	return db, nil
}

func PrepareSchema(cfg *config.Config) error {
	if cfg.DBSchema != "meta" {
		return fmt.Errorf("meta module schema must be 'meta', got %q", cfg.DBSchema)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, utils.GetTimezone(),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to connect database for meta schema preparation: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get schema preparation database instance: %w", err)
	}
	defer sqlDB.Close()

	metadataExists, err := schemaExists(db, "metadata")
	if err != nil {
		return err
	}
	metaExists, err := schemaExists(db, "meta")
	if err != nil {
		return err
	}

	if metadataExists && metaExists {
		return fmt.Errorf("both 'metadata' and 'meta' schemas exist; please resolve before starting meta service")
	}
	if metadataExists {
		if err := db.Exec(`ALTER SCHEMA metadata RENAME TO meta`).Error; err != nil {
			return fmt.Errorf("failed to rename schema metadata to meta: %w", err)
		}
		logger.L().Info("Meta schema renamed", "from", "metadata", "to", "meta")
	}
	return nil
}

func schemaExists(db *gorm.DB, schemaName string) (bool, error) {
	var exists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.schemata
			WHERE schema_name = ?
		)
	`, schemaName).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("failed to check schema %s: %w", schemaName, err)
	}
	return exists, nil
}

type gormSlogLogger struct {
	logger             *slog.Logger
	logLevel           gormLogger.LogLevel
	slowThreshold      time.Duration
	skipRecordNotFound bool
}

func newGormLogger(log *slog.Logger, cfg gormLogger.Config) gormLogger.Interface {
	if log == nil {
		log = logger.L()
	}
	return &gormSlogLogger{
		logger:             log,
		logLevel:           cfg.LogLevel,
		slowThreshold:      cfg.SlowThreshold,
		skipRecordNotFound: cfg.IgnoreRecordNotFoundError,
	}
}

func (l *gormSlogLogger) LogMode(level gormLogger.LogLevel) gormLogger.Interface {
	clone := *l
	clone.logLevel = level
	return &clone
}

func (l *gormSlogLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel < gormLogger.Info {
		return
	}
	l.logger.Info(msg, "data", data)
}

func (l *gormSlogLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel < gormLogger.Warn {
		return
	}
	l.logger.Warn(msg, "data", data)
}

func (l *gormSlogLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel < gormLogger.Error {
		return
	}
	l.logger.Error(msg, "data", data)
}

func (l *gormSlogLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.logLevel == gormLogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []any{
		"sql", sql,
		"rows", rows,
		"elapsed_ms", elapsed.Milliseconds(),
	}

	if err != nil && l.logLevel >= gormLogger.Error {
		if errors.Is(err, gorm.ErrRecordNotFound) && l.skipRecordNotFound {
			return
		}
		l.logger.Error("数据库执行失败", append(fields, "error", err)...)
		return
	}

	if l.slowThreshold > 0 && elapsed > l.slowThreshold && l.logLevel >= gormLogger.Warn {
		l.logger.Warn("数据库执行耗时较长", append(fields, "slow_threshold_ms", l.slowThreshold.Milliseconds())...)
		return
	}

	if l.logLevel >= gormLogger.Info {
		l.logger.Debug("数据库执行完成", fields...)
	}
}

// Note: autoMigrate function removed - now handled by commonRepo.InitDatabase

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
