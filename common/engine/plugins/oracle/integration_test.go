package oracle

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOracleCatalogAndRead(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_INTEGRATION=1 to run Oracle integration test")
	}

	connInfo := oracleIntegrationConnInfo(t)
	p := &OraclePlugin{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}

	root := plugin.CatalogRootPath(p.CatalogModel(), 92001)
	schemas, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	businessSchema := findOracleCatalogEntry(schemas, strings.ToUpper(oracleIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business")))
	if businessSchema == nil {
		t.Fatalf("business schema not found in %#v", oracleCatalogEntryNames(schemas))
	}

	items, err := p.ListChildren(ctx, connInfo, businessSchema.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(schema) error = %v", err)
	}
	orders := findOracleCatalogEntry(items, "ORDERS")
	if orders == nil || orders.Kind != plugin.CatalogKindTable {
		t.Fatalf("ORDERS table not found in %#v", oracleCatalogEntryNames(items))
	}
	if summary := findOracleCatalogEntry(items, "ORDER_SUMMARY"); summary == nil || summary.Kind != "view" {
		t.Fatalf("ORDER_SUMMARY view not found in %#v", oracleCatalogEntryNames(items))
	}

	facts, err := p.DescribeCatalogFacts(ctx, connInfo, orders.Path, plugin.CatalogFactsOptions{IncludeStatistics: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("ORDERS facts = %#v, want row_count=2", facts.Table)
	}
	assertOracleIntegrationField(t, facts.Table.Fields, "ID", datatype.FieldTypeBigInt, true)
	assertOracleIntegrationField(t, facts.Table.Fields, "AMOUNT", datatype.FieldTypeDecimal, false)
	assertOracleIntegrationField(t, facts.Table.Fields, "PAYLOAD", datatype.FieldTypeBytes, false)

	batch, err := p.ReadBatch(ctx, connInfo, orders.Path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if len(batch.Rows) != 2 || len(batch.Fields) != 7 {
		t.Fatalf("ReadBatch() rows=%d fields=%d", len(batch.Rows), len(batch.Fields))
	}
	if amount, ok := batch.Rows[0]["AMOUNT"].(string); !ok || amount == "" {
		t.Fatalf("AMOUNT = %#v, want lossless decimal string", batch.Rows[0]["AMOUNT"])
	}

	result, err := p.ExecuteSQL(ctx, connInfo,
		"SELECT ORDER_NO FROM ORDERS WHERE ID = :id",
		plugin.QueryOptions{ReadOnly: true, Limit: 1, Parameters: map[string]interface{}{"id": 1001}},
	)
	if err != nil {
		t.Fatalf("ExecuteSQL() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["ORDER_NO"] != "ORD-1001" {
		t.Fatalf("ExecuteSQL() rows = %#v", result.Rows)
	}
}

func oracleIntegrationConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	portText := oracleIntegrationEnv("ADDP_TEST_ORACLE_PORT", "ORACLE_PORT", "15210")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Oracle integration port %q: %v", portText, err)
	}
	return plugin.ConnectionInfo{
		"host":         oracleIntegrationEnv("ADDP_TEST_ORACLE_HOST", "", "127.0.0.1"),
		"port":         port,
		"service_name": oracleIntegrationEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "ORACLE_SERVICE_NAME", "FREEPDB1"),
		"user":         oracleIntegrationEnv("ADDP_TEST_ORACLE_USER", "ORACLE_APP_USER", "business"),
		"password":     oracleIntegrationEnv("ADDP_TEST_ORACLE_PASSWORD", "ORACLE_APP_PASSWORD", "business_oracle_password"),
	}
}

func oracleIntegrationEnv(primary, secondary, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	if secondary != "" {
		if value := strings.TrimSpace(os.Getenv(secondary)); value != "" {
			return value
		}
	}
	return fallback
}

func findOracleCatalogEntry(entries []plugin.CatalogEntry, name string) *plugin.CatalogEntry {
	for index := range entries {
		if strings.EqualFold(entries[index].Name, name) {
			return &entries[index]
		}
	}
	return nil
}

func oracleCatalogEntryNames(entries []plugin.CatalogEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name
	}
	return names
}

func assertOracleIntegrationField(t *testing.T, fields []datatype.FieldInfo, name string, fieldType datatype.FieldType, primaryKey bool) {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			if field.Type != fieldType || field.PrimaryKey != primaryKey {
				t.Fatalf("field %s = %#v, want type=%s primary_key=%v", name, field, fieldType, primaryKey)
			}
			return
		}
	}
	t.Fatalf("field %s not found in %#v", name, fields)
}
