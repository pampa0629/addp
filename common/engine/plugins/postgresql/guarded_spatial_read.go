package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

// The locked native table is the only row source. This deliberately accepts
// the generated EWKB SELECT, without extending the arbitrary SQL proof grammar.
func proveGuardedPostGISTable(ctx context.Context, tx *sql.Tx, path plugin.EngineCatalogPath) (*plugin.QueryReadSet, error) {
	parts := path.Segments
	if len(parts) != 3 || parts[0] != (plugin.EngineCatalogSegment{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer}) ||
		parts[1].Term != plugin.EngineCatalogTermSchema || parts[1].Kind != plugin.EngineCatalogKindNamespace ||
		parts[2].Term != plugin.EngineCatalogTermTable || parts[2].Kind != plugin.EngineCatalogKindTable ||
		strings.TrimSpace(parts[1].Name) != parts[1].Name || strings.TrimSpace(parts[2].Name) != parts[2].Name {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	readSet, err := plugin.NewQueryReadSet(path)
	if err != nil {
		return nil, err
	}
	parts = readSet.Paths[0].Segments
	dialect := commonquery.ForDialect("postgresql")
	schema, table := parts[1].Name, parts[2].Name
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "LOCK TABLE ONLY "+dialect.QualifiedTable(schema, table)+" IN ACCESS SHARE MODE"); err != nil {
		return nil, fmt.Errorf("%w: lock native table: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	var extensionSchema string
	err = tx.QueryRowContext(ctx, guardedPostGISTableSQL, schema, table).Scan(&extensionSchema)
	if err != nil {
		return nil, fmt.Errorf("%w: prove native PostGIS table: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog, "+dialect.QuoteIdentifier(extensionSchema)); err != nil {
		return nil, err
	}
	return readSet, nil
}

const guardedPostGISTableSQL = `
WITH spatial AS (
 SELECT n.nspname, t.oid AS geometry_oid
 FROM pg_catalog.pg_extension e
 JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace
 JOIN pg_catalog.pg_type t ON t.typnamespace=n.oid AND t.typname='geometry' AND t.typtype='b'
 JOIN pg_catalog.pg_proc p ON p.pronamespace=n.oid AND p.proname='st_asewkb'
  AND p.pronargs=1 AND p.proargtypes[0]=t.oid AND p.prorettype='pg_catalog.bytea'::regtype
  AND p.provolatile='i' AND NOT p.prosecdef AND p.provariadic=0
 JOIN pg_catalog.pg_language l ON l.oid=p.prolang AND l.lanname='c'
 WHERE e.extname='postgis'
 AND EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_type'::regclass
   AND d.objid=t.oid AND d.refclassid='pg_catalog.pg_extension'::regclass AND d.refobjid=e.oid AND d.deptype='e')
 AND EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_proc'::regclass
   AND d.objid=p.oid AND d.refclassid='pg_catalog.pg_extension'::regclass AND d.refobjid=e.oid AND d.deptype='e')
)
SELECT spatial.nspname
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
JOIN pg_catalog.pg_am am ON am.oid=c.relam AND am.amname='heap'
CROSS JOIN spatial
WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind='r' AND c.relpersistence IN ('p','u')
  AND NOT c.relrowsecurity AND NOT c.relhasrules
  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhrelid=c.oid OR i.inhparent=c.oid)
  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_index i
    JOIN pg_catalog.pg_class idx ON idx.oid=i.indexrelid
    JOIN pg_catalog.pg_am index_am ON index_am.oid=idx.relam
    JOIN pg_catalog.pg_proc handler ON handler.oid=index_am.amhandler
    WHERE i.indrelid=c.oid AND (i.indexprs IS NOT NULL OR i.indpred IS NOT NULL
      OR handler.pronamespace<>'pg_catalog'::regnamespace))
  AND EXISTS (SELECT 1 FROM pg_catalog.pg_locks l WHERE l.pid=pg_catalog.pg_backend_pid()
    AND l.locktype='relation' AND l.relation=c.oid AND l.mode='AccessShareLock' AND l.granted)
  AND (SELECT count(*) FROM pg_catalog.pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0
    AND NOT a.attisdropped AND a.atttypid=spatial.geometry_oid)=1
  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
    WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND
      (a.attgenerated<>'' OR (t.oid<>spatial.geometry_oid AND
        (t.typnamespace<>'pg_catalog'::regnamespace OR t.typtype<>'b' OR
         (t.typelem<>0 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_type element
           WHERE element.oid=t.typelem AND element.typnamespace='pg_catalog'::regnamespace AND element.typtype='b'))))))
`
