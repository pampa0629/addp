package repository

import (
	"fmt"
	"time"

	commonRepo "github.com/addp/common/repository"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	// Use common repository InitDatabase
	// Note: Manager needs access to manager, meta, and system schemas
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
	// 添加 Embedding 和 EmbeddingTask 模型（向量化功能）
	// 添加 QuickView 模型（MVT预缓存）
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.SearchHistory{},
		&models.EmbeddingTask{}, // 向量化任务定义表
		&models.MvtTask{},       // MVT 瓦片生成任务定义表
		&models.QuickView{},     // MVT预缓存状态表
	)
	if err != nil {
		return nil, err
	}

	if err := ensureEmbeddingArtifactStateSchema(db, cfg.VectorConfig.Dimension); err != nil {
		return nil, fmt.Errorf("failed to ensure embedding artifact state schema: %w", err)
	}
	if err := ensureEmbeddingTaskDefinitionSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure embedding task definition schema: %w", err)
	}

	// Configure connection pool for optimal performance
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Connection pool settings
	sqlDB.SetMaxOpenConns(100)                 // 最大打开连接数 (支持高并发MVT请求)
	sqlDB.SetMaxIdleConns(20)                  // 最大空闲连接数 (连接复用)
	sqlDB.SetConnMaxLifetime(10 * time.Minute) // 连接最大生命周期

	// Set search_path to manager schema only
	// Access to metadata and system schemas should be done via MetaClient and SystemClient
	db.Exec(fmt.Sprintf("SET search_path TO %s", cfg.DBSchema))

	return db, nil
}

func ensureEmbeddingArtifactStateSchema(db *gorm.DB, vectorDimension int) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector SCHEMA manager`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP TABLE IF EXISTS manager.document_embeddings`).Error; err != nil {
		return err
	}
	if vectorDimension <= 0 {
		vectorDimension = 2560
	}

	var tableExists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'manager'
			  AND table_name = 'embeddings'
			  AND table_type = 'BASE TABLE'
		)
	`).Scan(&tableExists).Error; err != nil {
		return err
	}
	if !tableExists {
		return db.AutoMigrate(&models.Embedding{})
	}

	var legacyCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'embeddings'
		  AND column_name IN ('fingerprint', 'modality', 'bucket', 'path', 'name')
	`).Scan(&legacyCount).Error; err != nil {
		return err
	}

	var hasItemFingerprint bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'manager'
			  AND table_name = 'embeddings'
			  AND column_name = 'item_fingerprint'
		)
	`).Scan(&hasItemFingerprint).Error; err != nil {
		return err
	}

	var embeddingTypmod int
	if err := db.Raw(`
		SELECT COALESCE(a.atttypmod, -1)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'manager'
		  AND c.relname = 'embeddings'
		  AND a.attname = 'embedding'
		  AND a.attnum > 0
		  AND NOT a.attisdropped
	`).Scan(&embeddingTypmod).Error; err != nil {
		return err
	}
	hasExpectedVectorDimension := embeddingTypmod == vectorDimension

	if legacyCount > 0 || !hasItemFingerprint || !hasExpectedVectorDimension {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.embeddings`).Error; err != nil {
			return err
		}
		if err := db.AutoMigrate(&models.Embedding{}); err != nil {
			return err
		}
	}
	return db.AutoMigrate(&models.Embedding{})
}

func ensureEmbeddingTaskDefinitionSchema(db *gorm.DB) error {
	var legacyCount int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'embedding_tasks'
		  AND column_name IN ('engine_id', 'bucket', 'prefix', 'recursive', 'modality', 'file_types')
	`).Scan(&legacyCount).Error; err != nil {
		return err
	}

	var hasConfig bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'manager'
			  AND table_name = 'embedding_tasks'
			  AND column_name = 'config'
		)
	`).Scan(&hasConfig).Error; err != nil {
		return err
	}

	if legacyCount > 0 || !hasConfig {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.embedding_tasks`).Error; err != nil {
			return err
		}
		if err := db.AutoMigrate(&models.EmbeddingTask{}); err != nil {
			return err
		}
	}
	if legacyCount == 0 && hasConfig {
		if err := db.AutoMigrate(&models.EmbeddingTask{}); err != nil {
			return err
		}
	}
	return nil
}
