package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/spatial"
)

type geometryCatalogQuerier interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

type geometryColumnDeclaration struct {
	SRID     int
	Type     string
	Declared bool
}

// Only catalog facts: never access rows of the named relation. Geometry OID
// checking prevents unrelated types' typmods from being interpreted as SRIDs.
func queryGeometryColumnDeclaration(ctx context.Context, db geometryCatalogQuerier, schema, table, column string) (*geometryColumnDeclaration, error) {
	const query = `
		SELECT postgis_typmod_srid(attr.atttypmod),
		       postgis_typmod_type(attr.atttypmod), attr.atttypmod >= 0
		FROM pg_attribute attr
		JOIN pg_class tbl ON tbl.oid = attr.attrelid
		JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
		JOIN pg_type typ ON typ.oid = attr.atttypid AND typ.typname = 'geometry'
		JOIN pg_extension ext ON ext.extname = 'postgis' AND ext.extnamespace = typ.typnamespace
		WHERE ns.nspname = $1 AND tbl.relname = $2 AND attr.attname = $3
		  AND attr.attnum > 0 AND NOT attr.attisdropped
	`
	var result geometryColumnDeclaration
	if err := db.QueryRowContext(ctx, query, schema, table, column).Scan(&result.SRID, &result.Type, &result.Declared); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query geometry type declaration: %w", err)
	}
	return &result, nil
}

// Only the explicitly requested generation task may inspect source contents.
// Untyped columns require an exhaustive dimension check, not a sample.
func queryVectorMaterializedViewSourceDimension(ctx context.Context, db geometryCatalogQuerier, schema, table, column string) (string, error) {
	declaration, err := queryGeometryColumnDeclaration(ctx, db, schema, table, column)
	if err != nil {
		return "", err
	}
	if declaration == nil {
		return "", errors.New("vector materialized view source must be a PostGIS geometry column")
	}
	if declaration.Declared {
		switch {
		case strings.HasSuffix(declaration.Type, "ZM"):
			return "GeometryZM", nil
		case strings.HasSuffix(declaration.Type, "Z"):
			return "GeometryZ", nil
		case strings.HasSuffix(declaration.Type, "M"):
			return "GeometryM", nil
		default:
			return "Geometry", nil
		}
	}
	quoted := spatial.QuotePostGISIdentifier(column)
	query := fmt.Sprintf("SELECT MIN(ST_Zmflag(%s)), MAX(ST_Zmflag(%s)) FROM %s WHERE %s IS NOT NULL",
		quoted, quoted, spatial.QualifiedPostGISTable(schema, table), quoted)
	var minimum, maximum sql.NullInt64
	if err := db.QueryRowContext(ctx, query).Scan(&minimum, &maximum); err != nil {
		return "", fmt.Errorf("verify source geometry dimensions for generation: %w", err)
	}
	if !minimum.Valid || !maximum.Valid || minimum.Int64 != maximum.Int64 {
		return "", errors.New("vector materialized view source geometry dimensions are mixed or undeclared and empty")
	}
	switch minimum.Int64 {
	case 0:
		return "Geometry", nil
	case 1:
		return "GeometryM", nil
	case 2:
		return "GeometryZ", nil
	case 3:
		return "GeometryZM", nil
	default:
		return "", errors.New("vector materialized view source geometry dimension is unsupported")
	}
}
