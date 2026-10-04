package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"gorm.io/gorm"
)

type postgresTableResultPlan struct {
	provider *PostgreSQLPlugin
	source   plugin.PreparedQuery
	target   plugin.EngineCatalogPath
	mode     string
	mu       sync.Mutex
	consumed bool
}

func (p *PostgreSQLPlugin) PrepareTableResult(ctx context.Context, connInfo plugin.ConnectionInfo, req plugin.TableResultRequest) (plugin.PreparedTableResult, error) {
	if err := plugin.ValidateQueryReadSet(req.Query, &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{req.Target}}); err != nil {
		return nil, err
	}
	segments := plugin.EngineCatalogPathWithoutRoot(req.Target).Segments
	if req.Query.EngineID == 0 || req.Target.EngineID != req.Query.EngineID || len(segments) != 2 || segments[0].Term != plugin.EngineCatalogTermSchema || segments[1].Kind != plugin.EngineCatalogKindTable ||
		(req.WriteMode != "append" && req.WriteMode != "overwrite") || !req.Query.Options.ReadOnly || req.Query.Options.Limit != 0 || req.Query.Options.Offset != 0 {
		return nil, fmt.Errorf("invalid existing-table result request")
	}
	canonical := plugin.EngineCatalogBranchLeafPath(p.EngineCatalogModel(), req.Query.EngineID, plugin.EngineCatalogTermSchema, segments[0].Name, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, segments[1].Name)
	if !reflect.DeepEqual(req.Target, canonical) {
		return nil, fmt.Errorf("table result target does not match the provider catalog model")
	}
	if err := p.rejectHiddenCatalogPath(req.Target); err != nil {
		return nil, err
	}
	source, err := p.PrepareQuery(ctx, connInfo, req.Query)
	if err != nil {
		return nil, err
	}
	target := req.Target
	target.Segments = append([]plugin.EngineCatalogSegment(nil), req.Target.Segments...)
	return &postgresTableResultPlan{provider: p, source: source, target: target, mode: req.WriteMode}, nil
}

func (p *postgresTableResultPlan) ReadSet(ctx context.Context) (*plugin.QueryReadSet, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return nil, plugin.ErrPreparedQueryConsumed
	}
	return p.source.ReadSet(ctx)
}

