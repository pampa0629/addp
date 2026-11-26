package executor

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/tidb"
)

func TestIntegrationTiDBConsumerInitialWatermark(t *testing.T) {
	fixtureFile := os.Getenv("ADDP_TEST_TIDB_CONSUMER_FIXTURE")
	if fixtureFile == "" {
		t.Skip("requires the TiDB gate's disposable consumer fixture")
	}
	data, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ConnectionInfo plugin.ConnectionInfo `json:"connection_info"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal("invalid disposable TiDB consumer descriptor")
	}
	const engineID = 1
	const database = "tidb_fixture"
	plan := WatermarkIncrementalPlan{
		Source: TableSourcePlan{
			ConnInfo: fixture.ConnectionInfo,
			Path:     plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_online_consumer_source"),
		},
		Target: TableTargetPlan{
			ConnInfo: fixture.ConnectionInfo,
			Path:     plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_online_consumer_target"),
		},
		WatermarkField: "updated_at",
		TieBreakers:    []string{"id"},
		TargetKeys:     []string{"id"},
		BatchSize:      100,
	}
	executor, err := NewWatermarkIncrementalExecutor("tidb", "tidb")
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(t.Context(), plan)
	if err != nil {
		t.Fatalf("TiDB native Transfer watermark failed with metrics %+v: %v", result, err)
	}
	if result.RecordsRead != 5 || result.RecordsWritten != 5 {
		t.Fatalf("first batch read=%d written=%d, want 5/5", result.RecordsRead, result.RecordsWritten)
	}
}
