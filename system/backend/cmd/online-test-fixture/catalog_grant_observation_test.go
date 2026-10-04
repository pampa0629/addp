package main

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCatalogObservationRequiresDedicatedScopeBeforeConnecting(t *testing.T) {
	id := uuid.New().String()
	baseline := []string{"ADDP_ONLINE_HOST=1", "ADDP_ONLINE_TEST=1", "POSTGRES_DB=addp_online",
		"POSTGRES_HOST=127.0.0.1", "POSTGRES_PORT=5432", "POSTGRES_USER=online",
		"ADDP_ONLINE_TEST_TENANT_ID=42", "ADDP_ONLINE_TEST_ENGINE_ID=7"}
	if _, _, _, err := catalogObservationScope(id, baseline); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"ADDP_ONLINE_HOST=0", "ADDP_ONLINE_TEST=0", "POSTGRES_DB=addp", "POSTGRES_DB=addp_test",
		"POSTGRES_DB=addp_iam_test", "POSTGRES_HOST=remote.example", "POSTGRES_PORT=0", "POSTGRES_PORT=65536",
		"POSTGRES_USER=", "ADDP_ONLINE_TEST_TENANT_ID=1", "ADDP_ONLINE_TEST_ENGINE_ID=0"} {
		t.Run(override, func(t *testing.T) {
			if _, _, _, err := catalogObservationScope(id, append(append([]string{}, baseline...), override)); err == nil {
				t.Fatal("unsafe environment accepted")
			}
		})
	}
	for _, invalid := range []string{"", uuid.Nil.String(), "wrong", "{" + id + "}"} {
		if _, _, _, err := catalogObservationScope(invalid, baseline); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if err := run([]string{"--suite", "other", "--observe-catalog-grant", id}, baseline); err == nil {
		t.Fatal("wrong suite accepted")
	}
	if err := run([]string{"--suite", "enterprise-catalog-publishing", "--observe-catalog-grant", id, "--output", "/tmp/unused"}, baseline); err == nil {
		t.Fatal("observation was allowed to enter identity initialization")
	}
}

func TestCatalogObservationCountsOnlySystemIssuanceFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { connection.Close() })
	for _, query := range []string{
		"ATTACH DATABASE ':memory:' AS system",
		"CREATE TABLE system.engine_access_fulfillment_outcomes (request_id TEXT, tenant_id INTEGER, engine_id INTEGER, outcome TEXT)",
		"CREATE TABLE system.engine_access_grants (request_id TEXT, granted_at TEXT)",
		"CREATE TABLE system.audit_logs (tenant_id INTEGER, entity_id TEXT, module_name TEXT, entity_type TEXT, event_name TEXT, result TEXT)",
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	id := uuid.New()
	if err := db.Exec("INSERT INTO system.engine_access_fulfillment_outcomes VALUES (?,42,7,'accepted')", id).Error; err != nil {
		t.Fatal(err)
	}
	missing, err := readCatalogGrantObservation(db, 42, 7, id)
	if err != nil || missing.ReceiptCount != 1 || missing.GrantCount != 0 || missing.IssuanceAuditCount != 0 || missing.GrantedAt != "" {
		t.Fatalf("missing issuance: %#v %v", missing, err)
	}
	if err := db.Exec("INSERT INTO system.engine_access_grants VALUES (?, '2026-10-04 10:00:00+00')", id).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		tenant        int
		event, result string
	}{
		{42, "system.engine_access_grant.issued", "succeeded"},
		{43, "system.engine_access_grant.issued", "succeeded"},
		{42, "system.engine_access_grant.issued", "failed"},
		{42, "system.engine_access_grant.revoked", "succeeded"},
	} {
		if err := db.Exec("INSERT INTO system.audit_logs VALUES (?,?,'system','engine_access_grant',?,?)", row.tenant, id.String(), row.event, row.result).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range [][2]int64{{42, 7}, {43, 7}, {42, 8}} {
		result, err := readCatalogGrantObservation(db, scope[0], scope[1], id)
		if err != nil {
			t.Fatal(err)
		}
		if scope == [2]int64{42, 7} {
			if result.GrantCount != 1 || result.IssuanceAuditCount != 1 || result.RequestID != id.String() || result.GrantedAt == "" {
				t.Fatalf("wrong issuance observation: %#v", result)
			}
		} else if result.ReceiptCount != 0 || result.GrantCount != 0 || result.IssuanceAuditCount != 0 {
			t.Fatal("cross-scope issuance exposed")
		}
	}
	if err := db.Exec("INSERT INTO system.audit_logs VALUES (42,?,'system','engine_access_grant','system.engine_access_grant.issued','succeeded')", id.String()).Error; err != nil {
		t.Fatal(err)
	}
	duplicate, err := readCatalogGrantObservation(db, 42, 7, id)
	if err != nil || duplicate.IssuanceAuditCount != 2 {
		t.Fatal("duplicate audit hidden")
	}
}