func (p *postgresTableResultPlan) Execute(ctx context.Context) (*plugin.TableResult, error) {
	p.mu.Lock()
	if p.consumed {
		p.mu.Unlock()
		return nil, plugin.ErrPreparedQueryConsumed
	}
	p.consumed = true
	p.mu.Unlock()
	// Freeze the read set before consuming the same already-bound source request.
	readSet, err := p.source.ReadSet(ctx)
	if err != nil {
		return nil, err
	}
	connInfo, req, err := plugin.ConsumeSQLPreparedQuery(p.source, p.provider)
	if err != nil {
		return nil, err
	}
	dependencies, err := inspectPostgresSQLReadDependencies(req.Query)
	if err != nil {
		return nil, err
	}
	db, err := p.provider.CreateConnectionPool(connInfo, &plugin.PoolConfig{MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer pool.Close() //nolint:errcheck
	var result *plugin.TableResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		native, ok := tx.Statement.ConnPool.(*sql.Tx)
		if !ok {
			return fmt.Errorf("table result requires a native SQL transaction")
		}
		catalog := &postgresDatabaseReadCatalog{db: native}
		current, err := p.provider.resolvePostgresQueryReadSet(ctx, req, dependencies, catalog)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, readSet) {
			return fmt.Errorf("table result read set changed before write")
		}
		paths := append([]plugin.EngineCatalogPath(nil), readSet.Paths...)
		for _, path := range paths {
			if reflect.DeepEqual(path, p.target) {
				return fmt.Errorf("table result target is also a read source")
			}
		}
		paths = append(paths, p.target)
		sort.Slice(paths, func(i, j int) bool { return tableResultPathName(paths[i]) < tableResultPathName(paths[j]) })
		for _, path := range paths {
			mode := "ACCESS SHARE"
			if reflect.DeepEqual(path, p.target) {
				mode = "EXCLUSIVE"
			}
			if err := tx.Exec("LOCK TABLE " + tableResultPathName(path) + " IN " + mode + " MODE").Error; err != nil {
				return err
			}
		}
		// Resolve again under schema locks; view closure and search-path bindings may
		// have changed while waiting for locks. Never execute a different read set.
		locked, err := p.provider.resolvePostgresQueryReadSet(ctx, req, dependencies, catalog)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(locked, readSet) {
			return fmt.Errorf("table result read set changed while locking")
		}
		targetParts := plugin.EngineCatalogPathWithoutRoot(p.target).Segments
		target, err := catalog.ResolveRelation(ctx, postgresRelationReference{Schema: targetParts[0].Name, Name: targetParts[1].Name})
		if err != nil {
			return err
		}
		if target.Relkind != "r" && target.Relkind != "p" {
			return fmt.Errorf("table result target must be a native table")
		}
		if err := p.provider.validateTableResultTarget(ctx, native, target.OID); err != nil {
			return err
		}
		targetFields, err := p.provider.listColumns(ctx, tx, target.Schema, target.Name)
		if err != nil {
			return err
		}
		if len(targetFields) == 0 {
			return fmt.Errorf("table result target fields unavailable")
		}
		var columnCount int
		if err := native.QueryRowContext(ctx, `SELECT count(*) FROM pg_catalog.pg_attribute WHERE attrelid=$1 AND attnum>0 AND NOT attisdropped`, target.OID).Scan(&columnCount); err != nil {
			return err
		}
		if columnCount != len(targetFields) {
			return fmt.Errorf("table result target fields are not completely visible")
		}
		sources := make([]plugin.QueryOutputSource, 0, len(readSet.Paths))
		for _, path := range readSet.Paths {
			parts := plugin.EngineCatalogPathWithoutRoot(path).Segments
			fields, err := p.provider.listColumns(ctx, tx, parts[0].Name, parts[1].Name)
			if err != nil {
				return err
			}
			if len(fields) == 0 {
				return fmt.Errorf("table result source fields unavailable")
			}
			sources = append(sources, plugin.QueryOutputSource{Path: path, Fields: fields})
		}
		parsed, err := pgquery.Parse(req.Query)
		if err != nil {
			return err
		}
		positional := map[*pgquery.SelectStmt]bool{}
		freezePostgresPositionalOutputs(parsed.GetStmts()[0].GetStmt().GetSelectStmt(), positional)
		resolver := postgresOutputResolver{ctx: ctx, catalog: catalog, sources: sources, opaqueSources: map[int]bool{}, positionalOutputs: positional}
		columns, proofErr := resolver.selectColumns(parsed.GetStmts()[0].GetStmt().GetSelectStmt(), nil)
		result = &plugin.TableResult{TargetFields: targetFields, FieldLineageComplete: proofErr == nil && len(columns) == len(targetFields) && len(resolver.opaqueSources) == 0}
		for _, source := range sources {
			result.Sources = append(result.Sources, plugin.TableResultSource{Path: source.Path, Fields: source.Fields})
		}
		if result.FieldLineageComplete {
			result.FieldMappings = postgresTableResultMappings(columns, sources, targetFields)
		}
		names := make([]string, len(targetFields))
		for i, field := range targetFields {
			names[i] = commonquery.ForDialect(p.provider.SQLDialect()).QuoteIdentifier(field.Name)
		}
		table := tableResultPathName(p.target)
		statement := "INSERT INTO " + table + " (" + strings.Join(names, ",") + ") " + req.Query
		prepared, err := native.PrepareContext(ctx, statement)
		if err != nil {
			return err
		} // server checks every positional assignment before deleting
		defer prepared.Close() //nolint:errcheck
		if p.mode == "overwrite" {
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return err
			}
		}
		write, err := prepared.ExecContext(ctx, req.Options.Args...)
		if err != nil {
			return err
		}
		result.RowsAffected, err = write.RowsAffected()
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func tableResultPathName(path plugin.EngineCatalogPath) string {
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	return commonquery.ForDialect(commonquery.DialectPostgreSQL).QualifiedTable(segments[0].Name, segments[1].Name)
}

// User triggers/rules can introduce reads absent from the frozen SELECT or
// rewrite stored values. They cannot be executed under a SELECT-only read gate.
func (p *PostgreSQLPlugin) validateTableResultTarget(ctx context.Context, tx *sql.Tx, oid int64) error {
	rowSecurity, generated := "c.relrowsecurity", "a.attgenerated <> ''"
	builtinNamespace := "n.nspname='pg_catalog'"
	if p.identity != nil {
		if expression := p.identity.TableResultRowSecurityExpression; expression != "" {
			rowSecurity = expression
		}
		if expression := p.identity.TableResultGeneratedExpression; expression != "" {
			generated = expression
		}
		if expression := p.identity.TableResultBuiltinNamespaceExpression; expression != "" {
			builtinNamespace = expression
		}
	}
	var unsafe bool
	query := fmt.Sprintf(`WITH RECURSIVE targets(oid) AS (
 SELECT $1::oid UNION ALL SELECT i.inhrelid FROM pg_catalog.pg_inherits i JOIN targets t ON i.inhparent=t.oid
 ) SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger g JOIN targets t ON g.tgrelid=t.oid WHERE NOT g.tgisinternal AND g.tgenabled <> 'D')
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_rewrite r JOIN targets t ON r.ev_class=t.oid WHERE r.rulename <> '_RETURN')
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN targets t ON c.oid=t.oid WHERE %s)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c JOIN targets t ON (c.conrelid=t.oid OR c.confrelid=t.oid) WHERE c.contype='f')
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c JOIN targets t ON c.conrelid=t.oid
 JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_constraint'::regclass AND d.objid=c.oid AND d.refclassid='pg_catalog.pg_proc'::regclass
 JOIN pg_catalog.pg_proc f ON f.oid=d.refobjid JOIN pg_catalog.pg_language l ON l.oid=f.prolang JOIN pg_catalog.pg_namespace n ON n.oid=f.pronamespace
 WHERE c.contype='c' AND (NOT (%s) OR l.lanname<>'internal'))
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_index i JOIN targets t ON i.indrelid=t.oid
 JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=i.indexrelid AND d.refclassid='pg_catalog.pg_proc'::regclass
 JOIN pg_catalog.pg_proc f ON f.oid=d.refobjid JOIN pg_catalog.pg_language l ON l.oid=f.prolang JOIN pg_catalog.pg_namespace n ON n.oid=f.pronamespace
 WHERE NOT (%s) OR l.lanname<>'internal')
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a JOIN targets t ON a.attrelid=t.oid
 JOIN pg_catalog.pg_type ty ON ty.oid=a.atttypid JOIN pg_catalog.pg_namespace n ON n.oid=ty.typnamespace
 WHERE a.attnum>0 AND NOT a.attisdropped AND NOT (%s))
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a JOIN targets t ON a.attrelid=t.oid WHERE a.attnum>0 AND NOT a.attisdropped AND (%s))`, rowSecurity, builtinNamespace, builtinNamespace, builtinNamespace, generated)
	err := tx.QueryRowContext(ctx, query, oid).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return fmt.Errorf("table result target has unresolved write-side effects")
	}
	return nil
}

