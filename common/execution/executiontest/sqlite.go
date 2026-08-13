// Package executiontest provides test database setup for the shared execution store.
package executiontest

import (
	"fmt"
	"strings"

	"github.com/addp/common/execution"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// EnsureSQLiteStore attaches the common schema and creates task_executions from
// the canonical execution model. SQLite ATTACH state is connection-scoped, so
// the test connection pool must use a single connection.
func EnsureSQLiteStore(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("execution test database is nil")
	}
	if db.Dialector.Name() != "sqlite" {
		return fmt.Errorf("execution test store requires sqlite, got %s", db.Dialector.Name())
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get SQLite connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	type attachedDatabase struct {
		Name string `gorm:"column:name"`
	}
	var databases []attachedDatabase
	if err := db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		return fmt.Errorf("list attached SQLite databases: %w", err)
	}
	for _, database := range databases {
		if database.Name == "common" {
			return migrateSQLiteStore(db)
		}
	}

	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		return fmt.Errorf("attach SQLite common schema: %w", err)
	}
	return migrateSQLiteStore(db)
}

func migrateSQLiteStore(db *gorm.DB) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&execution.TaskExecution{}); err != nil {
		return fmt.Errorf("parse canonical task execution model: %w", err)
	}

	columns := make([]string, 0, len(statement.Schema.DBNames))
	for _, name := range statement.Schema.DBNames {
		field := statement.Schema.FieldsByDBName[name]
		if field.IgnoreMigration {
			continue
		}
		dataType := db.Migrator().FullDataTypeOf(field)
		columns = append(columns, quoteSQLiteIdentifier(name)+" "+db.Dialector.Explain(dataType.SQL, dataType.Vars...))
	}

	createTableSQL := "CREATE TABLE IF NOT EXISTS common.task_executions (" + strings.Join(columns, ", ") + ")"
	if err := db.Exec(createTableSQL).Error; err != nil {
		return fmt.Errorf("create SQLite task execution table: %w", err)
	}

	for _, index := range statement.Schema.ParseIndexes() {
		if err := createSQLiteIndex(db, index); err != nil {
			return err
		}
	}
	return nil
}

func createSQLiteIndex(db *gorm.DB, index *schema.Index) error {
	columns := make([]string, 0, len(index.Fields))
	for _, field := range index.Fields {
		if field.Expression != "" {
			columns = append(columns, field.Expression)
			continue
		}
		column := quoteSQLiteIdentifier(field.DBName)
		if field.Sort != "" {
			column += " " + field.Sort
		}
		columns = append(columns, column)
	}

	class := ""
	if index.Class == "UNIQUE" {
		class = "UNIQUE "
	}
	query := "CREATE " + class + "INDEX IF NOT EXISTS common." + quoteSQLiteIdentifier(index.Name) +
		" ON task_executions (" + strings.Join(columns, ", ") + ")"
	if index.Where != "" {
		query += " WHERE " + index.Where
	}
	if err := db.Exec(query).Error; err != nil {
		return fmt.Errorf("create SQLite task execution index %s: %w", index.Name, err)
	}
	return nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
