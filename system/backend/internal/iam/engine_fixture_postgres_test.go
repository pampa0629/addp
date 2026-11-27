package iam

import (
	"database/sql"
	"encoding/json"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/all"
	"gorm.io/gorm"
)

func insertEngineFixture(
	t *testing.T,
	db *gorm.DB,
	tenantID interface{},
	name string,
	engineType string,
	connectionInfo engineplugin.ConnectionInfo,
	isBuiltin bool,
) int64 {
	t.Helper()
	identityKey, err := engineplugin.BuildConnectionIdentityKey(engineType, connectionInfo)
	if err != nil {
		t.Fatalf("build %s engine identity: %v", engineType, err)
	}
	connectionJSON, err := json.Marshal(connectionInfo)
	if err != nil {
		t.Fatalf("marshal %s engine connection: %v", engineType, err)
	}
	row := struct {
		ID                                                                              int64
		TenantID                                                                        sql.NullInt64
		Name, EngineType, ConnectionInfo, IdentityKey, LifecycleState, ConnectionStatus string
		IsBuiltin                                                                       bool
	}{Name: name, EngineType: engineType, ConnectionInfo: string(connectionJSON), IdentityKey: string(identityKey),
		LifecycleState: "active", ConnectionStatus: "online", IsBuiltin: isBuiltin}
	if err := row.TenantID.Scan(tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("system.engines").Create(&row).Error; err != nil {
		t.Fatalf("insert %s engine fixture: %v", engineType, err)
	}
	if row.ID <= 0 {
		t.Fatalf("insert %s engine fixture returned invalid ID %d", engineType, row.ID)
	}
	return row.ID
}
