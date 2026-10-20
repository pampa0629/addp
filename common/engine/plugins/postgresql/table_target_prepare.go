package postgresql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/engine/plugin"
)

var _ plugin.TableTargetPreparer = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) PrepareTableTarget(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableTargetPrepareOptions) (plugin.TableTargetPreparation, error) {
	if err := p.CheckTableWriteDefinition(plugin.TableWriteDefinitionFromOptions(opts.Definition)).UnsupportedError(); err != nil {
		return plugin.TableTargetPreparation{}, err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return plugin.TableTargetPreparation{}, err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return plugin.TableTargetPreparation{}, fmt.Errorf("build postgresql target dsn: %w", err)
	}
	db, err := sql.Open(p.sqlDriverName(), dsn)
	if err != nil {
		return plugin.TableTargetPreparation{}, fmt.Errorf("open postgresql target connection: %w", err)
	}
	defer db.Close()
	// Catalog presence is not filtered by information_schema privileges. Absence
	// is only an observation; the strict CREATE below must win the actual name.
	var existed bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=$1 AND c.relname=$2
	)`, schema, table).Scan(&existed)
	if err != nil {
		return plugin.TableTargetPreparation{}, fmt.Errorf("observe postgresql target: %w", err)
	}
	result, err := prepareObservedPostgresTableTarget(ctx, db, schema, table, opts, existed)
	if witness := decodePostgresCreationWitness(result.CreationWitness); err == nil && p.Type() == "postgresql" && path.EngineID != 0 && witness != nil {
		witness.EngineID = uint64(path.EngineID)
		result.CreationWitness = postgresCreationWitnessJSON(witness)
	} else {
		result.CreationWitness = ""
	}
	return result, err
}

func prepareObservedPostgresTableTarget(ctx context.Context, db *sql.DB, schema, table string, opts plugin.TableTargetPrepareOptions, existed bool) (plugin.TableTargetPreparation, error) {
	disposition := plugin.TableTargetCreated
	if existed {
		disposition = plugin.TableTargetReused
		if opts.RecreateExisting {
			if err := dropPostgresTable(ctx, db, schema, table); err != nil {
				return plugin.TableTargetPreparation{}, err
			}
			disposition = plugin.TableTargetRecreated
		}
	}
	// Never delete after an absent observation. IF NOT EXISTS would silently
	// adopt a concurrent creator's table and falsely report our own creation.
	witness, err := createPostgresTableWithPolicy(ctx, db, schema, table, opts.Definition.Fields, opts.Definition.SpatialInfo, true, existed)
	if err != nil {
		return plugin.TableTargetPreparation{}, err
	}
	return plugin.TableTargetPreparation{Disposition: disposition, CreationWitness: witness}, nil
}
