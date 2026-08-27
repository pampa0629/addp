package repository

import (
	"fmt"

	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

const queryServiceConsumerContractMigrationVersion int64 = 2026082701

// MigrateInvalidQueryServiceConsumerContracts applies the v1 publication
// invariant once. Invalid active REST services become explicit error resources
// before the Consumer Catalog can count or paginate them.
func (r *QueryServiceRepository) MigrateInvalidQueryServiceConsumerContracts(
	validate func(*models.QueryService) error,
) (int64, error) {
	if validate == nil {
		return 0, fmt.Errorf("query service consumer contract validator is required")
	}
	if r.db.Dialector.Name() != "postgres" {
		return 0, nil
	}

	var migrated int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", queryServiceConsumerContractMigrationVersion).Error; err != nil {
			return fmt.Errorf("acquire Query Service consumer contract migration lock: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS service.data_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`).Error; err != nil {
			return fmt.Errorf("create Service data migration registry: %w", err)
		}
		var applied bool
		if err := tx.Raw(
			"SELECT EXISTS (SELECT 1 FROM service.data_migrations WHERE version = ?)",
			queryServiceConsumerContractMigrationVersion,
		).Scan(&applied).Error; err != nil {
			return fmt.Errorf("read Query Service consumer contract migration state: %w", err)
		}
		if applied {
			return nil
		}

		var services []models.QueryService
		if err := tx.
			Where("status = ?", "active").
			Where("protocols @> ?::jsonb", `{"rest_api":{"enabled":true}}`).
			Order("id ASC").
			Find(&services).Error; err != nil {
			return fmt.Errorf("list active REST Query Services for consumer contract migration: %w", err)
		}
		for index := range services {
			if err := validate(&services[index]); err == nil {
				continue
			} else if updateErr := tx.Model(&models.QueryService{}).
				Where("id = ? AND status = ?", services[index].ID, "active").
				Updates(map[string]interface{}{
					"status":        "error",
					"error_message": fmt.Sprintf("consumer contract v1 validation failed: %v", err),
				}).Error; updateErr != nil {
				return fmt.Errorf("mark Query Service %d consumer contract invalid: %w", services[index].ID, updateErr)
			}
			migrated++
		}

		if err := tx.Exec(
			`INSERT INTO service.data_migrations (version, name) VALUES (?, ?)
			 ON CONFLICT (version) DO NOTHING`,
			queryServiceConsumerContractMigrationVersion,
			"query_service_consumer_contract_v1",
		).Error; err != nil {
			return fmt.Errorf("record Query Service consumer contract migration: %w", err)
		}
		return nil
	})
	return migrated, err
}
