package repository

import (
	"fmt"
	"gorm.io/gorm"
)

// Catalog projections are captured in the owner transaction; no network or dual write.
func migrateStandardCatalogChanges(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_standard_catalog_changes_tenant_id ON standard.catalog_resource_changes (tenant_id,id)`,
		`CREATE INDEX IF NOT EXISTS idx_standard_catalog_changes_source ON standard.catalog_resource_changes (tenant_id,source_type,source_identity,id DESC)`,
		`DROP TRIGGER IF EXISTS trg_standard_metric_catalog_change ON standard.metric_definitions`,
		`DROP FUNCTION IF EXISTS standard.capture_metric_catalog_change()`,
		`CREATE OR REPLACE FUNCTION standard.catalog_resource_snapshot(kind TEXT, resource_id BIGINT) RETURNS JSONB LANGUAGE plpgsql STABLE AS $fn$
 DECLARE root_table TEXT; revision_table TEXT; parent_column TEXT; identity JSONB; revision JSONB; current_revision BOOLEAN; result JSONB;
 BEGIN
  CASE kind
   WHEN 'metric' THEN root_table:='metric_definitions';revision_table:='metric_definition_revisions';parent_column:='metric_definition_id';
   WHEN 'element' THEN root_table:='elements';revision_table:='element_revisions';parent_column:='element_id';
   WHEN 'glossary' THEN root_table:='glossaries';revision_table:='glossary_revisions';parent_column:='glossary_id';
   WHEN 'code_set' THEN root_table:='code_sets';revision_table:='code_set_revisions';parent_column:='code_set_id';
   WHEN 'document' THEN root_table:='documents';revision_table:='document_revisions';parent_column:='document_id';
   ELSE RAISE EXCEPTION 'invalid standard catalog resource type';
  END CASE;
  EXECUTE format('SELECT to_jsonb(s) FROM standard.%I s WHERE id=$1',root_table) INTO identity USING resource_id;
  IF identity IS NULL THEN RETURN NULL; END IF;
  EXECUTE format('SELECT to_jsonb(r) FROM standard.%I r WHERE %I=$1 ORDER BY (status=''published'' AND effective_from<=CURRENT_TIMESTAMP AND (effective_to IS NULL OR effective_to>CURRENT_TIMESTAMP) AND $2=''active'') DESC NULLS LAST, revision_no DESC LIMIT 1',revision_table,parent_column) INTO revision USING resource_id,identity->>'lifecycle_state';
  current_revision:=COALESCE(revision->>'status'='published' AND (revision->>'effective_from')::timestamptz<=CURRENT_TIMESTAMP AND (revision->>'effective_to' IS NULL OR (revision->>'effective_to')::timestamptz>CURRENT_TIMESTAMP) AND identity->>'lifecycle_state'='active',FALSE);
  result:=jsonb_strip_nulls(jsonb_build_object('tenant_id',identity->'tenant_id','version',identity->'version',
   'name',COALESCE(revision->>'name',identity->>'code'),'code',identity->>'code','object_kind',kind,
   'lifecycle_state',identity->>'lifecycle_state','scope_type',identity->>'scope_type','domain_id',identity->>'owner_domain_id',
   'category_id',identity->>'category_id','unit_id',revision->>'unit_id','revision_id',revision->>'id',
   'revision_no',revision->'revision_no','revision_status',revision->>'status','is_current',current_revision,
   'definition_complete',COALESCE(length(trim(revision->>'name'))>0 AND (kind='document' OR length(trim(CASE WHEN kind='code_set' THEN revision->>'description' ELSE revision->>'definition' END))>0),FALSE)));
  IF kind='metric' THEN result:=result||jsonb_strip_nulls(jsonb_build_object('metric_type',revision->>'metric_type')); END IF;
  RETURN result;
 END $fn$`,
		`CREATE OR REPLACE FUNCTION standard.capture_catalog_resource_change() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
 DECLARE changed JSONB; resource_id BIGINT; snapshot JSONB; operation TEXT:='upsert';
 BEGIN
  changed:=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
  resource_id:=(changed->>TG_ARGV[1])::BIGINT;
  snapshot:=standard.catalog_resource_snapshot(TG_ARGV[0],resource_id);
  IF TG_ARGV[1]='id' AND TG_OP='DELETE' THEN
   operation:='missing';
   SELECT c.snapshot INTO snapshot FROM standard.catalog_resource_changes c
    WHERE c.tenant_id=(changed->>'tenant_id')::BIGINT AND c.source_type=TG_ARGV[0] AND c.source_identity=resource_id ORDER BY c.id DESC LIMIT 1;
   snapshot:=COALESCE(snapshot,jsonb_build_object('name',changed->>'code'))||jsonb_strip_nulls(jsonb_build_object('tenant_id',changed->'tenant_id','version',changed->'version','code',changed->>'code','object_kind',TG_ARGV[0],'scope_type',changed->>'scope_type','domain_id',changed->>'owner_domain_id'));
  END IF;
  IF snapshot IS NOT NULL THEN
   INSERT INTO standard.catalog_resource_changes (tenant_id,source_type,source_identity,operation,resource_version,snapshot,observed_at)
    VALUES ((snapshot->>'tenant_id')::BIGINT,TG_ARGV[0],resource_id,operation,(snapshot->>'version')::BIGINT,snapshot-'tenant_id'-'version',CURRENT_TIMESTAMP);
  END IF;
  RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
 END $fn$`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate Standard catalog capture: %w", err)
		}
	}
	configurations := [][4]string{{"metric", "metric_definitions", "metric_definition_revisions", "metric_definition_id"}, {"element", "elements", "element_revisions", "element_id"}, {"glossary", "glossaries", "glossary_revisions", "glossary_id"}, {"code_set", "code_sets", "code_set_revisions", "code_set_id"}, {"document", "documents", "document_revisions", "document_id"}}
	for _, item := range configurations {
		for _, trigger := range [][2]string{{item[1], "id"}, {item[2], item[3]}} {
			if err := db.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS capture_catalog_resource ON standard.%s`, trigger[0])).Error; err != nil {
				return err
			}
			if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER capture_catalog_resource AFTER INSERT OR UPDATE OR DELETE ON standard.%s FOR EACH ROW EXECUTE FUNCTION standard.capture_catalog_resource_change('%s','%s')`, trigger[0], item[0], trigger[1])).Error; err != nil {
				return err
			}
		}
		// A single append-only replay upgrades existing Metric snapshots too, preserving all identities/cursors.
		statement := fmt.Sprintf(`INSERT INTO standard.catalog_resource_changes (tenant_id,source_type,source_identity,operation,resource_version,snapshot,observed_at)
   SELECT s.tenant_id,?,s.id,'upsert',s.version,standard.catalog_resource_snapshot(?,s.id)-'tenant_id'-'version',CURRENT_TIMESTAMP
   FROM standard.%s s WHERE NOT EXISTS (SELECT 1 FROM standard.data_migrations WHERE version=2026102201) ORDER BY s.id`, item[1])
		if err := db.Exec(statement, item[0], item[0]).Error; err != nil {
			return err
		}
	}
	return db.Exec(`INSERT INTO standard.data_migrations (version,name) VALUES (2026102201,'catalog_five_standard_resources') ON CONFLICT (version) DO NOTHING`).Error
}
