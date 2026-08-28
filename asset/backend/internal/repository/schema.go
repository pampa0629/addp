package repository

import (
	"fmt"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

const assetSchemaLockID int64 = 2026082701

func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("asset schema database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS asset").Error; err != nil {
				return fmt.Errorf("create asset schema: %w", err)
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", assetSchemaLockID).Error; err != nil {
				return fmt.Errorf("acquire asset schema lock: %w", err)
			}
			if err := renameAssetCategoryStorage(tx); err != nil {
				return err
			}
		}
		if err := tx.AutoMigrate(
			&models.TypeDefinition{}, &models.TypeFieldSchema{}, &models.AssetCategory{}, &models.Asset{},
			&models.AssetComponent{}, &models.AssetExtField{}, &models.Application{},
			&models.Authorization{}, &models.Rating{},
		); err != nil {
			return fmt.Errorf("auto migrate asset schema: %w", err)
		}
		if tx.Dialector.Name() != "postgres" {
			return nil
		}
		statements := []string{
			`DO $migration$
			 BEGIN
			   IF to_regclass('asset.catalogs_id_seq') IS NOT NULL THEN
			     IF to_regclass('asset.categories_id_seq') IS NOT NULL THEN
			       RAISE EXCEPTION 'asset schema contains both catalogs_id_seq and categories_id_seq';
			     END IF;
			     ALTER SEQUENCE asset.catalogs_id_seq RENAME TO categories_id_seq;
			   END IF;
			   IF EXISTS (
			     SELECT 1 FROM pg_constraint
			     WHERE connamespace = 'asset'::regnamespace
			       AND conrelid = 'asset.categories'::regclass
			       AND conname = 'catalogs_pkey'
			   ) THEN
			     IF EXISTS (
			       SELECT 1 FROM pg_constraint
			       WHERE connamespace = 'asset'::regnamespace
			         AND conrelid = 'asset.categories'::regclass
			         AND conname = 'categories_pkey'
			     ) THEN
			       RAISE EXCEPTION 'asset categories contains both catalogs_pkey and categories_pkey';
			     END IF;
			     ALTER TABLE asset.categories RENAME CONSTRAINT catalogs_pkey TO categories_pkey;
			   END IF;
			 END
			 $migration$`,
			`DROP INDEX IF EXISTS asset.idx_asset_catalogs_parent_id`,
			`DROP INDEX IF EXISTS asset.idx_asset_catalogs_tenant_id`,
			`DROP INDEX IF EXISTS asset.idx_asset_assets_catalog_id`,
			`UPDATE asset.assets SET status = 'offline' WHERE status = 'published' AND NOT EXISTS (
			 SELECT 1 FROM asset.asset_components component WHERE component.asset_id = asset.assets.id
			)`,
			`DROP INDEX IF EXISTS asset.uidx_asset_fingerprint_tenant`,
			`ALTER TABLE asset.assets DROP COLUMN IF EXISTS source_module`,
			`ALTER TABLE asset.assets DROP COLUMN IF EXISTS source_reference`,
			`ALTER TABLE asset.assets DROP COLUMN IF EXISTS fingerprint`,
			`ALTER TABLE asset.assets DROP COLUMN IF EXISTS source_available`,
			`ALTER TABLE asset.type_definitions DROP COLUMN IF EXISTS source_module`,
			`ALTER TABLE asset.type_definitions DROP COLUMN IF EXISTS discovery_path`,
			`ALTER TABLE asset.type_definitions DROP COLUMN IF EXISTS auth_handler`,
			`ALTER TABLE asset.type_definitions DROP COLUMN IF EXISTS entry_type`,
			`CREATE TABLE IF NOT EXISTS asset.data_migrations (
			 version BIGINT PRIMARY KEY,
			 name TEXT NOT NULL,
			 applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`DO $migration$
			 BEGIN
			   IF NOT EXISTS (SELECT 1 FROM asset.data_migrations WHERE version = 2026082701) THEN
			     DELETE FROM asset.authorizations;
			     INSERT INTO asset.data_migrations (version, name)
			     VALUES (2026082701, 'remove_soft_authorizations_for_owner_grants');
			   END IF;
			 END
			 $migration$`,
			`ALTER TABLE asset.authorizations DROP COLUMN IF EXISTS credential`,
			`ALTER TABLE asset.authorizations DROP COLUMN IF EXISTS is_active`,
			`ALTER TABLE asset.authorizations ALTER COLUMN application_id SET NOT NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_asset_authorization_application
			 ON asset.authorizations (application_id)`,
			`ALTER TABLE asset.authorizations DROP CONSTRAINT IF EXISTS ck_asset_authorization_status`,
			`ALTER TABLE asset.authorizations ADD CONSTRAINT ck_asset_authorization_status
			 CHECK (status IN ('pending', 'effective', 'revocation_pending', 'revoked'))`,
			`ALTER TABLE asset.authorizations DROP CONSTRAINT IF EXISTS ck_asset_authorization_target`,
			`ALTER TABLE asset.authorizations ADD CONSTRAINT ck_asset_authorization_target CHECK (
			   (target_module = '' AND target_resource_type = '' AND target_resource_id = '') OR
			   (target_module = 'workbench' AND target_resource_type = 'data_application'
			    AND target_resource_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
			 )`,
			`ALTER TABLE asset.authorizations DROP CONSTRAINT IF EXISTS ck_asset_authorization_lifecycle`,
			`ALTER TABLE asset.authorizations ADD CONSTRAINT ck_asset_authorization_lifecycle CHECK (
			   (status = 'pending' AND revoked_at IS NULL) OR
			   (status = 'effective' AND target_resource_id <> '' AND fulfilled_at IS NOT NULL AND revoked_at IS NULL) OR
			   (status = 'revocation_pending' AND revoked_at IS NULL) OR
			   (status = 'revoked' AND target_resource_id <> '' AND revoked_at IS NOT NULL)
			 )`,
			`DROP INDEX IF EXISTS asset.idx_catalogs_unique_root_name`,
			`DROP INDEX IF EXISTS asset.idx_catalogs_unique_child_name`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_categories_unique_root_name
			 ON asset.categories (tenant_id, name) WHERE parent_id IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_categories_unique_child_name
			 ON asset.categories (tenant_id, parent_id, name) WHERE parent_id IS NOT NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_asset_component_entry
			 ON asset.asset_components (tenant_id, asset_id, catalog_entry_id)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_asset_component_primary
			 ON asset.asset_components (tenant_id, asset_id) WHERE role = 'primary'`,
			`ALTER TABLE asset.asset_components DROP CONSTRAINT IF EXISTS ck_asset_component_role`,
			`ALTER TABLE asset.asset_components ADD CONSTRAINT ck_asset_component_role CHECK (role IN ('primary', 'supporting'))`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate asset schema: %w", err)
			}
		}
		return nil
	})
}