func postgresTableResultMappings(columns []postgresValueColumn, sources []plugin.QueryOutputSource, target []datatype.FieldInfo) []plugin.TableResultFieldMapping {
	var mappings []plugin.TableResultFieldMapping
	for i, column := range columns {
		if len(column.origins) == 0 {
			mappings = append(mappings, plugin.TableResultFieldMapping{TargetField: target[i].Name, Transformation: "generated"})
			continue
		}
		for _, origin := range column.origins {
			source := sources[origin.source]
			transformation := plugin.QueryOutputTransformationDerived
			for _, field := range source.Fields {
				if len(origin.path) == 1 && field.Name == origin.path[0] && !origin.derived && field.NativeType == target[i].NativeType {
					transformation = plugin.QueryOutputTransformationDirect
				}
			}
			mappings = append(mappings, plugin.TableResultFieldMapping{SourcePath: source.Path, SourceField: strings.Join(origin.path, "."), TargetField: target[i].Name, Transformation: transformation})
		}
	}
	return mappings
}

func freezePostgresPositionalOutputs(stmt *pgquery.SelectStmt, outputs map[*pgquery.SelectStmt]bool) {
	if stmt == nil {
		return
	}
	outputs[stmt] = true
	if stmt.GetOp() == pgquery.SetOperation_SETOP_UNION {
		freezePostgresPositionalOutputs(stmt.GetLarg(), outputs)
		freezePostgresPositionalOutputs(stmt.GetRarg(), outputs)
	}
}
