package repository

import (
	"fmt"
	"log"
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

	if err := ensureEmbeddingArtifactStateSchema(db); err != nil {
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

	// 创建向量索引（IVFFlat 索引需要表中有数据才能创建，启动时可能失败）
	if err := createVectorIndexes(db); err != nil {
		log.Printf("⚠️  Warning: Failed to create vector indexes (will retry later): %v", err)
		// 不返回错误，因为空表无法创建 IVFFlat 索引
	}

	return db, nil
}

func ensureEmbeddingArtifactStateSchema(db *gorm.DB) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP TABLE IF EXISTS manager.document_embeddings`).Error; err != nil {
		return err
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

	if legacyCount > 0 || !hasItemFingerprint {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.embeddings`).Error; err != nil {
			return err
		}
		if err := db.AutoMigrate(&models.Embedding{}); err != nil {
			return err
		}
	}
	if legacyCount == 0 && hasItemFingerprint {
		if err := db.AutoMigrate(&models.Embedding{}); err != nil {
			return err
		}
	}
	return nil
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

// createVectorIndexes 创建向量索引
// IVFFlat 索引是 pgvector 提供的近似最近邻（ANN）索引，用于加速向量相似度搜索
// 注意：该索引需要表中有一定数据量才能创建（官方建议至少 10,000 行）
func createVectorIndexes(db *gorm.DB) error {
	// 检查索引是否已存在
	var exists bool
	db.Raw("SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = 'manager' AND indexname = 'idx_embeddings_vector')").Scan(&exists)

	if exists {
		log.Println("✅ Vector index already exists")
		return nil
	}

	// 尝试创建 IVFFlat 向量索引
	// USING ivfflat: 使用 IVFFlat 算法（Inverted File with Flat compression）
	// vector_cosine_ops: 使用余弦距离度量（适合文本和图像嵌入）
	// lists = 100: 聚类数量，官方建议 rows/1000（100 适合 10 万行数据）
	err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_embeddings_vector
		ON manager.embeddings
		USING ivfflat (embedding vector_cosine_ops)
		WITH (lists = 100)
	`).Error

	if err != nil {
		// 启动时表为空，创建失败是正常的
		return fmt.Errorf("IVFFlat index creation failed (table may be empty): %w", err)
	}

	log.Println("✅ Vector index created successfully")
	return nil
}
