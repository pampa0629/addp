package repository

import (
	"fmt"
	"log"
	"time"

	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	commonRepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	// Use common repository InitDatabase
	// Note: Manager needs access to manager, metadata, and system schemas
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
		&models.Embedding{},     // 向量嵌入表
		&models.EmbeddingTask{}, // 向量化任务表
		&models.QuickView{},     // MVT预缓存任务表
	)
	if err != nil {
		return nil, err
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
