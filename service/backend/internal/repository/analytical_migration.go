package repository

import (
	"fmt"
	"gorm.io/gorm"
)

// Remove the old metric SQL publication, retaining its owner reference only for
// explicit rebind. A stored SQL definition cannot be translated or republished.
func migrateAnalyticalPublications(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" || !db.Migrator().HasTable("service.query_services") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(2026091601)").Error; err != nil {
			return err
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS service.data_migrations (version BIGINT PRIMARY KEY,name TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`).Error; err != nil {
			return err
		}
		var applied int64
		if err := tx.Raw("SELECT count(*) FROM service.data_migrations WHERE version=2026091601").Scan(&applied).Error; err != nil {
			return err
		}
		if applied > 0 {
			return nil
		}
		statements := []string{
			`ALTER TABLE service.query_services DROP CONSTRAINT IF EXISTS query_services_config_type_check`,
			`ALTER TABLE service.query_services DROP CONSTRAINT IF EXISTS chk_service_query_services_config_type`,
			`ALTER TABLE service.query_services DROP CONSTRAINT IF EXISTS query_services_explicit_execution_engine_check`,
			`UPDATE service.query_services SET config_type='analytical',engine_id=NULL,runtime_engine_id=NULL,schema_name='',table_name='',sql_query='',named_parameters='[]'::jsonb,status='inactive',error_message='',
     data_config=jsonb_build_object('source_snapshot',jsonb_build_object('metric_source',data_config->'source_snapshot'->'metric_source'))
     WHERE config_type='sql' AND data_config->'source_snapshot'->'metric_source' IS NOT NULL`,
			`ALTER TABLE service.query_services ADD CONSTRAINT query_services_config_type_check CHECK (config_type IN ('table','sql','analytical'))`,
			`ALTER TABLE service.query_services ADD CONSTRAINT query_services_explicit_execution_engine_check CHECK (
    (config_type='table' AND engine_id IS NOT NULL) OR
    (config_type='sql' AND ((engine_id IS NOT NULL) <> (runtime_engine_id IS NOT NULL))) OR
    (config_type='analytical' AND engine_id IS NULL AND runtime_engine_id IS NULL AND coalesce(sql_query,'')='' AND coalesce(schema_name,'')='' AND coalesce(table_name,'')='' AND coalesce(named_parameters,'[]'::jsonb)='[]'::jsonb))`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate analytical publications: %w", err)
			}
		}
		return tx.Exec("INSERT INTO service.data_migrations (version,name) VALUES (2026091601,'analytical_publications')").Error
	})
}
