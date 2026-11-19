package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/system/internal/migration"
	_ "github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "apply one registered narrowly scoped migration repair")
	migrationVersion := flag.Uint("migration", 0, "registered dirty migration to repair: 75, 113, 130, or 240")
	flag.Parse()
	if !*apply {
		fmt.Fprintln(os.Stderr, "refusing mutation without --apply")
		os.Exit(2)
	}
	if *migrationVersion != 75 && *migrationVersion != 113 && *migrationVersion != 130 && *migrationVersion != 240 {
		fmt.Fprintln(os.Stderr, "--migration must be exactly 75, 113, 130, or 240")
		os.Exit(2)
	}
	dsn, err := bootstrap.PostgreSQLDSN(database.Config{
		Host:     commonconfig.GetEnv("POSTGRES_HOST", "localhost"),
		Port:     commonconfig.GetEnv("POSTGRES_PORT", "15432"),
		User:     commonconfig.GetEnv("POSTGRES_USER", "addp"),
		Password: commonconfig.GetEnv("POSTGRES_PASSWORD", "addp_password"),
		DBName:   commonconfig.GetEnv("POSTGRES_DB", "addp"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open migration repair database failed")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "connect migration repair database failed")
		os.Exit(1)
	}
	switch *migrationVersion {
	case 240:
		if err := migration.RepairDirtyProcessCgroupMigration240(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "migration 240 repair failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migration 240 repair completed; verified_target_facts=pre-migration; migration_state=239/clean")
	case 75:
		updated, err := migration.RepairDirtyExecutionAudienceMigration75(ctx, db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "migration 75 repair failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("migration 75 repair completed; normalized_authorizations=%d; migration_state=74/clean\n", updated)
	case 113:
		if err := migration.RepairDirtySecurityModuleMigration113(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "migration 113 repair failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migration 113 repair completed; verified_target_facts=zero; migration_state=112/clean")
	case 130:
		if err := migration.RepairDirtySecurityProtectionAccessRequestMigration130(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "migration 130 repair failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migration 130 repair completed; verified_target_facts=pre-migration; migration_state=129/clean")
	}
}
