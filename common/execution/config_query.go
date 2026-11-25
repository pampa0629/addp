package execution

import "gorm.io/gorm"

// WithConfigString matches a string at a module-defined dotted path in execution_config.
func WithConfigString(db *gorm.DB, path, value string) *gorm.DB {
	switch db.Dialector.Name() {
	case "postgres":
		return db.Where("execution_config #>> string_to_array(?, '.') = ?", path, value)
	case "mysql":
		return db.Where("JSON_UNQUOTE(JSON_EXTRACT(execution_config, ?)) = ?", "$."+path, value)
	case "sqlite":
		return db.Where("json_extract(execution_config, ?) = ?", "$."+path, value)
	default:
		unsupported := db.Session(&gorm.Session{})
		unsupported.AddError(gorm.ErrNotImplemented)
		return unsupported
	}
}
