package shared

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/xwb1989/sqlparser"
)

// MySQLCompatibleQueryProvenance provides fail-closed query source and output
// lineage resolution for engines that share MySQL SELECT and information_schema
// semantics. Engine identity remains owned by the calling plugin.
type MySQLCompatibleQueryProvenance struct {
	EngineName        string
	DefaultPort       int
	CatalogModel      plugin.EngineCatalogModelSpec
	BuildDSN          func(plugin.ConnectionInfo) (string, error)
	IsSystemNamespace func(string) bool
	DescribeFacts     func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error)
}

type mysqlCompatibleRelationReference struct {
	Database string
	Name     string
}

type mysqlCompatibleResolvedRelation struct {
	Database  string
	Name      string
	TableType string
	Engine    string
}

type mysqlCompatibleReadCatalog interface {
	ResolveRelation(context.Context, mysqlCompatibleRelationReference) (mysqlCompatibleResolvedRelation, error)
}

type mysqlCompatibleDatabaseReadCatalog struct {
	db *sql.DB
}

// ResolveReadSet resolves every base table read by a supported SELECT.
func (p MySQLCompatibleQueryProvenance) ResolveReadSet(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.QueryRequest,
) (*plugin.QueryReadSet, error) {
	if !req.Options.ReadOnly || req.EngineID == 0 || !strings.EqualFold(strings.TrimSpace(req.Language), "sql") {
		return nil, p.readSetError("query must be a read-only SQL query with an engine")
	}
	defaultDatabase := strings.TrimSpace(plugin.ParseDriverConnInfo(connInfo, p.DefaultPort, "").Database)
	if defaultDatabase == "" {
		return nil, p.readSetError("current database is required")
	}
	references, err := p.inspectReadReferences(req.Query)
	if err != nil {
		return nil, err
	}
	if p.BuildDSN == nil {
		return nil, p.readSetError("DSN builder is required")
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: build %s catalog connection: %v", plugin.ErrQueryReadSetUnresolved, p.engineName(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: open %s catalog connection: %v", plugin.ErrQueryReadSetUnresolved, p.engineName(), err)
	}
	defer db.Close() //nolint:errcheck

	return p.resolveReadSet(ctx, req, defaultDatabase, references, &mysqlCompatibleDatabaseReadCatalog{db: db})
}

func (p MySQLCompatibleQueryProvenance) inspectReadReferences(query string) ([]mysqlCompatibleRelationReference, error) {
	query = strings.TrimSpace(query)
	if strings.Contains(query, "/*!") {
		return nil, p.readSetError("executable comments are not supported")
	}
	statement, err := sqlparser.Parse(query)
	if err != nil {
		return nil, p.readSetError("query must contain exactly one supported SELECT")
	}
	if _, ok := statement.(sqlparser.SelectStatement); !ok {
		return nil, p.readSetError("query must contain exactly one SELECT")
	}

	references := make([]mysqlCompatibleRelationReference, 0)
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		switch typed := node.(type) {
		case *sqlparser.Select:
			if strings.TrimSpace(typed.Lock) != "" {
				return false, p.readSetError("row locking is not a read-only query source")
			}
		case *sqlparser.FuncExpr:
			return false, p.readSetError("function calls are not supported")
		case *sqlparser.AliasedTableExpr:
			relation, ok := typed.Expr.(sqlparser.TableName)
			if !ok {
				return true, nil
			}
			name := strings.TrimSpace(relation.Name.String())
			if name == "" {
				return false, p.readSetError("relation name is unresolved")
			}
			references = append(references, mysqlCompatibleRelationReference{
				Database: strings.TrimSpace(relation.Qualifier.String()),
				Name:     name,
			})
		}
		return true, nil
	}, statement)
	if err != nil {
		return nil, err
	}
	return references, nil
}

func (p MySQLCompatibleQueryProvenance) resolveReadSet(
	ctx context.Context,
	req plugin.QueryRequest,
	defaultDatabase string,
	references []mysqlCompatibleRelationReference,
	catalog mysqlCompatibleReadCatalog,
) (*plugin.QueryReadSet, error) {
	defaultDatabase = strings.TrimSpace(defaultDatabase)
	if !req.Options.ReadOnly || req.EngineID == 0 || defaultDatabase == "" || catalog == nil || p.IsSystemNamespace == nil {
		return nil, p.readSetError("query context is incomplete")
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(references))
	for _, reference := range references {
		reference.Database = strings.TrimSpace(reference.Database)
		reference.Name = strings.TrimSpace(reference.Name)
		if reference.Database == "" {
			reference.Database = defaultDatabase
		}
		if reference.Name == "" || p.IsSystemNamespace(reference.Database) {
			return nil, p.readSetError("relation is not a business base table")
		}
		resolved, err := catalog.ResolveRelation(ctx, reference)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve %s relation: %v", plugin.ErrQueryReadSetUnresolved, p.engineName(), err)
		}
		if strings.TrimSpace(resolved.Database) == "" || strings.TrimSpace(resolved.Name) == "" ||
			!strings.EqualFold(strings.TrimSpace(resolved.TableType), "BASE TABLE") ||
			!strings.EqualFold(strings.TrimSpace(resolved.Engine), "InnoDB") || p.IsSystemNamespace(resolved.Database) {
			return nil, p.readSetError("relation is not a business base table")
		}
		paths = append(paths, plugin.EngineCatalogBranchLeafPath(
			p.CatalogModel, req.EngineID,
			plugin.EngineCatalogTermDatabase, resolved.Database,
			plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, resolved.Name,
		))
	}
	readSet, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, err
	}
	if err := plugin.ValidateQueryReadSet(req, readSet); err != nil {
		return nil, err
	}
	return readSet, nil
}

func (c *mysqlCompatibleDatabaseReadCatalog) ResolveRelation(ctx context.Context, reference mysqlCompatibleRelationReference) (mysqlCompatibleResolvedRelation, error) {
	if c == nil || c.db == nil {
		return mysqlCompatibleResolvedRelation{}, fmt.Errorf("catalog connection is required")
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_schema, table_name, table_type, COALESCE(engine, '')
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ?
		  AND table_type IN ('BASE TABLE', 'VIEW')
	`, reference.Database, reference.Name)
	if err != nil {
		return mysqlCompatibleResolvedRelation{}, err
	}
	defer rows.Close()

	var matches []mysqlCompatibleResolvedRelation
	for rows.Next() {
		var relation mysqlCompatibleResolvedRelation
		if err := rows.Scan(&relation.Database, &relation.Name, &relation.TableType, &relation.Engine); err != nil {
			return mysqlCompatibleResolvedRelation{}, err
		}
		matches = append(matches, relation)
	}
	if err := rows.Err(); err != nil {
		return mysqlCompatibleResolvedRelation{}, err
	}
	if len(matches) != 1 {
		return mysqlCompatibleResolvedRelation{}, fmt.Errorf("relation resolution returned %d matches", len(matches))
	}
	return matches[0], nil
}

func (p MySQLCompatibleQueryProvenance) engineName() string {
	if name := strings.TrimSpace(p.EngineName); name != "" {
		return name
	}
	return "MySQL-compatible engine"
}

func (p MySQLCompatibleQueryProvenance) readSetError(reason string) error {
	return fmt.Errorf("%w: %s %s", plugin.ErrQueryReadSetUnresolved, p.engineName(), reason)
}
