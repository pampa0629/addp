package repository

import (
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

// SchemaVersion increases with every module schema or startup migration change.
const SchemaVersion int64 = 7

// RequireMySQLRuntime checks every installed owner using the runtime connection.
func RequireMySQLRuntime(db *gorm.DB) error {
	for _, r := range []struct {
		name    string
		version int64
	}{
		{mysqlScanTasksOwner, mysqlScanTasksVersion}, {mysqlScanCatalogOwner, mysqlScanCatalogVersion},
		{mysqlScanChangesOwner, mysqlScanChangesVersion},
	} {
		if err := schema.RequireMySQL(db, r.name, r.version); err != nil {
			return err
		}
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.Require(db, "meta", SchemaVersion)
}
