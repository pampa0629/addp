package postgresql

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestPostgreSQLDeclaresAndImplementsQueryReadSession(t *testing.T) {
	provider := &PostgreSQLPlugin{}
	if _, ok := interface{}(provider).(plugin.QueryReadSessionProvider); !ok {
		t.Fatal("PostgreSQL plugin does not implement QueryReadSessionProvider")
	}
}

func TestValidatePostgresQueryReadSessionRejectsUnsafeOrPreviewRequests(t *testing.T) {
	for _, request := range []plugin.QueryRequest{
		{Options: plugin.QueryOptions{}},
		{Options: plugin.QueryOptions{ReadOnly: true, Limit: 10}},
		{Options: plugin.QueryOptions{ReadOnly: true, Offset: 1}},
	} {
		if err := validatePostgresQueryReadSession(request); err == nil {
			t.Fatalf("request %#v was accepted", request)
		}
	}
	if err := validatePostgresQueryReadSession(plugin.QueryRequest{Options: plugin.QueryOptions{ReadOnly: true}}); err != nil {
		t.Fatal(err)
	}
}
