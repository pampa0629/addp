package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// migratePlatformAlertStore is a one-time in-place schema transition, not a
// runtime fallback. schema is fixed to monitor by the owner; disposable tests
// supply their own schema so they never rename shared test tables.
func migratePlatformAlertStore(db *gorm.DB, schema string) error {
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	qualified := func(name string) string { return quote(schema) + "." + quote(name) }
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", schema+".platform-alert-schema").Error; err != nil {
			return err
		}
		names := []string{"incidents", "events", "destinations", "deliveries"}
		oldCount, newCount := 0, 0
		for _, name := range names {
			for _, entry := range []struct {
				prefix string
				count  *int
			}{{"platform_log_", &oldCount}, {"platform_alert_", &newCount}} {
				var exists bool
				if err := tx.Raw("SELECT to_regclass(?) IS NOT NULL", qualified(entry.prefix+name)).Scan(&exists).Error; err != nil {
					return err
				}
				if exists {
					(*entry.count)++
				}
			}
		}
		if oldCount == 0 {
			return nil
		}
		if oldCount != len(names) || newCount != 0 {
			return fmt.Errorf("platform alert schema transition requires one complete source set; old=%d new=%d", oldCount, newCount)
		}
		for _, name := range names {
			if err := tx.Exec("ALTER TABLE " + qualified("platform_log_"+name) + " RENAME TO " + quote("platform_alert_"+name)).Error; err != nil {
				return err
			}
		}
		statements := []string{
			"ALTER TABLE " + qualified("platform_alert_incidents") + " RENAME COLUMN node TO subject_id",
			"ALTER TABLE " + qualified("platform_alert_incidents") + " ADD COLUMN category varchar(32) NOT NULL DEFAULT 'log_pipeline', ADD COLUMN subject_kind varchar(32) NOT NULL DEFAULT 'log_observer'",
			"ALTER TABLE " + qualified("platform_alert_incidents") + " ALTER COLUMN category DROP DEFAULT, ALTER COLUMN subject_kind DROP DEFAULT",
			"ALTER TABLE " + qualified("platform_alert_destinations") + ` ADD COLUMN categories jsonb NOT NULL DEFAULT '["log_pipeline"]'::jsonb`,
			"ALTER TABLE " + qualified("platform_alert_destinations") + " ALTER COLUMN categories DROP DEFAULT",
			"DROP INDEX IF EXISTS " + qualified("uq_monitor_platform_log_active"),
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		// Keep sequence state and index contents while removing legacy object names.
		for _, name := range names {
			table := "platform_alert_" + name
			var sequence *string
			if name == "incidents" || name == "destinations" {
				if err := tx.Raw("SELECT pg_get_serial_sequence(?, 'id')", qualified(table)).Scan(&sequence).Error; err != nil {
					return err
				}
				if sequence != nil && strings.Contains(*sequence, "platform_log_") {
					if err := tx.Exec("ALTER SEQUENCE " + *sequence + " RENAME TO " + quote("platform_alert_"+name+"_id_seq")).Error; err != nil {
						return err
					}
				}
			}
			var indexes []string
			if err := tx.Raw("SELECT indexname FROM pg_indexes WHERE schemaname=? AND tablename=?", schema, table).Scan(&indexes).Error; err != nil {
				return err
			}
			for _, index := range indexes {
				renamed := strings.ReplaceAll(index, "platform_log_", "platform_alert_")
				if table == "platform_alert_incidents" && strings.HasSuffix(renamed, "_node") {
					renamed = strings.TrimSuffix(renamed, "_node") + "_subject_id"
				}
				if renamed != index {
					if err := tx.Exec("ALTER INDEX " + qualified(index) + " RENAME TO " + quote(renamed)).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
