package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strconv"
	"time"

	commonquery "github.com/addp/common/query"
)

const postgresCreationWitnessSchema = "postgresql.table_creation/v1"

// Private, transient execution handoff. Neither an ACL identity nor a receipt
// that may be obtained later by merely inspecting a completed task's target.
type postgresCreationWitness struct {
	SchemaVersion string    `json:"schema_version"`
	EngineID      uint64    `json:"engine_id"`
	Cluster       string    `json:"cluster"`
	ServerStart   time.Time `json:"server_start"`
	Database      uint32    `json:"database"`
	Namespace     uint32    `json:"namespace"`
	Relation      uint32    `json:"relation"`
	CatalogXmin   uint32    `json:"catalog_xmin"`
	Transaction   uint64    `json:"transaction"`
	Schema        string    `json:"schema"`
	Table         string    `json:"table"`
}

func (w postgresCreationWitness) valid() bool {
	cluster, err := strconv.ParseUint(w.Cluster, 10, 64)
	return err == nil && cluster != 0 && w.SchemaVersion == postgresCreationWitnessSchema &&
		!w.ServerStart.IsZero() && w.Database != 0 && w.Namespace != 0 && w.Relation != 0 &&
		w.CatalogXmin >= 3 && w.Transaction >= 3 && w.Schema != "" && w.Table != ""
}

func decodePostgresCreationWitness(encoded string) *postgresCreationWitness {
	if len(encoded) == 0 || len(encoded) > 4096 {
		return nil
	}
	var w postgresCreationWitness
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&w) != nil || decoder.Decode(new(any)) != io.EOF || !w.valid() || uint32(w.Transaction) != w.CatalogXmin {
		return nil
	}
	return &w
}

// A row-version change is deliberately unproven even when caused by legitimate
// catalog maintenance. Never turn a 32-bit xmin/OID ABA into successful proof.
func samePostgresCreatedTable(created, current *postgresCreationWitness) bool {
	return created != nil && current != nil && created.valid() && current.valid() &&
		created.EngineID != 0 && created.EngineID == current.EngineID &&
		uint32(created.Transaction) == created.CatalogXmin &&
		current.Transaction > created.Transaction && current.Transaction-created.Transaction < uint64(1)<<31 &&
		created.Cluster == current.Cluster && created.ServerStart.Equal(current.ServerStart) &&
		created.Database == current.Database && created.Namespace == current.Namespace && created.Relation == current.Relation &&
		created.CatalogXmin == current.CatalogXmin && created.Schema == current.Schema && created.Table == current.Table
}

// Optional observation must not poison the real CREATE/COPY transaction. A
// missing pg_control_system privilege produces no evidence, not extra grants,
// a different write strategy, or a second connection used as false authority.
func observePostgresCreationWitness(ctx context.Context, tx *sql.Tx, schema, table string, lock bool) *postgresCreationWitness {
	if _, err := tx.ExecContext(ctx, "SAVEPOINT addp_creation_witness"); err != nil {
		return nil
	}
	var err error
	if lock {
		_, err = tx.ExecContext(ctx, "LOCK TABLE "+commonquery.ForDialect("postgresql").QualifiedTable(schema, table)+" IN ROW EXCLUSIVE MODE")
	}
	w := postgresCreationWitness{SchemaVersion: postgresCreationWitnessSchema, Schema: schema, Table: table}
	if err == nil {
		err = tx.QueryRowContext(ctx, `SELECT (pg_catalog.pg_control_system()).system_identifier::text,
 pg_catalog.pg_postmaster_start_time(), d.oid, n.oid, c.oid, c.xmin::text, pg_catalog.pg_current_xact_id()::text
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 CROSS JOIN pg_catalog.pg_database d
 WHERE d.datname=pg_catalog.current_database() AND n.nspname=$1 AND c.relname=$2 AND c.relkind='r'`, schema, table).
			Scan(&w.Cluster, &w.ServerStart, &w.Database, &w.Namespace, &w.Relation, &w.CatalogXmin, &w.Transaction)
	}
	if err != nil {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT addp_creation_witness")
	}
	if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT addp_creation_witness"); releaseErr != nil || err != nil || !w.valid() {
		return nil
	}
	return &w
}

func postgresCreationWitnessJSON(w *postgresCreationWitness) string {
	if w == nil || !w.valid() || uint32(w.Transaction) != w.CatalogXmin {
		return ""
	}
	encoded, err := json.Marshal(w)
	if err != nil {
		return ""
	}
	return string(encoded)
}
