package repository

import (
	_ "embed"
	"strings"

	"gorm.io/gorm"
)

//go:embed security.mysql.sql
var securityMySQLSchema string

func migrateMySQL(db *gorm.DB) error {
	for _, statement := range strings.Split(strings.TrimSpace(securityMySQLSchema), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
