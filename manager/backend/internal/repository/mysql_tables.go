package repository

import (
	_ "embed"
	"strings"

	"gorm.io/gorm"
)

//go:embed content_index.mysql.sql
var mysqlContentIndexTables string

//go:embed data_profile.mysql.sql
var mysqlProfileTables string

//go:embed task_definition.mysql.sql
var mysqlTaskDefinitionTables string

//go:embed inference_settings.mysql.sql
var mysqlInferenceSettingsTables string

//go:embed preview_policy.mysql.sql
var mysqlPreviewPolicyTables string

//go:embed preview_artifact.mysql.sql
var mysqlPreviewArtifactTables string

func prepareManagerMySQLTables(db *gorm.DB, ddl string) error {
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(ddl), ";"), ";") {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
