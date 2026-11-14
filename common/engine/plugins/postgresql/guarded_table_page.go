package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

// The protocol adapter opts into this proof explicitly. It does not inherit
// PostgreSQL's arbitrary-query or PostGIS certification.
func (p *PostgreSQLPlugin) proveGuardedNonSpatialTable(ctx context.Context, tx *sql.Tx, path plugin.EngineCatalogPath) (*plugin.QueryReadSet, error) {
	parts := path.Segments
	if len(parts) != 3 || parts[0] != (plugin.EngineCatalogSegment{Kind: "server", Term: "server"}) ||
		parts[1].Kind != plugin.EngineCatalogKindNamespace || parts[1].Term != plugin.EngineCatalogTermSchema ||
		parts[2].Kind != plugin.EngineCatalogKindTable || parts[2].Term != plugin.EngineCatalogTermTable ||
		strings.TrimSpace(parts[1].Name) != parts[1].Name || strings.TrimSpace(parts[2].Name) != parts[2].Name {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	set, err := plugin.NewQueryReadSet(path)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
		return nil, err
	}
	qualified := commonquery.ForDialect("postgresql").QualifiedTable(parts[1].Name, parts[2].Name)
	if _, err := tx.ExecContext(ctx, "LOCK TABLE ONLY "+qualified+" IN ACCESS SHARE MODE"); err != nil {
		return nil, fmt.Errorf("%w: lock native table: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	var safe bool
	proof := guardedNonSpatialTableSQL
	if p.identity != nil && p.identity.TableReadCatalogCheckSQL != "" {
		proof = p.identity.TableReadCatalogCheckSQL
	}
	err = tx.QueryRowContext(ctx, proof, parts[1].Name, parts[2].Name).Scan(&safe)
	if err != nil {
		return nil, fmt.Errorf("%w: prove locked native table: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if !safe {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	return set, nil
}

// Observe the certified HighGo catalog and the PG protocol fixture through
// catalog row values. This does not grant a source when its relation identity,
// access method, types, rules or index dependencies remain unresolved.
const guardedNonSpatialTableSQL = `SELECT c.relkind='r' AND c.relpersistence IN ('p','u')
 AND NOT c.relrowsecurity AND NOT c.relhasrules
 AND COALESCE((pg_catalog.to_jsonb(c)->>'relam')::oid,0) IN
   (0,(SELECT oid FROM pg_catalog.pg_am WHERE amname='heap'))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhrelid=c.oid OR i.inhparent=c.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_index i
   JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
   JOIN pg_catalog.pg_am am ON am.oid=idx.relam
   WHERE i.indrelid=c.oid AND (i.indexprs IS NOT NULL OR i.indpred IS NOT NULL
    OR am.amname NOT IN ('btree','hash','gin','gist','spgist','brin')
    OR EXISTS (SELECT 1 FROM pg_catalog.unnest(i.indclass) op
      JOIN pg_catalog.pg_opclass oc ON oc.oid=op
      JOIN pg_catalog.pg_namespace ns ON ns.oid=oc.opcnamespace WHERE ns.nspname<>'pg_catalog')))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
   JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
   JOIN pg_catalog.pg_namespace tn ON tn.oid=t.typnamespace
   WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
    AND (COALESCE(pg_catalog.to_jsonb(a)->>'attgenerated','')<>'' OR tn.nspname<>'pg_catalog'
     OR t.typtype<>'b' OR (t.typelem<>0 AND NOT EXISTS
       (SELECT 1 FROM pg_catalog.pg_type elem JOIN pg_catalog.pg_namespace en ON en.oid=elem.typnamespace
        WHERE elem.oid=t.typelem AND en.nspname='pg_catalog' AND elem.typtype='b'))))
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=$1 AND c.relname=$2`
