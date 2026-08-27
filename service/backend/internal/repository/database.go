package repository

import (
	"fmt"
	"log"

	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 创建 schema
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	// 自动迁移表结构
	if err := AutoMigrate(db); err != nil {
		return nil, err
	}

	log.Printf("✅ Database initialized successfully (schema: %s)", cfg.DBSchema)
	return db, nil
}

// AutoMigrate 自动迁移所有表结构
func AutoMigrate(db *gorm.DB) error {
	migrateModels := []interface{}{
		// Phase 2 新模型
		&models.QueryService{},
		&models.RegisteredService{},
		&models.RegisteredServiceLayer{},
		// 图查询服务
		&models.GraphQueryService{},
		// 瓦片服务
		&models.TileService{},
		&models.TileServiceLayer{},
		&models.RuntimePolicy{},
		&models.CatalogResourceChangeRow{},
	}

	for _, model := range migrateModels {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("failed to migrate %T: %w", model, err)
		}
	}
	if err := migrateCatalogQueryServiceChanges(db); err != nil {
		return err
	}

	log.Println("✅ Database migration completed")
	return nil
}

func migrateCatalogQueryServiceChanges(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(2026082604)).Error; err != nil {
			return fmt.Errorf("acquire Service catalog schema lock: %w", err)
		}
		statements := []string{
			`CREATE TABLE IF NOT EXISTS service.data_migrations (
				version BIGINT PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`CREATE INDEX IF NOT EXISTS idx_service_catalog_changes_tenant_id
				ON service.catalog_resource_changes (tenant_id, id)`,
			`CREATE INDEX IF NOT EXISTS idx_service_catalog_changes_source
				ON service.catalog_resource_changes (tenant_id, source_type, source_identity, id DESC)`,
			`INSERT INTO service.catalog_resource_changes (
				tenant_id, source_type, source_identity, operation, snapshot, observed_at
			)
			SELECT
				query_service.tenant_id,
				'query_service',
				query_service.id,
				'upsert',
				jsonb_strip_nulls(jsonb_build_object(
					'name', query_service.title,
					'code', query_service.service_name,
					'object_kind', 'query_service',
					'service_status', query_service.status,
					'config_type', query_service.config_type,
					'access_mode', CASE WHEN query_service.public_access THEN 'public' ELSE 'private' END,
					'engine_id', CASE WHEN query_service.engine_id IS NULL THEN NULL ELSE query_service.engine_id::TEXT END,
					'runtime_engine_id', CASE WHEN query_service.runtime_engine_id IS NULL THEN NULL ELSE query_service.runtime_engine_id::TEXT END
				)),
				COALESCE(query_service.updated_at, query_service.created_at, NOW())
			FROM service.query_services AS query_service
			WHERE NOT EXISTS (
				SELECT 1 FROM service.data_migrations WHERE version = 2026082602
			)
			ORDER BY query_service.id`,
			`INSERT INTO service.data_migrations (version, name)
			VALUES (2026082602, 'catalog_query_service_change_feed_v1')
			ON CONFLICT (version) DO NOTHING`,
			`CREATE OR REPLACE FUNCTION service.capture_query_service_catalog_change()
			RETURNS TRIGGER
			LANGUAGE plpgsql
			AS $function$
			DECLARE
				changed service.query_services%ROWTYPE;
			BEGIN
				changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
				INSERT INTO service.catalog_resource_changes (
					tenant_id, source_type, source_identity, operation, snapshot, observed_at
				) VALUES (
					changed.tenant_id,
					'query_service',
					changed.id,
					CASE WHEN TG_OP = 'DELETE' THEN 'missing' ELSE 'upsert' END,
					jsonb_strip_nulls(jsonb_build_object(
						'name', changed.title,
						'code', changed.service_name,
						'object_kind', 'query_service',
						'service_status', changed.status,
						'config_type', changed.config_type,
						'access_mode', CASE WHEN changed.public_access THEN 'public' ELSE 'private' END,
						'engine_id', CASE WHEN changed.engine_id IS NULL THEN NULL ELSE changed.engine_id::TEXT END,
						'runtime_engine_id', CASE WHEN changed.runtime_engine_id IS NULL THEN NULL ELSE changed.runtime_engine_id::TEXT END
					)),
					NOW()
				);
				RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
			END;
			$function$`,
			`DROP TRIGGER IF EXISTS trg_service_query_service_catalog_change ON service.query_services`,
			`CREATE TRIGGER trg_service_query_service_catalog_change
			AFTER INSERT OR UPDATE OR DELETE ON service.query_services
			FOR EACH ROW EXECUTE FUNCTION service.capture_query_service_catalog_change()`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate Service catalog Query Service changes: %w", err)
			}
		}
		return nil
	})
}
