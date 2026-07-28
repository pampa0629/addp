package iamcli

import (
	"fmt"

	"github.com/addp/system/internal/migration"
	"gorm.io/gorm"
)

func RequireCurrentMigration(db *gorm.DB) error {
	catalog, err := migration.ReadCatalog(migration.EmbeddedSQL, migration.DefaultMigrationsRoot)
	if err != nil {
		return fmt.Errorf("读取内嵌 IAM migration 目录: %w", err)
	}
	var version uint
	var dirty bool
	if err := db.Raw(`SELECT version, dirty FROM system.schema_migrations`).Row().Scan(&version, &dirty); err != nil {
		return fmt.Errorf("读取 IAM migration 状态: %w", err)
	}
	if dirty || version != catalog.LatestVersion {
		return fmt.Errorf("IAM migration 必须为 (%d, clean)，当前为 (%d, dirty=%t)", catalog.LatestVersion, version, dirty)
	}
	return nil
}