func renameAssetCategoryStorage(tx *gorm.DB) error {
	var catalogsExists, categoriesExists bool
	if err := tx.Raw(`SELECT to_regclass('asset.catalogs') IS NOT NULL`).Scan(&catalogsExists).Error; err != nil {
		return fmt.Errorf("inspect legacy asset catalogs table: %w", err)
	}
	if err := tx.Raw(`SELECT to_regclass('asset.categories') IS NOT NULL`).Scan(&categoriesExists).Error; err != nil {
		return fmt.Errorf("inspect asset categories table: %w", err)
	}
	if catalogsExists && categoriesExists {
		return fmt.Errorf("asset schema contains both catalogs and categories tables")
	}
	if catalogsExists {
		if err := tx.Exec(`ALTER TABLE asset.catalogs RENAME TO categories`).Error; err != nil {
			return fmt.Errorf("rename asset catalogs table: %w", err)
		}
	}

	var catalogColumnExists, categoryColumnExists bool
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'asset' AND table_name = 'assets' AND column_name = 'catalog_id'
	)`).Scan(&catalogColumnExists).Error; err != nil {
		return fmt.Errorf("inspect legacy asset catalog_id column: %w", err)
	}
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'asset' AND table_name = 'assets' AND column_name = 'category_id'
	)`).Scan(&categoryColumnExists).Error; err != nil {
		return fmt.Errorf("inspect asset category_id column: %w", err)
	}
	if catalogColumnExists && categoryColumnExists {
		return fmt.Errorf("asset assets table contains both catalog_id and category_id columns")
	}
	if catalogColumnExists {
		if err := tx.Exec(`ALTER TABLE asset.assets RENAME COLUMN catalog_id TO category_id`).Error; err != nil {
			return fmt.Errorf("rename asset catalog_id column: %w", err)
		}
	}
	return nil
}
