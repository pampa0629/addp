package repository

import (
	"fmt"

	"github.com/addp/catalog/internal/models"
	"gorm.io/gorm"
)

// Migrate only once, under the schema transaction and exclusive table lock.
// Existing finite history has unambiguous semantics; dates are never extended.
func migrateSharingDecisionExpiry(tx *gorm.DB) error {
	if tx.Dialector.Name() != "postgres" || !tx.Migrator().HasTable(&models.SharingDecision{}) || tx.Migrator().HasColumn(&models.SharingDecision{}, "expiry_mode") {
		return nil
	}
	return tx.Exec(`LOCK TABLE catalog.sharing_decisions IN ACCESS EXCLUSIVE MODE;
		DROP TRIGGER IF EXISTS guard_sharing_decision ON catalog.sharing_decisions;
		ALTER TABLE catalog.sharing_decisions ADD COLUMN expiry_mode varchar(32);
		UPDATE catalog.sharing_decisions SET expiry_mode = 'at_time';
		ALTER TABLE catalog.sharing_decisions ALTER COLUMN expiry_mode SET NOT NULL;
		ALTER TABLE catalog.sharing_decisions ALTER COLUMN expires_at DROP NOT NULL;`).Error
}

func applySharingDecisionConstraints(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`ALTER TABLE catalog.sharing_decisions DROP CONSTRAINT IF EXISTS fk_sharing_decision_entry`,
		`ALTER TABLE catalog.sharing_decisions ADD CONSTRAINT fk_sharing_decision_entry FOREIGN KEY (tenant_id, catalog_entry_id) REFERENCES catalog.entries(tenant_id,id)`,
		`ALTER TABLE catalog.sharing_decisions DROP CONSTRAINT IF EXISTS ck_sharing_decision_shape`,
		`ALTER TABLE catalog.sharing_decisions ADD CONSTRAINT ck_sharing_decision_shape CHECK ((
			id <> '00000000-0000-0000-0000-000000000000'::uuid AND entry_version > 0 AND engine_id > 0
			AND confirmed_by > 0 AND confirmer_membership_id > 0 AND authorization_version > 0
			AND recipient_type IN ('user','project_group') AND recipient_id > 0 AND action = 'read'
			AND self_beneficiary = (recipient_type = 'user' AND recipient_id = confirmed_by)
			AND (NOT confirmer_in_project_group OR recipient_type = 'project_group')
			AND btrim(reason) <> '' AND char_length(reason) <= 2000
			AND isfinite(created_at) AND (
				(expiry_mode = 'at_time' AND expires_at IS NOT NULL AND isfinite(expires_at) AND expires_at > created_at)
				OR (expiry_mode = 'until_revoked' AND expires_at IS NULL))
			AND jsonb_typeof(catalog_path) = 'object' AND catalog_path->>'version' = 'catalog.path/v1'
			AND catalog_path->>'engine_id' = engine_id::text AND jsonb_typeof(catalog_path->'segments') = 'array'
			AND jsonb_array_length(catalog_path->'segments') BETWEEN 3 AND 129) IS TRUE)`,
		`CREATE OR REPLACE FUNCTION catalog.guard_sharing_decision() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
		DECLARE entry catalog.entries%ROWTYPE;
		BEGIN
			IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'business sharing decision history is immutable' USING ERRCODE = '23514'; END IF;
			SELECT * INTO entry FROM catalog.entries WHERE tenant_id = NEW.tenant_id AND id = NEW.catalog_entry_id FOR UPDATE;
			IF NOT FOUND OR entry.entry_type <> 'data_item' OR entry.entry_status <> 'active' OR entry.governance_status = 'deprecated'
				OR NEW.entry_version <> entry.version + 1 OR (NEW.expiry_mode = 'at_time' AND NEW.expires_at <= clock_timestamp()) OR NEW.created_at > clock_timestamp() THEN
				RAISE EXCEPTION 'business sharing decision requires a current eligible entry' USING ERRCODE = '23514';
			END IF;
			IF NOT EXISTS (SELECT 1 FROM catalog.source_bindings source WHERE source.tenant_id = NEW.tenant_id AND source.catalog_entry_id = NEW.catalog_entry_id
				AND source.id = NEW.source_binding_id AND source.is_current AND source.source_status = 'active' AND source.source_module = 'meta' AND source.source_type = 'data_item' AND source.source_version = NEW.source_version)
				OR NOT EXISTS (SELECT 1 FROM catalog.responsibilities owner WHERE owner.tenant_id = NEW.tenant_id AND owner.catalog_entry_id = NEW.catalog_entry_id
				AND owner.id = NEW.responsibility_id AND owner.role = 'business_owner' AND owner.subject_type = 'user' AND owner.subject_id = NEW.confirmed_by AND owner.status = 'active') THEN
				RAISE EXCEPTION 'business sharing decision requires current source and business ownership' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END $$`,
		`DROP TRIGGER IF EXISTS guard_sharing_decision ON catalog.sharing_decisions`,
		`CREATE TRIGGER guard_sharing_decision BEFORE INSERT OR UPDATE OR DELETE ON catalog.sharing_decisions FOR EACH ROW EXECUTE FUNCTION catalog.guard_sharing_decision()`,
		`DROP TRIGGER IF EXISTS guard_sharing_decision_truncate ON catalog.sharing_decisions`,
		`CREATE TRIGGER guard_sharing_decision_truncate BEFORE TRUNCATE ON catalog.sharing_decisions FOR EACH STATEMENT EXECUTE FUNCTION catalog.guard_sharing_decision()`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply sharing decision constraints: %w", err)
		}
	}
	return nil
}
