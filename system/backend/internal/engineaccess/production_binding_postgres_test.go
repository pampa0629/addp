package engineaccess

import (
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Invoked by the existing TestFulfillmentArbitrationAgainstPostgres fixture,
// whose standard gate owns migrations, test identity and zero-residual cleanup.
func exerciseTransferProductionBindingAgainstPostgres(t *testing.T, db *gorm.DB, tenantID int64) {
	t.Run("System PG production boundary consumes shared owner clients", func(t *testing.T) {
		tenant := uint(tenantID)
		engine := models.Engine{Name: "Production evidence fixture", EngineType: "postgresql", TenantID: &tenant,
			LifecycleState: models.EngineLifecycleActive, ConnectionInfo: models.ConnectionInfo{},
			IdentityKey: models.JSONString(`{"host":"production.invalid","database":"` + uuid.NewString() + `"}`)}
		if err := db.Table("system.engines").Create(&engine).Error; err != nil {
			t.Fatal(err)
		}
		// Internal evidence preparation must not create approval requirements,
		// accepted requests, content grants/denies, revocations or IAM audit.
		tables := []string{"engine_access_approval_requirements", "engine_access_fulfillment_outcomes", "engine_access_grants", "engine_access_denies", "engine_access_grant_revocations", "audit_logs"}
		counts := make([]int64, len(tables))
		for i, table := range tables {
			if err := db.Table("system." + table).Count(&counts[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		exerciseTransferProductionClients(t, db, tenantID, engine.ID)
		for i, table := range tables {
			var after int64
			if err := db.Table("system." + table).Count(&after).Error; err != nil || after != counts[i] {
				t.Fatalf("boundary preparation wrote %s: before=%d after=%d err=%v", table, counts[i], after, err)
			}
		}
	})
}
