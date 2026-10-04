package repository

import (
	"fmt"

	"gorm.io/gorm"
)

// Owner-schema guards cover all writers, including sync and responsibility
// reconciliation. There is no network IO, global lock or Ready dependency.
// Acceptance reconciliation releases basis protection independently of issuance
// recovery; neither local marker is System's outcome or Grant authority.
func applyFulfillmentConstraints(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_entry_tenant_id ON catalog.entries (tenant_id, id)`,
		`ALTER TABLE catalog.fulfillment_checks DROP CONSTRAINT IF EXISTS fk_catalog_fulfillment_entry`,
		`ALTER TABLE catalog.fulfillment_checks ADD CONSTRAINT fk_catalog_fulfillment_entry
			FOREIGN KEY (tenant_id, catalog_entry_id) REFERENCES catalog.entries(tenant_id, id)`,
		`ALTER TABLE catalog.fulfillment_checks DROP CONSTRAINT IF EXISTS ck_catalog_fulfillment_shape`,
		`ALTER TABLE catalog.fulfillment_checks ADD CONSTRAINT ck_catalog_fulfillment_shape CHECK (
			request_id <> '00000000-0000-0000-0000-000000000000'::uuid
			AND jsonb_typeof(request_binding) = 'object' AND request_binding <> '{}'::jsonb
			AND isfinite(created_at) AND (resolved_at IS NULL OR (isfinite(resolved_at) AND resolved_at >= created_at))
			AND (grant_reconciled_at IS NULL OR (resolved_at IS NOT NULL AND isfinite(grant_reconciled_at)
				AND grant_reconciled_at >= resolved_at)))`,
		`CREATE INDEX IF NOT EXISTS ix_catalog_fulfillment_pending ON catalog.fulfillment_checks
			(tenant_id, catalog_entry_id) WHERE resolved_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS ix_catalog_fulfillment_grant_pending ON catalog.fulfillment_checks
			(tenant_id, created_at, request_id) WHERE grant_reconciled_at IS NULL`,
		`CREATE OR REPLACE FUNCTION catalog.guard_fulfillment_check() RETURNS trigger
		LANGUAGE plpgsql SET search_path = pg_catalog AS $$
		DECLARE entry catalog.entries%ROWTYPE;
		BEGIN
			IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
				RAISE EXCEPTION 'Catalog fulfillment check history is immutable' USING ERRCODE = '23514';
			END IF;
			IF TG_OP = 'UPDATE' AND (
				(to_jsonb(NEW) - 'resolved_at' - 'grant_reconciled_at') IS DISTINCT FROM
					(to_jsonb(OLD) - 'resolved_at' - 'grant_reconciled_at')
				OR (NEW.resolved_at IS DISTINCT FROM OLD.resolved_at AND (OLD.resolved_at IS NOT NULL OR NEW.resolved_at IS NULL))
				OR (NEW.grant_reconciled_at IS DISTINCT FROM OLD.grant_reconciled_at AND
					(OLD.grant_reconciled_at IS NOT NULL OR NEW.grant_reconciled_at IS NULL OR OLD.resolved_at IS NULL))
				OR (NEW.resolved_at IS NOT DISTINCT FROM OLD.resolved_at AND NEW.grant_reconciled_at IS NOT DISTINCT FROM OLD.grant_reconciled_at)) THEN
				RAISE EXCEPTION 'Catalog fulfillment check binding is immutable' USING ERRCODE = '23514';
			END IF;
			SELECT * INTO entry FROM catalog.entries
				WHERE tenant_id = NEW.tenant_id AND id = NEW.catalog_entry_id FOR UPDATE;
			IF NOT FOUND THEN
				RAISE EXCEPTION 'Catalog fulfillment check entry is missing' USING ERRCODE = '23514';
			END IF;
			IF TG_OP = 'INSERT' AND (NEW.resolved_at IS NOT NULL OR NEW.grant_reconciled_at IS NOT NULL OR entry.entry_status <> 'active'
				OR entry.governance_status = 'deprecated' OR entry.entry_type <> 'data_item'
				OR NOT EXISTS (SELECT 1 FROM catalog.source_bindings WHERE tenant_id = NEW.tenant_id
					AND catalog_entry_id = entry.id AND is_current AND source_status = 'active' AND source_module = 'meta')) THEN
				RAISE EXCEPTION 'Catalog fulfillment check requires a current non-deprecated data item' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END $$`,
		`DROP TRIGGER IF EXISTS guard_fulfillment_check ON catalog.fulfillment_checks`,
		`CREATE TRIGGER guard_fulfillment_check BEFORE INSERT OR UPDATE OR DELETE ON catalog.fulfillment_checks
			FOR EACH ROW EXECUTE FUNCTION catalog.guard_fulfillment_check()`,
		`DROP TRIGGER IF EXISTS guard_fulfillment_check_truncate ON catalog.fulfillment_checks`,
		`CREATE TRIGGER guard_fulfillment_check_truncate BEFORE TRUNCATE ON catalog.fulfillment_checks
			FOR EACH STATEMENT EXECUTE FUNCTION catalog.guard_fulfillment_check()`,
		`CREATE OR REPLACE FUNCTION catalog.guard_pending_fulfillment_basis() RETURNS trigger
		LANGUAGE plpgsql SET search_path = pg_catalog AS $$
		DECLARE tenant bigint; entry_id uuid; old_tenant bigint; old_entry_id uuid;
		BEGIN
			IF TG_TABLE_NAME = 'entries' THEN
				IF TG_OP = 'UPDATE' AND (OLD.tenant_id, OLD.id, OLD.entry_type, OLD.entry_status, OLD.governance_status, OLD.merged_into_entry_id)
					IS NOT DISTINCT FROM (NEW.tenant_id, NEW.id, NEW.entry_type, NEW.entry_status, NEW.governance_status, NEW.merged_into_entry_id) THEN
					RETURN NEW;
				END IF;
				tenant := OLD.tenant_id; entry_id := OLD.id;
			ELSE
				IF TG_OP = 'UPDATE' THEN
					IF TG_TABLE_NAME = 'responsibilities' THEN
						IF (OLD.tenant_id, OLD.catalog_entry_id, OLD.role, OLD.subject_type, OLD.subject_id, OLD.status)
							IS NOT DISTINCT FROM (NEW.tenant_id, NEW.catalog_entry_id, NEW.role, NEW.subject_type, NEW.subject_id, NEW.status) THEN
							RETURN NEW;
						END IF;
					END IF;
					IF TG_TABLE_NAME = 'source_bindings' THEN
						IF (OLD.tenant_id, OLD.catalog_entry_id, OLD.source_module, OLD.source_type, OLD.source_identity, OLD.source_status, OLD.is_current, OLD.source_version, OLD.observed_snapshot)
							IS NOT DISTINCT FROM (NEW.tenant_id, NEW.catalog_entry_id, NEW.source_module, NEW.source_type, NEW.source_identity, NEW.source_status, NEW.is_current, NEW.source_version, NEW.observed_snapshot) THEN
							RETURN NEW;
						END IF;
					END IF;
				END IF;
				IF TG_OP <> 'DELETE' THEN tenant := NEW.tenant_id; entry_id := NEW.catalog_entry_id; END IF;
				IF TG_OP <> 'INSERT' THEN old_tenant := OLD.tenant_id; old_entry_id := OLD.catalog_entry_id; END IF;
			END IF;
			-- Stable entry-first ordering also protects against direct owner writes
			-- moving a row away from the original aggregate.
			PERFORM 1 FROM catalog.entries WHERE (tenant_id = tenant AND id = entry_id)
				OR (tenant_id = old_tenant AND id = old_entry_id) ORDER BY id FOR UPDATE;
			IF EXISTS (SELECT 1 FROM catalog.fulfillment_checks WHERE resolved_at IS NULL
				AND ((tenant_id = tenant AND catalog_entry_id = entry_id)
					OR (tenant_id = old_tenant AND catalog_entry_id = old_entry_id))) THEN
				RAISE EXCEPTION 'Catalog authorization fulfillment must be reconciled before changing its basis'
					USING ERRCODE = '23514', CONSTRAINT = 'catalog_fulfillment_unresolved';
			END IF;
			IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
			RETURN NEW;
		END $$`,
	}
	for _, table := range []string{"entries", "responsibilities", "source_bindings"} {
		events := "INSERT OR UPDATE OR DELETE"
		if table == "entries" {
			events = "UPDATE OR DELETE"
		}
		statements = append(statements,
			fmt.Sprintf(`DROP TRIGGER IF EXISTS guard_pending_fulfillment_basis ON catalog.%s`, table),
			fmt.Sprintf(`CREATE TRIGGER guard_pending_fulfillment_basis BEFORE %s ON catalog.%s
				FOR EACH ROW EXECUTE FUNCTION catalog.guard_pending_fulfillment_basis()`, events, table))
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply Catalog fulfillment constraints: %w", err)
		}
	}
	return nil
}
