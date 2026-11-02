package repository

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Standard Manager T2: a protected result is persisted without its sensitive
// field, decoded from PostgreSQL and safely protected again at the read boundary.
func TestIntegrationPostgresManagerProfileProtectedResultRoundTrip(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DataProfile{}, &models.DataProfileField{}); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	state := &models.DataProfile{TenantID: 987654321, ItemFingerprint: key, EngineID: 2,
		Locator: "addp://engine/2/path/public/profile_fixture?type=table", SourceVersion: key,
		ProfileConfigHash: key, LastExecutionID: uuid.NewString(), DependencySnapshot: []byte(`{}`)}
	t.Cleanup(func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			ids := tx.Model(&models.DataProfile{}).Select("id").Where("tenant_id = ? AND item_fingerprint = ?", state.TenantID, key)
			if err := tx.Where("profile_id IN (?)", ids).Delete(&models.DataProfileField{}).Error; err != nil {
				return err
			}
			return tx.Where("tenant_id = ? AND item_fingerprint = ?", state.TenantID, key).Delete(&models.DataProfile{}).Error
		}); err != nil {
			t.Error("profile fixture cleanup failed", err)
		}
		var remaining int64
		if err := db.Model(&models.DataProfile{}).Where("tenant_id = ? AND item_fingerprint = ?", state.TenantID, key).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Error("profile fixture cleanup unconfirmed")
		}
	})
	fields := []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString}, {Name: "phone", Type: datatype.FieldTypeString}}
	component := dataprotection.Component{Key: "phone", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	rules := []dataprotection.Rule{{Action: managerprotection.ActionProfile, Component: component, Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress}}}
	raw := dataprofile.Build([]map[string]interface{}{{"name": "synthetic", "phone": "sensitive-fixture-only"}}, fields, dataprofile.BuildOptions{
		Mode: dataprofile.ModeSample, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, ProfiledAt: time.Now().UTC(),
	})
	protected, err := managerprotection.ProtectProfile(&raw, rules, fields)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDataProfileRepository(db)
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.ReplaceCurrent(t.Context(), tx, state, *protected) }); err != nil {
		t.Fatal(err)
	}
	_, stored, err := repo.GetCurrent(t.Context(), state.TenantID, key, dataprofile.ModeSample, key)
	if err != nil || stored == nil {
		t.Fatalf("read protected profile: %v", err)
	}
	payload, _ := json.Marshal(stored)
	if strings.Contains(string(payload), "sensitive-fixture-only") || stored.FieldCount != 1 || stored.Fields[0].Name != "name" {
		t.Fatal("sensitive statistics were persisted")
	}
	reread, err := managerprotection.ProtectProfile(stored, rules, fields)
	if err != nil || !reflect.DeepEqual(reread, stored) {
		t.Fatalf("read boundary rejected already-protected result: %v", err)
	}
	if _, err := managerprotection.ProtectProfile(stored, rules, fields[:1]); !errors.Is(err, managerprotection.ErrRequired) {
		t.Fatal("schema mismatch accepted")
	}
}
