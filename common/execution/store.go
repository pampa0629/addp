package execution

import (
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// EnsureStore ensures the common execution schema objects are present.
func EnsureStore(db *gorm.DB) error {
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		return fmt.Errorf("failed to create common schema: %w", err)
	}

	exists, err := taskExecutionsTableExists(db)
	if err != nil {
		return err
	}

	if exists {
		if err := runSQLMigrations(db); err != nil {
			return err
		}
	}

	if err := db.AutoMigrate(&TaskExecution{}); err != nil {
		return fmt.Errorf("failed to auto-migrate TaskExecution: %w", err)
	}

	if !exists {
		if err := runSQLMigrations(db); err != nil {
			return err
		}
	}

	return nil
}

func taskExecutionsTableExists(db *gorm.DB) (bool, error) {
	var exists bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'common'
			  AND table_name = 'task_executions'
		)
	`).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("failed to check common.task_executions existence: %w", err)
	}
	return exists, nil
}

func runSQLMigrations(db *gorm.DB) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read execution migrations directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("failed to read execution migration file %s: %w", name, err)
		}

		log.Printf("[Execution Migration] ▶ Applying SQL migration: %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("failed to apply execution migration %s: %w", name, err)
		}
		log.Printf("[Execution Migration] ✅ Applied: %s", name)
	}

	return nil
}
