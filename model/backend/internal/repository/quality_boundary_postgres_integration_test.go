package repository

import (
	"testing"

	"github.com/addp/model/internal/migration"
)

func TestPostgresRemovesDWLayerQualitySLA(t *testing.T) {
	db := openStandardReferenceGuardPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE model.dw_layers ADD COLUMN quality_sla JSONB`,
		`INSERT INTO model.dw_layers (tenant_id,layer_code,layer_name,version,quality_sla) VALUES (987654321,'quality_boundary','Quality boundary',3,'{"pass_rate":99}')`,
		`DELETE FROM model.schema_migrations WHERE version='023_remove_dw_layer_quality_sla.up.sql'`,
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
	var columns, version int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='model' AND table_name='dw_layers' AND column_name='quality_sla'`).Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Raw(`SELECT version FROM model.dw_layers WHERE tenant_id=987654321 AND layer_code='quality_boundary'`).Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if columns != 0 || version != 4 {
		t.Fatalf("columns=%d version=%d", columns, version)
	}
}
