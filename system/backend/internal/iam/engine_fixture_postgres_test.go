package iam

import (
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
	var engineID int64
	if err := db.Raw(`
		INSERT INTO system.engines (
			tenant_id, name, engine_type, connection_info, identity_key,
			lifecycle_state, connection_status, is_builtin
		) VALUES (?, ?, ?, CAST(? AS jsonb), CAST(? AS jsonb), 'active', 'online', ?)
		RETURNING id
	`, tenantID, name, engineType, string(connectionJSON), identityKey, isBuiltin).Scan(&engineID).Error; err != nil {
		t.Fatalf("insert %s engine fixture: %v", engineType, err)
	}
	if engineID <= 0 {
		t.Fatalf("insert %s engine fixture returned invalid ID %d", engineType, engineID)
	}
	return engineID
}
