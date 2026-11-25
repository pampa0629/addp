package repository

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion = 1

//go:embed tables.mysql.sql
var mysqlTables string

//go:embed foreign_keys.mysql.sql
var mysqlForeignKeys string

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQLWithData(db, "standard", SchemaVersion, prepareMySQL, func(tx *gorm.DB, _ int64) error {
			return migrateMeasurementCatalog(tx)
		})
	}
	return schema.Migrate(db, "standard", SchemaVersion, Migrate)
}

func prepareMySQL(db *gorm.DB) error {
	if err := cleanup.PrepareMySQLResultTable(db, "standard"); err != nil {
		return err
	}
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	// Circular draft pointers require both tables to exist first. MySQL DDL
	// commits individually, so a resumed installation skips installed keys.
	for _, stmt := range strings.Split(strings.TrimSpace(mysqlForeignKeys), "\n") {
		parts := strings.Fields(stmt)
		var present int
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema=DATABASE() AND table_name=? AND constraint_name=?", parts[2], parts[5]).Row().Scan(&present); err != nil {
			return err
		}
		if present == 0 {
			if err := db.Exec(stmt).Error; err != nil {
				return err
			}
		}
	}
	triggers := []string{cleanup.MySQLResultUpdateGuard}
	for _, resource := range mysqlCatalogResources {
		triggers = append(triggers, resource.triggers()...)
	}
	return schema.CheckMySQLTriggers(db, triggers)
}

type mysqlCatalogResource struct{ kind, root, revisions, owner string }

var mysqlCatalogResources = []mysqlCatalogResource{
	{"element", "elements", "element_revisions", "element_id"},
	{"metric", "metric_definitions", "metric_definition_revisions", "metric_definition_id"},
	{"code_set", "code_sets", "code_set_revisions", "code_set_id"},
	{"glossary", "glossaries", "glossary_revisions", "glossary_id"},
	{"document", "documents", "document_revisions", "document_id"},
}

// All identifiers here belong to the five module-owned tables above. Each
// change is appended inside the same transaction as the source mutation.
func (r mysqlCatalogResource) triggers() []string {
	result := []string{}
	for _, event := range []string{"INSERT", "UPDATE"} {
		body := fmt.Sprintf(`BEGIN
 IF NEW.status='published' AND EXISTS (
     SELECT 1 FROM %s v WHERE v.%s=NEW.%s AND v.id<>NEW.id AND v.status='published'
       AND (v.effective_to IS NULL OR NEW.effective_from<v.effective_to)
       AND (NEW.effective_to IS NULL OR v.effective_from<NEW.effective_to)
 ) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='published %s revision effective intervals overlap'; END IF;
END`, r.revisions, r.owner, r.owner, r.kind)
		result = append(result, mysqlStandardTrigger("interval", "BEFORE", event, r.revisions, body))
	}
	for _, table := range []string{r.root, r.revisions} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			ref := "NEW."
			if event == "DELETE" {
				ref = "OLD."
			}
			body := ""
			if table == r.root && event == "DELETE" {
				body = fmt.Sprintf(`INSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,resource_version,snapshot,observed_at)
 VALUES (OLD.tenant_id,'%[1]s',OLD.id,'missing',OLD.version,
 JSON_MERGE_PATCH(COALESCE((SELECT c.snapshot FROM catalog_resource_changes c WHERE c.tenant_id=OLD.tenant_id AND c.source_type='%[1]s' AND c.source_identity=OLD.id ORDER BY c.id DESC LIMIT 1),JSON_OBJECT('name',OLD.code)),
 JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT('code',OLD.code,'object_kind','%[1]s','scope_type',OLD.scope_type,'domain_id',CAST(OLD.owner_domain_id AS CHAR)))),CURRENT_TIMESTAMP(6))`, r.kind)
			} else {
				id := ref + "id"
				if table == r.revisions {
					id = ref + r.owner
				}
				body = r.upsertSnapshot(id)
			}
			result = append(result, mysqlStandardTrigger("catalog", "AFTER", event, table, body))
		}
	}
	return result
}

func mysqlStandardTrigger(purpose, when, event, table, body string) string {
	return fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s_%s_%s\n%s %s ON %s FOR EACH ROW\n%s", purpose, table, strings.ToLower(event), when, event, table, body)
}

func (r mysqlCatalogResource) upsertSnapshot(id string) string {
	complete := "LENGTH(TRIM(v.name))>0"
	extra := ""
	switch r.kind {
	case "code_set":
		complete += " AND LENGTH(TRIM(v.description))>0"
	case "document":
	default:
		complete += " AND LENGTH(TRIM(v.definition))>0"
	}
	if r.kind == "element" || r.kind == "metric" {
		extra += ", 'unit_id',CAST(v.unit_id AS CHAR)"
	}
	if r.kind == "metric" {
		extra += ", 'category_id',CAST(s.category_id AS CHAR), 'metric_type',v.metric_type"
	}
	current := func(alias string) string {
		return "s.lifecycle_state='active' AND " + alias + ".status='published' AND " + alias + ".effective_from<=CURRENT_TIMESTAMP(6) AND (" + alias + ".effective_to IS NULL OR " + alias + ".effective_to>CURRENT_TIMESTAMP(6))"
	}
	return fmt.Sprintf(`INSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,resource_version,snapshot,observed_at)
 SELECT s.tenant_id,'%[1]s',s.id,'upsert',s.version,
 JSON_MERGE_PATCH(JSON_OBJECT(), JSON_OBJECT(
   'name',COALESCE(v.name,s.code),'code',s.code,'object_kind','%[1]s','lifecycle_state',s.lifecycle_state,'scope_type',s.scope_type,
   'domain_id',CAST(s.owner_domain_id AS CHAR),'revision_id',CAST(v.id AS CHAR),'revision_no',v.revision_no,'revision_status',v.status,
   'is_current',JSON_EXTRACT(IF(%[2]s,'true','false'),'$'),
   'definition_complete',JSON_EXTRACT(IF(%[3]s,'true','false'),'$')%[4]s)), CURRENT_TIMESTAMP(6)
 FROM %[5]s s LEFT JOIN %[6]s v ON v.id=(
   SELECT cand.id FROM %[6]s cand WHERE cand.%[7]s=s.id ORDER BY (%[8]s) DESC,cand.revision_no DESC LIMIT 1)
 WHERE s.id=%[9]s`, r.kind, current("v"), complete, extra, r.root, r.revisions, r.owner, current("cand"), id)
}
