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

	// Initialize database with auto-migration.
	// Quick View / tile cache tables use an explicit clean-break schema guard below.
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.SearchHistory{},
		&models.EmbeddingTask{}, // 向量化任务定义表
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
	if err := ensureTileCacheStateSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure tile cache state schema: %w", err)
	}
	if err := ensureQuickViewOptimizationSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure quick view optimization schema: %w", err)
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

func ensureTileCacheStateSchema(db *gorm.DB) error {
	legacyTileTaskTable := "mvt" + "_tasks"
	if err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS manager.%q`, legacyTileTaskTable)).Error; err != nil {
		return err
	}

	var legacyQuickViewColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'quick_view'
		  AND column_name IN (
		    'engine_id', 'schema_name', 'table_name', 'min_zoom', 'max_zoom',
		    'actual_max_zoom', 'total_tiles', 'cached_tiles', 'fingerprint',
		    'extent', 'extent_srid', 'optimization_config', 'preparation_status',
		    'started_at', 'completed_at', 'item_id'
		  )
	`).Scan(&legacyQuickViewColumns).Error; err != nil {
		return err
	}

	var quickViewDerivedColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'quick_view'
		  AND column_name IN (
		    'can_use_quick_view', 'can_generate_tile_cache', 'default_artifact_id',
		    'status', 'unavailable_reason', 'last_checked_at'
		  )
	`).Scan(&quickViewDerivedColumns).Error; err != nil {
		return err
	}

	var quickViewRequiredColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'quick_view'
		  AND column_name IN ('item_fingerprint', 'preferred_mode')
	`).Scan(&quickViewRequiredColumns).Error; err != nil {
		return err
	}

	if legacyQuickViewColumns > 0 || quickViewDerivedColumns > 0 || quickViewRequiredColumns < 2 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.quick_view`).Error; err != nil {
			return err
		}
	}

	var tileTaskLegacyColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'tile_cache_tasks'
		  AND column_name IN ('engine_id', 'schema_name', 'table_name', 'min_zoom', 'max_zoom', 'optimization_config')
	`).Scan(&tileTaskLegacyColumns).Error; err != nil {
		return err
	}

	var hasTileTaskConfig bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'manager'
			  AND table_name = 'tile_cache_tasks'
			  AND column_name = 'config'
		)
	`).Scan(&hasTileTaskConfig).Error; err != nil {
		return err
	}

	if tileTaskLegacyColumns > 0 || !hasTileTaskConfig {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.tile_cache_tasks`).Error; err != nil {
			return err
		}
	}

	var hasTileCacheItemFingerprint bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'manager'
			  AND table_name = 'tile_cache'
			  AND column_name = 'item_fingerprint'
		)
	`).Scan(&hasTileCacheItemFingerprint).Error; err != nil {
		return err
	}
	if !hasTileCacheItemFingerprint {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.tile_cache`).Error; err != nil {
			return err
		}
	}

	var tileCacheSignatureColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'tile_cache'
		  AND column_name IN ('source_version', 'source_signature')
	`).Scan(&tileCacheSignatureColumns).Error; err != nil {
		return err
	}
	if tileCacheSignatureColumns > 0 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.tile_cache`).Error; err != nil {
			return err
		}
	}

	var tileCacheConfigHashColumn int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'tile_cache'
		  AND column_name = 'config_hash'
	`).Scan(&tileCacheConfigHashColumn).Error; err != nil {
		return err
	}
	if tileCacheConfigHashColumn > 0 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.tile_cache`).Error; err != nil {
			return err
		}
	}

	if err := db.Exec(`DROP TABLE IF EXISTS manager.tile_cache_artifacts`).Error; err != nil {
		return err
	}

	if err := db.AutoMigrate(&models.TileCacheTask{}, &models.TileCache{}, &models.QuickView{}); err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM manager.quick_view
		WHERE COALESCE(item_fingerprint, '') = ''
		   OR item_fingerprint LIKE 'locator:%'
		   OR COALESCE(locator, '') = ''
		   OR locator NOT LIKE 'addp://engine/%/path/%?%item_id=%'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM manager.tile_cache
		WHERE COALESCE(item_fingerprint, '') = ''
		   OR item_fingerprint LIKE 'locator:%'
		   OR COALESCE(locator, '') = ''
		   OR locator NOT LIKE 'addp://engine/%/path/%?%item_id=%'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`ALTER TABLE manager.tile_cache_tasks ALTER COLUMN enabled DROP DEFAULT`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_tile_cache_tenant_fingerprint_config_unique`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_tile_cache_tenant_fingerprint_format_unique
		ON manager.tile_cache (tenant_id, item_fingerprint, tile_format)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureQuickViewOptimizationSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.QuickViewOptimizationTask{}, &models.QuickViewOptimization{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.quick_view_optimization_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.quick_view_optimization_tasks
		SET created_at = NOW()
		WHERE created_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.quick_view_optimization_tasks
		SET updated_at = NOW()
		WHERE updated_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.quick_view_optimization
		SET created_at = NOW()
		WHERE created_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.quick_view_optimization
		SET updated_at = NOW()
		WHERE updated_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.quick_view_optimization_tasks
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN enabled SET NOT NULL,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.quick_view_optimization
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN item_id TYPE BIGINT,
			ALTER COLUMN task_id TYPE BIGINT,
			ALTER COLUMN source_engine_id TYPE BIGINT,
			ALTER COLUMN source_srid TYPE BIGINT,
			ALTER COLUMN target_srid TYPE BIGINT,
			ALTER COLUMN render_extent_srid TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_manager_quick_view_optimization_tasks_deleted_at`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_manager_quick_view_optimization_deleted_at`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_qvo_current_target_unique
		ON manager.quick_view_optimization (tenant_id, item_fingerprint, source_geometry_column, target_srid)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		return err
	}
	return nil
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
	if err := db.Exec(`ALTER TABLE manager.embedding_tasks ALTER COLUMN enabled DROP DEFAULT`).Error; err != nil {
		return err
	}
	return nil
}
