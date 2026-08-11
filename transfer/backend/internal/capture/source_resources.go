package capture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/addp/transfer/internal/models"
	"github.com/lib/pq"
)

var ErrSourceCaptureResourceActive = errors.New("source capture resource is still active")

type SourceResourceControl interface {
	EnsureOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
	DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
}

type DatabaseSourceResources struct{}

func (DatabaseSourceResources) EnsureOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType {
		return fmt.Errorf("database capture provisioning requires matching plan and resource")
	}
	if plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("refuse to provision database capture resources because the source connection identity changed")
	}
	if plan.SourceType == models.CaptureSourceOracle {
		return ensureOracleSpatialOwnedResources(ctx, plan, resource)
	}
	return nil
}

func (DatabaseSourceResources) DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType {
		return fmt.Errorf("database capture cleanup requires matching plan and resource")
	}
	if plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("refuse to clean database capture resources because the source connection identity changed")
	}
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		return dropPostgreSQLOwnedResources(ctx, plan, resource)
	case models.CaptureSourceMySQL:
		if resource.MySQL == nil {
			return fmt.Errorf("MySQL capture cleanup requires provider resources")
		}
		return nil
	case models.CaptureSourceOracle:
		if resource.Oracle == nil {
			return fmt.Errorf("Oracle capture cleanup requires provider resources")
		}
		return dropOracleSpatialOwnedResources(ctx, plan, resource)
	default:
		return fmt.Errorf("unsupported database capture source type %q", plan.SourceType)
	}
}

func dropPostgreSQLOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil {
		return fmt.Errorf("PostgreSQL capture cleanup requires plan and resource")
	}
	if plan.SourceType != models.CaptureSourcePostgreSQL || resource.SourceType != models.CaptureSourcePostgreSQL || resource.PostgreSQL == nil {
		return fmt.Errorf("PostgreSQL capture cleanup requires matching provider resources")
	}
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()

	provider := resource.PostgreSQL
	if provider.SlotOwned {
		var plugin, database string
		var active bool
		err := db.QueryRowContext(ctx, `
			SELECT plugin, database, active FROM pg_replication_slots WHERE slot_name = $1`, provider.SlotName).
			Scan(&plugin, &database, &active)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("inspect ADDP-owned replication slot: %w", err)
		}
		if err == nil {
			if plugin != "pgoutput" || database != resource.SourceDatabase {
				return fmt.Errorf("refuse to drop replication slot %q because its identity does not match capture registration", provider.SlotName)
			}
			if active {
				return fmt.Errorf("%w: replication slot %q", ErrSourceCaptureResourceActive, provider.SlotName)
			}
			if _, err := db.ExecContext(ctx, `SELECT pg_drop_replication_slot($1)`, provider.SlotName); err != nil {
				return fmt.Errorf("drop ADDP-owned replication slot: %w", err)
			}
		}
	}

	if provider.PublicationOwned {
		rows, err := db.QueryContext(ctx, `
			SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1 ORDER BY schemaname, tablename`, provider.PublicationName)
		if err != nil {
			return fmt.Errorf("inspect ADDP-owned publication: %w", err)
		}
		var tables [][2]string
		for rows.Next() {
			var schema, table string
			if err := rows.Scan(&schema, &table); err != nil {
				rows.Close()
				return err
			}
			tables = append(tables, [2]string{schema, table})
		}
		if err := rows.Close(); err != nil {
			return err
		}
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)`, provider.PublicationName).Scan(&exists); err != nil {
			return err
		}
		if exists {
			if len(tables) != 1 || tables[0][0] != resource.SourceSchema || tables[0][1] != resource.SourceTable {
				return fmt.Errorf("refuse to drop publication %q because its table identity does not match capture registration", provider.PublicationName)
			}
			if _, err := db.ExecContext(ctx, `DROP PUBLICATION `+pq.QuoteIdentifier(provider.PublicationName)); err != nil {
				return fmt.Errorf("drop ADDP-owned publication: %w", err)
			}
		}
	}
	return nil
}
