package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
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
		&models.MetaNode{},     // 元数据节点（schema/prefix）
		&models.MetaItem{},     // 元数据条目（table/object）
		&models.ScanTask{},     // 扫描任务定义
		&models.ScanTaskRun{},  // 扫描任务执行记录
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

	DB = db
	logger.L().Info("数据库连接成功", "host", cfg.DBHost, "schema", cfg.DBSchema)
	return db, nil
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
