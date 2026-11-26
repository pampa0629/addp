package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// CheckDevelopFileCutover runs only on the installation connection, before
// either owner adopts the ordered target protocol. It reports execution IDs
// without modifying original journals, business receipts or authorization.
func CheckDevelopFileCutover(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, provider string) error {
	existsQuery := "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='develop' AND c.relname='file_executions')"
	journal := "to_jsonb(f)->'publication_json'"
	version := "f.intent_json->>'schema_version'"
	if provider == "mysql" {
		existsQuery = "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='develop' AND table_name='file_executions')"
		journal = "f.publication_json"
		version = "JSON_UNQUOTE(JSON_EXTRACT(f.intent_json,'$.schema_version'))"
	}
	var exists bool
	if err := db.QueryRowContext(ctx, existsQuery).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	query := fmt.Sprintf(`SELECT f.execution_id,COALESCE(e.status,''),%s FROM develop.file_executions f
		LEFT JOIN common.task_executions e ON e.execution_id=f.execution_id AND e.tenant_id=f.tenant_id
		WHERE %s='develop.file-intent/v1' AND (e.status IN ('pending','running') OR %s IS NOT NULL)
		ORDER BY f.execution_id`, journal, version, journal)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("inspect original Develop file operations: %w", err)
	}
	defer rows.Close()
	var pending []string
	for rows.Next() {
		var id, status string
		var raw []byte
		if err := rows.Scan(&id, &status, &raw); err != nil {
			return err
		}
		if status == "running" || status == "pending" {
			pending = append(pending, id+" (execution "+status+")")
			continue
		}
		type outcome struct {
			State          string
			CleanupPending bool
		}
		var original struct {
			PreparationObserved bool     `json:"preparation_observed"`
			DispatchReleasedAt  *string  `json:"dispatch_released_at"`
			Outcome             *outcome `json:"outcome"`
			Recovery            *outcome `json:"recovery_outcome"`
			Initial             *struct {
				Status string `json:"status"`
			} `json:"initial_authorization"`
		}
		if err := json.Unmarshal(raw, &original); err != nil {
			return fmt.Errorf("cannot verify original Develop execution %s: %w", id, err)
		}
		result := original.Outcome
		if original.Recovery != nil {
			result = original.Recovery
		}
		switch {
		case original.DispatchReleasedAt != nil && (result == nil || (result.State != "published" && result.State != "not_published")):
			pending = append(pending, id+" (original operation unresolved)")
		case result == nil && original.PreparationObserved:
			pending = append(pending, id+" (original preparation unresolved)")
		case result != nil && result.CleanupPending:
			pending = append(pending, id+" (original cleanup pending)")
		case (result != nil && result.State == "published" || original.Initial != nil) && (original.Initial == nil || (original.Initial.Status != "issued" && original.Initial.Status != "denied")):
			pending = append(pending, id+" (initial authorization pending)")
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("drain original Develop file executions before upgrading: %s", strings.Join(pending, ", "))
	}
	return nil
}
