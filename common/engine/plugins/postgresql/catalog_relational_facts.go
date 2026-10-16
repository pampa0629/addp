package postgresql

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/addp/common/engine/plugin"
	"gorm.io/gorm"
)

// Relational callbacks are certified by the owning plugin, not inherited merely
// because another engine speaks the PostgreSQL wire protocol.
func (p *PostgreSQLPlugin) relationalFactsCapability() plugin.EngineCatalogFactsCapability {
	if p.identity != nil {
		if p.identity.CatalogFacts != nil {
			return *p.identity.CatalogFacts
		}
		return plugin.EngineCatalogFactsCapability{}
	}
	return plugin.EngineCatalogFactsCapability{Indexes: true, Constraints: true, Partitioning: true}
}

func (p *PostgreSQLPlugin) listIndexes(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.IndexFacts, error) {
	var rows []struct {
		Name       string
		FieldsJSON string
		IsUnique   bool
		IndexType  string
	}
	err := db.WithContext(ctx).Raw(`SELECT ix.relname AS name, am.amname AS index_type,
		i.indisunique AS is_unique,
		COALESCE((SELECT json_agg(a.attname ORDER BY k.ord)
		 FROM unnest(i.indkey::smallint[]) WITH ORDINALITY k(attnum,ord)
		 JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum
		 WHERE k.ord<=i.indnkeyatts), '[]'::json)::text AS fields_json
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class t ON t.oid=i.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
		JOIN pg_catalog.pg_class ix ON ix.oid=i.indexrelid
		JOIN pg_catalog.pg_am am ON am.oid=ix.relam
		WHERE n.nspname=? AND t.relname=? AND i.indisvalid AND i.indisready
		 AND i.indexprs IS NULL AND i.indpred IS NULL AND i.indnkeyatts>0
		ORDER BY ix.relname`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]plugin.IndexFacts, 0, len(rows))
	for _, row := range rows {
		fields, err := postgresCatalogFields(row.FieldsJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, plugin.IndexFacts{Name: row.Name, Fields: fields, IsUnique: row.IsUnique, IndexType: row.IndexType})
	}
	return result, nil
}

func (p *PostgreSQLPlugin) listConstraints(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ConstraintFacts, error) {
	var rows []struct {
		Name                 string
		ConstraintType       string
		FieldsJSON           string
		ReferencedNamespace  string
		ReferencedTable      string
		ReferencedFieldsJSON string
	}
	err := db.WithContext(ctx).Raw(`SELECT c.conname AS name,
		CASE c.contype WHEN 'p' THEN 'primary_key' WHEN 'u' THEN 'unique' WHEN 'f' THEN 'foreign_key' END AS constraint_type,
		COALESCE((SELECT json_agg(a.attname ORDER BY k.ord)
		 FROM unnest(c.conkey) WITH ORDINALITY k(attnum,ord)
		 JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum), '[]'::json)::text AS fields_json,
		COALESCE(rn.nspname,'') AS referenced_namespace, COALESCE(rt.relname,'') AS referenced_table,
		COALESCE((SELECT json_agg(a.attname ORDER BY k.ord)
		 FROM unnest(c.confkey) WITH ORDINALITY k(attnum,ord)
		 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum), '[]'::json)::text AS referenced_fields_json
		FROM pg_catalog.pg_constraint c
		JOIN pg_catalog.pg_class t ON t.oid=c.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
		LEFT JOIN pg_catalog.pg_class rt ON rt.oid=c.confrelid
		LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid=rt.relnamespace
		WHERE n.nspname=? AND t.relname=? AND c.contype IN ('p','u','f')
		ORDER BY c.conname`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]plugin.ConstraintFacts, 0, len(rows))
	for _, row := range rows {
		fields, err := postgresCatalogFields(row.FieldsJSON)
		if err != nil {
			return nil, err
		}
		references, err := postgresCatalogFields(row.ReferencedFieldsJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, plugin.ConstraintFacts{Name: row.Name, ConstraintType: row.ConstraintType, Fields: fields,
			ReferencedNamespace: row.ReferencedNamespace, ReferencedTable: row.ReferencedTable, ReferencedFields: references})
	}
	return result, nil
}

func (p *PostgreSQLPlugin) describePartitioning(ctx context.Context, db *gorm.DB, schema, table string) (*plugin.TablePartitioningFacts, error) {
	var rows []struct {
		IsRoot         bool
		Strategy       string
		ExpressionKey  bool
		FieldsJSON     string
		PartitionCount int
	}
	err := db.WithContext(ctx).Raw(`WITH target AS (
		SELECT t.oid FROM pg_catalog.pg_class t JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
		WHERE n.nspname=? AND t.relname=?
	) SELECT t.oid=target.oid AS is_root, p.partstrat::text AS strategy,
		p.partexprs IS NOT NULL AS expression_key,
		COALESCE((SELECT json_agg(a.attname ORDER BY k.ord)
		 FROM unnest(p.partattrs::smallint[]) WITH ORDINALITY k(attnum,ord)
		 JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum), '[]'::json)::text AS fields_json,
		(SELECT count(*) FROM pg_catalog.pg_inherits i WHERE i.inhparent=t.oid) AS partition_count
		FROM target JOIN pg_catalog.pg_class t ON t.oid=target.oid OR t.oid IN (
		 SELECT i.inhrelid FROM pg_catalog.pg_inherits i WHERE i.inhparent=target.oid)
		JOIN pg_catalog.pg_partitioned_table p ON p.partrelid=t.oid
		ORDER BY is_root DESC,t.oid`, schema, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || !rows[0].IsRoot || rows[0].ExpressionKey {
		return nil, nil
	}
	root := rows[0]
	keys, err := postgresCatalogFields(root.FieldsJSON)
	if err != nil {
		return nil, err
	}
	facts := &plugin.TablePartitioningFacts{Strategy: postgresPartitionStrategy(root.Strategy), KeyFields: keys, PartitionCount: root.PartitionCount}
	if root.PartitionCount > 0 && len(rows)-1 == root.PartitionCount {
		var subKeys []string
		var strategy string
		for i, sub := range rows[1:] {
			if sub.ExpressionKey {
				return facts, nil
			}
			keys, err := postgresCatalogFields(sub.FieldsJSON)
			if err != nil {
				return nil, err
			}
			if i == 0 {
				subKeys, strategy = keys, postgresPartitionStrategy(sub.Strategy)
			} else if strategy != postgresPartitionStrategy(sub.Strategy) || !slices.Equal(subKeys, keys) {
				return facts, nil
			}
		}
		facts.SubpartitionStrategy = strategy
		facts.SubpartitionKeyFields = subKeys
	}
	return facts, nil
}

func postgresCatalogFields(raw string) ([]string, error) {
	var fields []string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("decode PostgreSQL catalog fields: %w", err)
	}
	return fields, nil
}

func postgresPartitionStrategy(value string) string {
	switch value {
	case "r":
		return "range"
	case "l":
		return "list"
	case "h":
		return "hash"
	}
	return ""
}
