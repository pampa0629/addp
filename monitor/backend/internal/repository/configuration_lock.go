// Monitor holds these locks until the surrounding policy or evidence transaction ends.
package repository

import (
	"errors"

	"github.com/addp/monitor/internal/models"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func lockMySQLConfiguration(tx *gorm.DB, key string) error {
	return tx.Exec(`INSERT INTO monitor.configuration_locks (configuration_key) VALUES (?)
		ON DUPLICATE KEY UPDATE configuration_key=configuration_key`, key).Error
}

// LockAlertRuleSet serializes the capacity check and insertion for this rule family.
func LockAlertRuleSet(tx *gorm.DB, key string) error {
	if tx.Dialector.Name() == "mysql" {
		return lockMySQLConfiguration(tx, key)
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error
}

// LockResourceQueryPolicy protects a policy edit and any evaluation relying on its version.
func LockResourceQueryPolicy(tx *gorm.DB) error {
	if tx.Dialector.Name() == "mysql" {
		return lockMySQLConfiguration(tx, "monitor.resource-query-policy")
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", models.ResourceQueryPolicyLockNamespace, models.ResourceQueryPolicyLockID).Error
}

// tryLockMonitoringTargets preserves the non-blocking admission used by target writes.
func tryLockMonitoringTargets(tx *gorm.DB) error {
	if tx.Dialector.Name() == "mysql" {
		var key string
		err := tx.Raw(`SELECT configuration_key FROM monitor.configuration_locks
			WHERE configuration_key = 'monitor.monitoring-targets' FOR UPDATE NOWAIT`).Row().Scan(&key)
		var driverErr *mysqlDriver.MySQLError
		const lockNotAvailable = 3572
		if errors.As(err, &driverErr) && driverErr.Number == lockNotAvailable {
			return ErrTargetBusy
		}
		return err
	}
	var acquired bool
	if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?, ?)", 1094992976, 6001).Scan(&acquired).Error; err != nil {
		return err
	}
	if !acquired {
		return ErrTargetBusy
	}
	return nil
}
