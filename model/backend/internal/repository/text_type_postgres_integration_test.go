package repository

import (
	"github.com/addp/model/internal/migration"
	"testing"
)

func TestPostgresUnifiesLogicalTextType(t *testing.T) {
	db := openStandardReferenceGuardPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE model.entity_attributes DROP CONSTRAINT ck_model_entity_attribute_type`,
		`ALTER TABLE model.entity_attributes ADD CONSTRAINT ck_model_entity_attribute_type CHECK (data_type IN ('text','string','int','bigint','float','decimal','date','datetime','bool','json','geometry'))`,
		`ALTER TABLE model.logical_fields DROP CONSTRAINT ck_model_logical_field_type`,
		`ALTER TABLE model.logical_fields ADD CONSTRAINT ck_model_logical_field_type CHECK (data_type IN ('text','string','int','bigint','float','decimal','date','datetime','bool','json','geometry'))`,
		`INSERT INTO model.entities (id,tenant_id,name,code,status,version,created_by) VALUES (987654321,987654321,'Text entity','text_migration','draft',3,1)`,
		`INSERT INTO model.entity_model_revisions (tenant_id,revision) VALUES (987654321,3) ON CONFLICT (tenant_id) DO UPDATE SET revision=3`,
		`INSERT INTO model.entity_attributes (entity_id,name,column_name,data_type) VALUES (987654321,'Description','description','text')`,
		`INSERT INTO model.dw_layers (tenant_id,layer_code,layer_name,version) VALUES (987654321,'text_migration','Text',1)`,
		`INSERT INTO model.logical_tables (id,tenant_id,name,code,table_type,layer,status,version,created_by) VALUES (987654321,987654321,'Text table','text_migration','entity','text_migration','draft',5,1)`,
		`INSERT INTO model.logical_fields (table_id,name,column_name,data_type,length,field_role) VALUES (987654321,'Description','description','text',32,'regular')`,
		`DELETE FROM model.schema_migrations WHERE version='025_unify_logical_text_type.up.sql'`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migration.Run(tx); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err := tx.Raw(`SELECT e.version=4 AND r.revision=4 AND a.data_type='string' FROM model.entities e JOIN model.entity_model_revisions r ON e.tenant_id=r.tenant_id JOIN model.entity_attributes a ON a.entity_id=e.id WHERE e.id=987654321`).Scan(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("entity type or versions were not normalized exactly once")
	}
	if err := tx.Raw(`SELECT t.version=6 AND f.data_type='string' AND f.length IS NULL FROM model.logical_tables t JOIN model.logical_fields f ON f.table_id=t.id WHERE t.id=987654321`).Scan(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("logical text physical semantics changed")
	}
	if err := tx.Exec(`UPDATE model.logical_fields SET data_type='text' WHERE table_id=987654321`).Error; err == nil {
		t.Fatal("retired type is still writable")
	}
}
