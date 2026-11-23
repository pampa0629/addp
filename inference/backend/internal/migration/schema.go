package migration

import (
	"context"
	_ "embed"
	"strings"
	"time"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/schema"
	"github.com/addp/inference/internal/config"
	"github.com/addp/inference/internal/models"
	"gorm.io/gorm"
)

const Version int64 = 1

//go:embed tables.mysql.sql
var mysqlTables string

func Prepare() error {
	commonconfig.LoadEnv()
	db, err := bootstrap.OpenPlatformInstallation(config.PlatformDatabase(), "inference")
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "inference", Version, func(tx *gorm.DB) error {
			for _, ddl := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
				if err := tx.Exec(ddl).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	return schema.Migrate(db, "inference", Version, func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.ProviderConnection{}, &models.ProviderTenantGrant{}, &models.ModelDeployment{}, &models.ModelProfile{}, &models.CredentialAudit{}); err != nil {
			return err
		}
		return ensureSchemaConstraints(tx)
	})
}

func ensureSchemaConstraints(db *gorm.DB) error {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_platform_profile_code ON inference.model_profiles (code) WHERE scope_type = 'platform' AND tenant_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_tenant_profile_code ON inference.model_profiles (tenant_id, code) WHERE scope_type = 'tenant' AND tenant_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_inference_profiles_resolution ON inference.model_profiles (scope_type, tenant_id, status)`,
		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'ck_inference_deployment_chat_max_output_tokens_parameter'
			) THEN
				ALTER TABLE inference.model_deployments
				ADD CONSTRAINT ck_inference_deployment_chat_max_output_tokens_parameter
				CHECK (chat_max_output_tokens_parameter IN ('max_tokens', 'max_completion_tokens'));
			END IF;
		END $$`,
		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'ck_inference_deployment_chat_temperature_mode'
			) THEN
				ALTER TABLE inference.model_deployments
				ADD CONSTRAINT ck_inference_deployment_chat_temperature_mode
				CHECK (chat_temperature_mode IN ('configurable', 'default_only'));
			END IF;
		END $$`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
