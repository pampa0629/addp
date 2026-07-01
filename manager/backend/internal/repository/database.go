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
		&models.ExportSession{},
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
	if err := dropLegacyQuickViewTables(db); err != nil {
		return nil, fmt.Errorf("failed to drop legacy quick view tables: %w", err)
	}
	if err := ensureTileCacheStateSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure tile cache state schema: %w", err)
	}
	if err := ensureQuickViewOptimizationSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure quick view optimization schema: %w", err)
	}
	if err := ensureRasterCOGSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure raster COG schema: %w", err)
	}
	if err := ensureRasterMosaicSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure raster mosaic schema: %w", err)
	}
	if err := ensureModel3DQuickViewSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure model 3d quick view schema: %w", err)
	}
	if err := ensureGaussianSplatQuickViewSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure gaussian splat quick view schema: %w", err)
	}
	if err := ensureModel3DTilesSchema(db); err != nil {
		return nil, fmt.Errorf("failed to ensure model 3d tiles schema: %w", err)
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

func dropLegacyQuickViewTables(db *gorm.DB) error {
	legacyTables := []string{
		"quick_view_optimization",
		"quick_view_optimization_tasks",
		"tile_cache",
		"tile_cache_tasks",
		"cog_artifacts",
		"cog_artifact_tasks",
		"vector_tile_cache_artifacts",
		"mvt_tasks",
	}
	for _, table := range legacyTables {
		if err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS manager.%q`, table)).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureTileCacheStateSchema(db *gorm.DB) error {
	if err := renameQuickViewTableToPreviewState(db); err != nil {
		return err
	}

	var legacyQuickViewColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'preview_state'
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
		  AND table_name = 'preview_state'
		  AND column_name IN (
		    'can_use_quick_view', 'can_generate_vector_tile_cache', 'default_artifact_id',
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
		  AND table_name = 'preview_state'
		  AND column_name IN ('item_fingerprint', 'preferred_mode')
	`).Scan(&quickViewRequiredColumns).Error; err != nil {
		return err
	}

	if legacyQuickViewColumns > 0 || quickViewDerivedColumns > 0 || quickViewRequiredColumns < 2 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.preview_state`).Error; err != nil {
			return err
		}
	}

	var tileTaskLegacyColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'vector_tile_cache_tasks'
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
			  AND table_name = 'vector_tile_cache_tasks'
			  AND column_name = 'config'
		)
	`).Scan(&hasTileTaskConfig).Error; err != nil {
		return err
	}

	if tileTaskLegacyColumns > 0 || !hasTileTaskConfig {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.vector_tile_cache_tasks`).Error; err != nil {
			return err
		}
	}

	var hasTileCacheItemFingerprint bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'manager'
			  AND table_name = 'vector_tile_cache'
			  AND column_name = 'item_fingerprint'
		)
	`).Scan(&hasTileCacheItemFingerprint).Error; err != nil {
		return err
	}
	if !hasTileCacheItemFingerprint {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.vector_tile_cache`).Error; err != nil {
			return err
		}
	}

	var tileCacheSignatureColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'vector_tile_cache'
		  AND column_name IN ('source_version', 'source_signature')
	`).Scan(&tileCacheSignatureColumns).Error; err != nil {
		return err
	}
	if tileCacheSignatureColumns > 0 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.vector_tile_cache`).Error; err != nil {
			return err
		}
	}

	var tileCacheConfigHashColumn int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'manager'
		  AND table_name = 'vector_tile_cache'
		  AND column_name = 'config_hash'
	`).Scan(&tileCacheConfigHashColumn).Error; err != nil {
		return err
	}
	if tileCacheConfigHashColumn > 0 {
		if err := db.Exec(`DROP TABLE IF EXISTS manager.vector_tile_cache`).Error; err != nil {
			return err
		}
	}

	if err := db.AutoMigrate(&models.TileCacheTask{}, &models.TileCache{}, &models.PreviewState{}); err != nil {
		return err
	}
	if err := normalizePreviewStateViewState(db); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_tile_cache_tasks
		SET config = config - 'preparation'
		WHERE config ? 'preparation'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM manager.preview_state
		WHERE COALESCE(item_fingerprint, '') = ''
		   OR item_fingerprint LIKE 'locator:%'
		   OR COALESCE(locator, '') = ''
		   OR locator NOT LIKE 'addp://engine/%/path/%?%item_id=%'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM manager.vector_tile_cache
		WHERE COALESCE(item_fingerprint, '') = ''
		   OR item_fingerprint LIKE 'locator:%'
		   OR COALESCE(locator, '') = ''
		   OR locator NOT LIKE 'addp://engine/%/path/%?%item_id=%'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`ALTER TABLE manager.vector_tile_cache_tasks ALTER COLUMN enabled DROP DEFAULT`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_vector_tile_cache_tenant_fingerprint_config_unique`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE common.task_executions
		SET metadata = jsonb_set(
			metadata,
			'{tile_generation_target}',
			((metadata->'tile_generation_target') - 'prepared_3857') || jsonb_build_object(
				'target_kind',
				CASE
					WHEN metadata->'tile_generation_target'->>'target_kind' IS NOT NULL
						THEN metadata->'tile_generation_target'->>'target_kind'
					WHEN metadata->'tile_generation_target'->>'prepared_3857' = 'true'
						AND metadata->'tile_generation_target'->>'table' LIKE 'addp_qvo_%'
						THEN 'source_schema_materialized_view'
					WHEN metadata->'tile_generation_target'->>'prepared_3857' = 'true'
						THEN 'external_3857_materialized_view'
					ELSE 'source_table'
				END
			),
			true
		)
		WHERE module = 'manager'
		  AND task_type = 'vector_tile_cache_generation'
		  AND metadata ? 'tile_generation_target'
		  AND (
		    NOT (metadata->'tile_generation_target' ? 'target_kind')
		    OR metadata->'tile_generation_target' ? 'prepared_3857'
		  )
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_vector_tile_cache_tenant_fingerprint_format_unique
		ON manager.vector_tile_cache (tenant_id, item_fingerprint, tile_format)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		return err
	}
	return nil
}

func renameQuickViewTableToPreviewState(db *gorm.DB) error {
	var legacyExists bool
	if err := db.Raw(`SELECT to_regclass('manager.quick_view') IS NOT NULL`).Scan(&legacyExists).Error; err != nil {
		return err
	}
	if !legacyExists {
		return nil
	}
	var targetExists bool
	if err := db.Raw(`SELECT to_regclass('manager.preview_state') IS NOT NULL`).Scan(&targetExists).Error; err != nil {
		return err
	}
	if !targetExists {
		if err := db.Exec(`ALTER TABLE manager.quick_view RENAME TO preview_state`).Error; err != nil {
			return err
		}
	} else if err := db.Exec(`DROP TABLE IF EXISTS manager.quick_view`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_quick_view_tenant_fingerprint`).Error; err != nil {
		return err
	}
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_preview_state_tenant_fingerprint
		ON manager.preview_state (tenant_id, item_fingerprint)
	`).Error
}

func normalizePreviewStateViewState(db *gorm.DB) error {
	if err := db.Exec(`
		WITH normalized AS (
			SELECT
				id,
				CASE
					WHEN jsonb_typeof(view_state->'basic_preview') = 'object' THEN view_state->'basic_preview'
					ELSE '{}'::jsonb
				END AS basic_state,
				CASE
					WHEN jsonb_typeof(view_state->'quick_view') = 'object' THEN view_state->'quick_view'
					ELSE '{}'::jsonb
				END AS quick_state,
				COALESCE(view_state->'map', '{}'::jsonb) AS flat_map_state,
				(
					COALESCE(view_state->'model_3d', '{}'::jsonb) ||
					COALESCE(view_state->'tiles_3d', '{}'::jsonb) ||
					COALESCE(view_state->'gaussian_splat', '{}'::jsonb)
				) AS flat_scene_3d_state
			FROM manager.preview_state
		)
		UPDATE manager.preview_state AS qv
		SET view_state = jsonb_build_object(
			'basic_preview',
			jsonb_strip_nulls(jsonb_build_object(
				'map',
				NULLIF(normalized.basic_state->'map', '{}'::jsonb),
				'scene_3d',
				NULLIF((
					COALESCE(normalized.basic_state->'model_3d', '{}'::jsonb) ||
					COALESCE(normalized.basic_state->'tiles_3d', '{}'::jsonb) ||
					COALESCE(normalized.basic_state->'gaussian_splat', '{}'::jsonb) ||
					COALESCE(normalized.basic_state->'scene_3d', '{}'::jsonb)
				), '{}'::jsonb)
			)),
			'quick_view',
			jsonb_strip_nulls(jsonb_build_object(
				'map',
				NULLIF((
					normalized.flat_map_state ||
					COALESCE(normalized.quick_state->'map', '{}'::jsonb)
				), '{}'::jsonb),
				'scene_3d',
				NULLIF((
					normalized.flat_scene_3d_state ||
					COALESCE(normalized.quick_state->'model_3d', '{}'::jsonb) ||
					COALESCE(normalized.quick_state->'tiles_3d', '{}'::jsonb) ||
					COALESCE(normalized.quick_state->'gaussian_splat', '{}'::jsonb) ||
					COALESCE(normalized.quick_state->'scene_3d', '{}'::jsonb)
				), '{}'::jsonb)
			))
		)
		FROM normalized
		WHERE qv.id = normalized.id
		  AND (
			  qv.view_state ? 'map'
			  OR qv.view_state ? 'model_3d'
			  OR qv.view_state ? 'tiles_3d'
			  OR qv.view_state ? 'gaussian_splat'
			  OR normalized.basic_state ? 'model_3d'
			  OR normalized.basic_state ? 'tiles_3d'
			  OR normalized.basic_state ? 'gaussian_splat'
			  OR normalized.quick_state ? 'model_3d'
			  OR normalized.quick_state ? 'tiles_3d'
			  OR normalized.quick_state ? 'gaussian_splat'
		  )
	`).Error; err != nil {
		return err
	}
	return db.Exec(`
		COMMENT ON COLUMN manager.preview_state.view_state IS
		'预览交互状态。顶层按显示模式分为 basic_preview 和 quick_view，模式内按渲染域保存 map 地图视口和 scene_3d 三维相机状态'
	`).Error
}

func ensureQuickViewOptimizationSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.QuickViewOptimizationTask{}, &models.QuickViewOptimization{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_quick_view_target_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_quick_view_target_tasks
		SET created_at = NOW()
		WHERE created_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_quick_view_target_tasks
		SET updated_at = NOW()
		WHERE updated_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_quick_view_targets
		SET created_at = NOW()
		WHERE created_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.vector_quick_view_targets
		SET updated_at = NOW()
		WHERE updated_at IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.vector_quick_view_target_tasks
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
		ALTER TABLE manager.vector_quick_view_targets
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
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_manager_vector_quick_view_target_tasks_deleted_at`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DROP INDEX IF EXISTS manager.idx_manager_vector_quick_view_target_generation_deleted_at`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_qvo_current_target_unique
		ON manager.vector_quick_view_targets (tenant_id, item_fingerprint, source_geometry_column, target_srid)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureRasterCOGSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.RasterCOGTask{}, &models.RasterCOG{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.raster_cog_tasks
		SET config = jsonb_set(config - 'artifact', '{result}', config->'artifact', true)
		WHERE config ? 'artifact'
		  AND NOT (config ? 'result')
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.raster_cog_tasks
		SET config = config - 'artifact'
		WHERE config ? 'artifact'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.raster_cog_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.raster_cog_tasks
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
		CREATE UNIQUE INDEX IF NOT EXISTS idx_raster_cog_current_unique
		ON manager.raster_cog (tenant_id, item_fingerprint)
		WHERE deleted_at IS NULL AND status <> 'deleted'
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.raster_cog
			ALTER COLUMN source_crs TYPE TEXT
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.raster_cog
			DROP COLUMN IF EXISTS source_locator
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureRasterMosaicSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.RasterMosaicTask{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.raster_mosaic_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.raster_mosaic_tasks
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN enabled SET NOT NULL,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureModel3DTilesSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.Model3DTilesTask{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.model_3d_tiles_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.model_3d_tiles_tasks
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN enabled SET NOT NULL,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureModel3DQuickViewSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.Model3DQuickViewTask{}, &models.Model3DQuickView{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.model_3d_quick_view_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.model_3d_quick_view_tasks
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
		ALTER TABLE manager.model_3d_quick_view
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN item_id TYPE BIGINT,
			ALTER COLUMN task_id TYPE BIGINT,
			ALTER COLUMN source_engine_id TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		WITH ranked AS (
			SELECT
				id,
				ROW_NUMBER() OVER (
					PARTITION BY tenant_id, config->'source'->>'item_fingerprint'
					ORDER BY updated_at DESC, id DESC
				) AS rn
			FROM manager.model_3d_quick_view_tasks
			WHERE deleted_at IS NULL
				AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
		)
		UPDATE manager.model_3d_quick_view_tasks AS tasks
		SET deleted_at = NOW(), updated_at = NOW(), enabled = false
		FROM ranked
		WHERE tasks.id = ranked.id AND ranked.rn > 1
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_quick_view_tasks_source_unique
		ON manager.model_3d_quick_view_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
		WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_quick_view_current_unique
		ON manager.model_3d_quick_view (tenant_id, item_fingerprint)
		WHERE deleted_at IS NULL AND status <> 'deleted'
	`).Error; err != nil {
		return err
	}
	return nil
}

func ensureGaussianSplatQuickViewSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.GaussianSplatQuickViewTask{}, &models.GaussianSplatQuickView{}); err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE manager.gaussian_splat_quick_view_tasks
		SET enabled = false
		WHERE enabled IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE manager.gaussian_splat_quick_view_tasks
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
		ALTER TABLE manager.gaussian_splat_quick_view
			ALTER COLUMN id TYPE BIGINT,
			ALTER COLUMN tenant_id TYPE BIGINT,
			ALTER COLUMN item_id TYPE BIGINT,
			ALTER COLUMN task_id TYPE BIGINT,
			ALTER COLUMN source_engine_id TYPE BIGINT,
			ALTER COLUMN created_by TYPE BIGINT,
			ALTER COLUMN created_at SET NOT NULL,
			ALTER COLUMN updated_at SET NOT NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		WITH ranked AS (
			SELECT
				id,
				ROW_NUMBER() OVER (
					PARTITION BY tenant_id, config->'source'->>'item_fingerprint'
					ORDER BY updated_at DESC, id DESC
				) AS rn
			FROM manager.gaussian_splat_quick_view_tasks
			WHERE deleted_at IS NULL
				AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
		)
		UPDATE manager.gaussian_splat_quick_view_tasks AS tasks
		SET deleted_at = NOW(), updated_at = NOW(), enabled = false
		FROM ranked
		WHERE tasks.id = ranked.id AND ranked.rn > 1
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tasks_source_unique
		ON manager.gaussian_splat_quick_view_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
		WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_current_unique
		ON manager.gaussian_splat_quick_view (tenant_id, item_fingerprint)
		WHERE deleted_at IS NULL AND status <> 'deleted'
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
