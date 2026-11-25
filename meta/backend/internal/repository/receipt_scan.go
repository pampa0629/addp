package repository

import "gorm.io/gorm"

// LockReceiptScan serializes the first scan for this receipt until the caller's transaction ends.
func LockReceiptScan(tx *gorm.DB, key string) error {
	switch tx.Dialector.Name() {
	case "postgres":
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error
	case "mysql":
		return tx.Exec(`INSERT INTO meta.receipt_scan_locks (receipt_key) VALUES (UNHEX(SHA2(?,256)))
			ON DUPLICATE KEY UPDATE receipt_key=receipt_key`, key).Error
	case "sqlite":
		// SQLite serializes writers in the deterministic service fixture.
		return nil
	default:
		return gorm.ErrNotImplemented
	}
}

// WithScanConfigValue matches a string at a module-defined dotted key path.
func WithScanConfigValue(db *gorm.DB, path, value string) *gorm.DB {
	switch db.Dialector.Name() {
	case "postgres":
		return db.Where("execution_config #>> string_to_array(?, '.') = ?", path, value)
	case "mysql":
		return db.Where("JSON_UNQUOTE(JSON_EXTRACT(execution_config, ?)) = ?", "$."+path, value)
	default:
		return db.Where("json_extract(execution_config, ?) = ?", "$."+path, value)
	}
}
