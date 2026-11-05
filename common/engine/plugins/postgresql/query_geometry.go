package postgresql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/twpayne/go-geom/encoding/ewkb"
)

// openQueryDB registers only extension-owned PostGIS types on the actual
// connection. Native point, BYTEA and protocol-compatible engines retain their
// own types. No user query is executed twice to discover its output schema.
func (p *PostgreSQLPlugin) openQueryDB(connInfo plugin.ConnectionInfo) (*sql.DB, error) {
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, err
	}
	if p.identity != nil {
		return sql.Open(p.sqlDriverName(), dsn)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return stdlib.OpenDB(*config, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		rows, err := conn.Query(ctx, `SELECT t.oid, t.typname FROM pg_catalog.pg_type t
		 JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_type'::regclass AND d.objid=t.oid AND d.deptype='e'
		 JOIN pg_catalog.pg_extension e ON e.oid=d.refobjid AND d.refclassid='pg_catalog.pg_extension'::regclass
		 WHERE e.extname='postgis' AND t.typname IN ('geometry','geography')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var oid uint32
			var name string
			if err := rows.Scan(&oid, &name); err != nil {
				return err
			}
			conn.TypeMap().RegisterType(&pgtype.Type{Name: name, OID: oid, Codec: pgtype.TextCodec{}})
		}
		return rows.Err()
	})), nil
}

func (p *PostgreSQLPlugin) DecodeSQLQueryValue(field datatype.FieldInfo, value interface{}) (interface{}, error) {
	if p.identity != nil || field.Type != datatype.FieldTypeGeometry || value == nil {
		return value, nil
	}
	// Only verified PostGIS output names enter this route, never native POINT.
	if field.NativeType != "GEOMETRY" && field.NativeType != "GEOGRAPHY" {
		return value, nil
	}
	var text string
	switch raw := value.(type) {
	case []byte:
		text = string(raw)
	case string:
		text = raw
	default:
		return nil, fmt.Errorf("invalid PostGIS query geometry")
	}
	encoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("decode PostGIS query geometry")
	}
	if _, err := ewkb.Unmarshal(encoded); err != nil {
		return nil, fmt.Errorf("invalid PostGIS query EWKB")
	}
	return encoded, nil
}
