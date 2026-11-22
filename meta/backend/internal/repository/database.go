package repository

import (
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/addp/common/cleanup"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/logger"
	"github.com/addp/common/schema"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := OpenRuntimeDatabase(cfg, "META")
	if err != nil {
		return nil, err
	}

	// Apply custom settings for Meta module
	// Set connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// Apply custom logger (optional, overwrites common logger)
	dbLogger := newGormLogger(logger.With("component", "gorm"), gormLogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  gormLogger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	db.Logger = dbLogger

	DB = db
	logger.L().Info("数据库连接成功", "host", cfg.DBHost, "schema", cfg.DBSchema)
	return db, nil
}

// OpenRuntimeDatabase opens the connection for the selected Meta process. The
// deployment maps the Backend and Worker to their own runtime identities.
func OpenRuntimeDatabase(cfg *config.Config, process string) (*gorm.DB, error) {
	if cfg.DBSchema != "meta" {
		return nil, fmt.Errorf("meta module schema must be meta")
	}
	pg := database.Config{Host: cfg.DBHost, Port: cfg.DBPort, DBName: cfg.DBName, Schema: "meta", SSLMode: "disable"}
	if commonConfig.GetEnv("ADDP_INFRA_DATABASE_PROVIDER", "postgres") == "postgres" {
		prefix := "ADDP_PG_" + process
		pg.User, pg.Password = os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD")
		if pg.User == "" || pg.Password == "" {
			return nil, fmt.Errorf("Meta PostgreSQL runtime requires %s credentials", prefix)
		}
	}
	db, err := bootstrap.OpenRuntimeDatabase(pg, "meta", "ADDP_MYSQL_"+process, &gorm.Config{Logger: gormLogger.Default.LogMode(gormLogger.Info)})
	if err != nil {
		return nil, err
	}
	if err := RequireRuntime(db); err != nil {
		if pool, openErr := db.DB(); openErr == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	return db, nil
}

func OpenPostgresInstallation(cfg *config.Config) (*gorm.DB, error) {
	return bootstrap.OpenDatabase(database.Config{Host: cfg.DBHost, Port: cfg.DBPort, DBName: cfg.DBName, User: cfg.DBUser, Password: cfg.DBPassword, Schema: "meta", SSLMode: "disable"}, &gorm.Config{Logger: gormLogger.Default.LogMode(gormLogger.Silent)})
}

//go:embed postgres_fact_triggers.sql
var postgresFactTriggerAuthority string

func PreparePostgres(db *gorm.DB, triggerOwner string, retireCollectionApproval func(*gorm.DB) error) error {
	if triggerOwner == "" {
		return fmt.Errorf("Meta PostgreSQL preparation requires ADDP_PG_META_TRIGGER_OWNER")
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := schema.Migrate(tx, "meta", SchemaVersion, func(conn *gorm.DB) error {
			var previous int64
			if err := conn.Raw("SELECT COALESCE(MAX(version),0) FROM meta.startup_schema_revision").Scan(&previous).Error; err != nil {
				return err
			}
			// Revision 8 only changes the deployment and fact trigger authority.
			if previous == 7 {
				return nil
			}
			if err := conn.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}, &models.ObjectIndexDeletion{}); err != nil {
				return err
			}
			if err := applySQLMigrations(conn); err != nil {
				return err
			}
			// Version 4 retired PG collection approvals. Later schema upgrades must
			// preserve schedules and executions created under the new policy.
			if previous < 4 {
				if err := retireCollectionApproval(conn); err != nil {
					return err
				}
			}
			return cleanup.MigrateResults(conn, "meta")
		}); err != nil {
			return err
		}
		if err := tx.Exec("SELECT set_config('search_path','pg_catalog,pg_temp',true), set_config('addp.meta_trigger_owner',?,true)", triggerOwner).Error; err != nil {
			return err
		}
		return tx.Exec(postgresFactTriggerAuthority).Error
	})
}
