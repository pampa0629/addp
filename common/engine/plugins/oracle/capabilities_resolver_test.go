package oracle

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestApplyOracleSDEWorkspaceCapabilityDoesNotDetectWeakSignatures(t *testing.T) {
	base := (&OraclePlugin{}).Capabilities()
	resolved := applyOracleSDEWorkspaceCapability(base, []oracleSDEOwnerFacts{{
		Owner:  "SDE",
		Tables: oracleSDETableSet("TABLE_REGISTRY", "STATES", "VERSIONS"),
	}}, false)

	workspace := onlyOracleSDEWorkspace(t, resolved)
	if workspace.State != plugin.SpatialWorkspaceStateNotDetected || workspace.CanEnable {
		t.Fatalf("workspace = %#v, want not_detected and not enableable", workspace)
	}
	if workspace.Evidence["required_registry_count"] != float64(1) && workspace.Evidence["required_registry_count"] != 1 {
		t.Fatalf("evidence = %#v", workspace.Evidence)
	}
	if resolved.Storage == nil || resolved.Storage.Store == nil || resolved.Storage.Store.ChangeStreamRead != nil {
		t.Fatalf("Oracle store capability changed unexpectedly: %#v", resolved.Storage)
	}
}

func TestApplyOracleSDEWorkspaceCapabilityDetectsOfficialRegistryCombination(t *testing.T) {
	base := (&OraclePlugin{}).Capabilities()
	tables := append(append([]string{}, oracleSDERequiredRegistryTables...), oracleSDEVersioningTables...)
	tables = append(tables, oracleSDEFeatureRegistryTables...)
	resolved := applyOracleSDEWorkspaceCapability(base, []oracleSDEOwnerFacts{{
		Owner:  "SDE",
		Tables: oracleSDETableSet(tables...),
	}}, false)

	workspace := onlyOracleSDEWorkspace(t, resolved)
	if workspace.State != plugin.SpatialWorkspaceStateDetected || workspace.BackendEngineType != "oracle" || workspace.CanEnable {
		t.Fatalf("workspace = %#v, want detected read-only fact", workspace)
	}
	if workspace.Evidence["repository_owner"] != "SDE" || workspace.Evidence["versioned_repository_detected"] != true {
		t.Fatalf("evidence = %#v", workspace.Evidence)
	}
	if workspace.RiskLevel != plugin.SpatialWorkspaceRiskHigh {
		t.Fatalf("risk level = %q", workspace.RiskLevel)
	}
}

func TestApplyOracleSDEWorkspaceCapabilityMarksProbePermissionDenied(t *testing.T) {
	workspace := onlyOracleSDEWorkspace(t, applyOracleSDEWorkspaceCapability((&OraclePlugin{}).Capabilities(), nil, true))
	if workspace.State != plugin.SpatialWorkspaceStatePermissionDenied || workspace.Evidence["probe_permission_denied"] != true {
		t.Fatalf("workspace = %#v, want permission_denied", workspace)
	}
}

func TestSelectOracleSDERepositoryOwnerRequiresTablesInSameOwner(t *testing.T) {
	owner, count := selectOracleSDERepositoryOwner([]oracleSDEOwnerFacts{
		{Owner: "SDE", Tables: oracleSDETableSet("TABLE_REGISTRY", "GDB_ITEMS")},
		{Owner: "BUSINESS", Tables: oracleSDETableSet("TABLE_REGISTRY", "GDB_ITEMS", "GDB_ITEMTYPES", "GEOMETRY_COLUMNS")},
	})
	if owner == nil || count != 2 || owner.Owner != "SDE" {
		t.Fatalf("owner = %#v count = %d", owner, count)
	}
}

func onlyOracleSDEWorkspace(t *testing.T, capabilities plugin.EngineCapabilities) plugin.SpatialWorkspaceFact {
	t.Helper()
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(capabilities.Extensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspaces = %#v, want one ArcGIS SDE fact", workspaces)
	}
	if workspaces[0].Ecosystem != "arcgis" || workspaces[0].Kind != plugin.SpatialWorkspaceArcGISSDE {
		t.Fatalf("workspace identity = %#v", workspaces[0])
	}
	return workspaces[0]
}

func oracleSDETableSet(names ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[name] = struct{}{}
	}
	return result
}

func TestOracleSDERequiredRegistryTablesAreStable(t *testing.T) {
	want := []string{"TABLE_REGISTRY", "GDB_ITEMS", "GDB_ITEMTYPES", "GEOMETRY_COLUMNS"}
	if !reflect.DeepEqual(oracleSDERequiredRegistryTables, want) {
		t.Fatalf("required tables = %#v, want %#v", oracleSDERequiredRegistryTables, want)
	}
}

func TestQuoteOracleIdentifierQuotesDictionaryNames(t *testing.T) {
	if got := quoteOracleIdentifier(`SDE"OWNER`); got != `"SDE""OWNER"` {
		t.Fatalf("quoted identifier = %q", got)
	}
}
