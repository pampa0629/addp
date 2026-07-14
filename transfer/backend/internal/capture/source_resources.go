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
	DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
}

type PostgreSQLSourceResources struct{}

func (PostgreSQLSourceResources) DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil {
		return fmt.Errorf("PostgreSQL capture cleanup requires plan and resource")
	}
	if plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("refuse to clean PostgreSQL capture resources because the source connection identity changed")
	}
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()

	if resource.SlotOwned {
		var plugin, database string
		var active bool
		err := db.QueryRowContext(ctx, `
			SELECT plugin, database, active FROM pg_replication_slots WHERE slot_name = $1`, resource.SlotName).
			Scan(&plugin, &database, &active)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("inspect ADDP-owned replication slot: %w", err)
		}
		if err == nil {
			if plugin != "pgoutput" || database != resource.SourceDatabase {
				return fmt.Errorf("refuse to drop replication slot %q because its identity does not match capture registration", resource.SlotName)
			}
			if active {
				return fmt.Errorf("%w: replication slot %q", ErrSourceCaptureResourceActive, resource.SlotName)
			}
			if _, err := db.ExecContext(ctx, `SELECT pg_drop_replication_slot($1)`, resource.SlotName); err != nil {
				return fmt.Errorf("drop ADDP-owned replication slot: %w", err)
			}
		}
	}

	if resource.PublicationOwned {
		rows, err := db.QueryContext(ctx, `
			SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1 ORDER BY schemaname, tablename`, resource.PublicationName)
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
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)`, resource.PublicationName).Scan(&exists); err != nil {
			return err
		}
		if exists {
			if len(tables) != 1 || tables[0][0] != resource.SourceSchema || tables[0][1] != resource.SourceTable {
				return fmt.Errorf("refuse to drop publication %q because its table identity does not match capture registration", resource.PublicationName)
			}
			if _, err := db.ExecContext(ctx, `DROP PUBLICATION `+pq.QuoteIdentifier(resource.PublicationName)); err != nil {
				return fmt.Errorf("drop ADDP-owned publication: %w", err)
			}
		}
	}
	return nil
}
