package analytical

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

func TestValidateMySQLCompatibleExecutionRequiresTransactionAndHooks(t *testing.T) {
	err := ValidateMySQLCompatibleExecution(context.Background(), nil, nil, MySQLCompatibleExecutionOptions{})
	if !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatalf("nil transaction/options error = %v", err)
	}
	err = ValidateMySQLCompatibleExecution(context.Background(), nil, nil, MySQLCompatibleExecutionOptions{
		Certify: func(context.Context, sqlcompile.InstanceProbeSession) (plugin.SupportReport, error) {
			return plugin.SupportReport{Supported: true}, nil
		},
		LoadFields: func(context.Context, *sql.Tx, string, string) ([]datatype.FieldInfo, error) { return nil, nil },
	})
	if !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatalf("nil transaction with hooks error = %v", err)
	}
}

func TestMySQLCompatibleScanRequiresSystemSchemaPolicy(t *testing.T) {
	path := plugin.EngineCatalogBranchLeafPath(
		plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase),
		1,
		plugin.EngineCatalogTermDatabase,
		"business",
		plugin.EngineCatalogTermTable,
		plugin.EngineCatalogKindTable,
		"orders",
	)
	_, err := (MySQLCompatibleScanDialect{}).Table(plugin.SourceBinding{Source: "orders", Path: path})
	if !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatalf("missing system schema policy error = %v", err)
	}
}
