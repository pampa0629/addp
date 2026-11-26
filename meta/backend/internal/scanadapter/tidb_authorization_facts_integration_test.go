package scanadapter_test

import (
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/tidb"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"github.com/addp/meta/internal/scanruntime"
	"github.com/addp/meta/internal/service"
)

func TestIntegrationTiDBAuthorizationFacts(t *testing.T) {
	if os.Getenv("ADDP_TEST_TIDB_META_FIXTURE") == "" {
		t.Skip("requires the common-tidb gate's isolated consumer fixture")
	}
	connection, err := os.ReadFile(os.Getenv("ADDP_TEST_TIDB_META_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ConnectionInfo commonmodels.ConnectionInfo `json:"connection_info"`
	}
	if err := json.Unmarshal(connection, &fixture); err != nil {
		t.Fatal("invalid isolated TiDB connection descriptor")
	}
	const engineID, tenantID uint = 1, 2
	owner := tenantID
	source := &commonmodels.Engine{
		ID: engineID, Name: "Isolated TiDB", EngineType: "tidb",
		TenantID: &owner, ConnectionInfo: fixture.ConnectionInfo,
	}
	provider := &tidb.Plugin{}
	entries, err := scanresource.RootBranchEntries(t.Context(), source, provider)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int{"tidb_fixture": 2, "tidb_scope": 2, "tidb_outside": 1, "tidb_empty": 0}
	counts := make(map[string]int, len(entries))
	for _, entry := range entries {
		if entry.LeafCount == nil {
			t.Fatalf("TiDB database %q has no table count", entry.Name)
		}
		counts[entry.Name] = *entry.LeafCount
	}
	if len(counts) != len(entries) || !maps.Equal(counts, wantCounts) {
		t.Fatalf("TiDB enumerated database table counts %v, want %v", counts, wantCounts)
	}
	t.Logf("TiDB enumerated databases %q with expected table counts", slices.Sorted(maps.Keys(counts)))
	db := metatest.OpenMetadataDB(t)
	repo := repository.NewScanRepository(db)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runtime := scanruntime.NewDatabaseRuntime(db, log, repo, nil)
	dispatcher := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log, runtime, nil, nil, nil)
	result, err := dispatcher.Dispatch(scanflow.DispatchRequest{
		Context: t.Context(), Resource: source, EnginePlugin: provider, TenantID: tenantID,
		ScanDepth: "deep", Force: true, Mode: scanflow.DispatchManual,
	})
	if err != nil || result.CatalogNodes != 4 || result.Items != 5 {
		t.Fatalf("TiDB deep scan result %+v: %v", result, err)
	}
	svc := service.NewMetadataQueryService(db)
	for _, target := range [][2]string{
		{"tidb_fixture", "addp_online_consumer_source"},
		{"tidb_fixture", "addp_online_consumer_target"},
		{"tidb_scope", "node_existing"},
		{"tidb_scope", "node_future"},
		{"tidb_outside", "node_future"},
	} {
		fullName := target[0] + "." + target[1]
		fingerprint := commonmodels.GenerateItemFingerprint(engineID, fullName)
		item, found, err := repo.FindItemByFullName(tenantID, engineID, fullName)
		if err != nil || !found || item.Fingerprint != fingerprint {
			t.Fatalf("Meta scan did not persist %s as %s: found=%t err=%v", fullName, fingerprint, found, err)
		}
		facts, err := svc.GetDataItemAuthorizationFacts(t.Context(), tenantID, fingerprint)
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
			t.Fatalf("Meta authorization facts for %s unavailable: %v", fullName, err)
		}
		path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, target[0], target[1])
		if _, err := facts.ProvePrimaryTarget(path); err != nil {
			t.Fatalf("Meta authorization facts for %s do not match its typed path: %v", fullName, err)
		}
		t.Logf("verified %s with fingerprint %s", fullName, fingerprint)
	}
}
