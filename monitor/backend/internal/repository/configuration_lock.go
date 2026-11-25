// Monitor holds these locks until the surrounding policy or evidence transaction ends.
package repository

import (
	"github.com/addp/monitor/internal/models"
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
