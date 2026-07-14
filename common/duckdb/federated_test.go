package duckdb

import (
	"context"
	"testing"

	commonModels "github.com/addp/common/models"
)

type noCallSystemClient struct {
	called bool
}

func (c *noCallSystemClient) ListEngines(string, uint) ([]commonModels.Engine, error) {
	c.called = true
	return nil, nil
}

func TestPrepareFederatedQueryWithObjectTablesDoesNotRequireMetaForLocalSQL(t *testing.T) {
	t.Parallel()

	systemClient := &noCallSystemClient{}
	session, err := PrepareFederatedQueryWithObjectTables(context.Background(), 7, "SELECT 1", systemClient, nil)
	if err != nil {
		t.Fatalf("PrepareFederatedQueryWithObjectTables() error = %v", err)
	}
	defer session.Close()
	if systemClient.called {
		t.Fatal("local SQL without engine references must not query System")
	}
	var value int
	if err := session.Conn.QueryRowContext(context.Background(), session.RewrittenSQL).Scan(&value); err != nil {
		t.Fatalf("execute rewritten SQL: %v", err)
	}
	if value != 1 {
		t.Fatalf("value = %d, want 1", value)
	}
}
