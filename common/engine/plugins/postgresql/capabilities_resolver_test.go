package postgresql

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestPostgresCapabilityFactsQueryKeepsSpatialWorkspaceColumnsAligned(t *testing.T) {
	query := postgresCapabilityFactsQuery()
	sdx := strings.Index(query, "lower(schema_name) = 'sdx'")
	sdeSchema := strings.Index(query, "lower(schema_name) = 'sde'")
	sdeTables := strings.Index(query, "lower(table_schema) = 'sde'")
	if sdx < 0 || sdeSchema < 0 || sdeTables < 0 {
		t.Fatalf("workspace probe query is missing expected columns: %s", query)
	}
	if !(sdx < sdeSchema && sdeSchema < sdeTables) {
		t.Fatalf("workspace probe query columns are misordered: sdx=%d sde_schema=%d sde_tables=%d", sdx, sdeSchema, sdeTables)
	}
}

func TestApplyPostgresInstanceCapabilitiesKeepsSpatialCapabilitiesWhenPostGISIsReady(t *testing.T) {
	base := (&PostgreSQLPlugin{}).Capabilities()
	facts := postgresInstanceCapabilityFacts{
		ServerVersion:    "15.8",
		ServerVersionNum: 150008,
		InstalledExtensions: map[string]postgresExtensionFact{
			"postgis":          {Name: "postgis", Version: "3.4.3", Schema: "public"},
			"postgis_topology": {Name: "postgis_topology", Version: "3.4.3", Schema: "topology"},
		},
		AvailableExtensions: map[string]string{
			"postgis":          "3.4.3",
			"postgis_topology": "3.4.3",
		},
		HasPostGISVersion: true,
		HasSTExtent:       true,
		HasSTTransform:    true,
	}

	resolved := applyPostgresInstanceCapabilities(base, facts)

	if resolved.Storage == nil || resolved.Storage.Facts == nil || !resolved.Storage.Facts.SpatialFacts {
		t.Fatalf("spatial_facts = false, want true: %#v", resolved.Storage)
	}
	if resolved.Storage.Store == nil || !resolved.Storage.Store.TableReadSpatialTransform {
		t.Fatalf("table_read_spatial_transform = false, want true: %#v", resolved.Storage.Store)
	}
	if resolved.Storage.Store.TableSpatialEncoding == nil || !resolved.Storage.Store.TableSpatialEncoding.NativeSpatialFunctions {
		t.Fatalf("table_spatial_encoding missing native spatial functions: %#v", resolved.Storage.Store)
	}
	postgresqlExt, ok := resolved.Extensions["postgresql"].(map[string]interface{})
	if !ok {
		t.Fatalf("postgresql extensions missing: %#v", resolved.Extensions)
	}
	postgis, ok := postgresqlExt["postgis"].(map[string]interface{})
	if !ok || postgis["installed"] != true || postgis["version"] != "3.4.3" {
		t.Fatalf("postgis extension facts = %#v", postgis)
	}
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(resolved.Extensions)
	if err != nil {
		t.Fatalf("SpatialWorkspacesFromExtensions error = %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("spatial workspaces = %#v, want both SuperMap workspace candidates", workspaces)
	}
	supermap := workspaces[0]
	if supermap.Ecosystem != "supermap" || supermap.Kind != plugin.SpatialWorkspaceSuperMapSDXPostGIS {
		t.Fatalf("supermap workspace fact = %#v", supermap)
	}
	if supermap.State != plugin.SpatialWorkspaceStateNotDetected {
		t.Fatalf("supermap state = %q, want not_detected", supermap.State)
	}
	if !supermap.CanEnable {
		t.Fatalf("supermap can_enable = false, want true when PostGIS is ready: %#v", supermap)
	}
	if workspaces[1].Kind != plugin.SpatialWorkspaceSuperMapSDXPostgreSQL ||
		workspaces[1].State != plugin.SpatialWorkspaceStateNotDetected || !workspaces[1].CanEnable {
		t.Fatalf("SDX+ for PostgreSQL candidate = %#v, want selectable candidate", workspaces[1])
	}
}

func TestApplyPostgresInstanceCapabilitiesDetectsSpatialWorkspaceSignatures(t *testing.T) {
	base := (&PostgreSQLPlugin{}).Capabilities()
	facts := postgresInstanceCapabilityFacts{
		ServerVersion:            "15.8",
		ServerVersionNum:         150008,
		InstalledExtensions:      map[string]postgresExtensionFact{},
		AvailableExtensions:      map[string]string{},
		SuperMapSystemTableCount: 3,
		ArcGISSdeSchemaCount:     1,
		ArcGISSdeTableCount:      2,
	}

	resolved := applyPostgresInstanceCapabilities(base, facts)
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(resolved.Extensions)
	if err != nil {
		t.Fatalf("SpatialWorkspacesFromExtensions error = %v", err)
	}
	if len(workspaces) != 3 {
		t.Fatalf("spatial workspaces = %#v, want 3", workspaces)
	}
	if workspaces[0].Kind != plugin.SpatialWorkspaceSuperMapSDXPostGIS || workspaces[0].State != plugin.SpatialWorkspaceStateUnavailable {
		t.Fatalf("SDX+ for PostGIS candidate = %#v, want unavailable", workspaces[0])
	}
	if workspaces[1].State != plugin.SpatialWorkspaceStateDetected || workspaces[1].Kind != plugin.SpatialWorkspaceSuperMapSDXPostgreSQL {
		t.Fatalf("SDX+ for PostgreSQL workspace = %#v, want detected", workspaces[1])
	}
	if workspaces[1].CanEnable {
		t.Fatalf("supermap can_enable = true, want false when SuperMap SDX+ for PostgreSQL is already detected")
	}
	if workspaces[2].Ecosystem != "arcgis" || workspaces[2].Kind != "sde" || workspaces[2].State != plugin.SpatialWorkspaceStateDetected {
		t.Fatalf("arcgis workspace fact = %#v, want detected SDE", workspaces[2])
	}
}

func TestApplyPostgresInstanceCapabilitiesKeepsPostGISAndDetectsSDXPostgreSQL(t *testing.T) {
	base := (&PostgreSQLPlugin{}).Capabilities()
	facts := postgresInstanceCapabilityFacts{
		InstalledExtensions: map[string]postgresExtensionFact{
			"postgis": {Name: "postgis", Version: "3.4.3", Schema: "public"},
		},
		AvailableExtensions:      map[string]string{"postgis": "3.4.3"},
		HasPostGISVersion:        true,
		HasSTExtent:              true,
		HasSTTransform:           true,
		SuperMapSystemTableCount: superMapSDXSystemTableThreshold,
		SuperMapSDXSchemaCount:   1,
	}

	resolved := applyPostgresInstanceCapabilities(base, facts)
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(resolved.Extensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 || workspaces[1].Kind != plugin.SpatialWorkspaceSuperMapSDXPostgreSQL || workspaces[1].State != plugin.SpatialWorkspaceStateDetected {
		t.Fatalf("workspaces = %#v, want detected SDX+ for PostgreSQL", workspaces)
	}
	if resolved.Storage == nil || resolved.Storage.Facts == nil || !resolved.Storage.Facts.SpatialFacts {
		t.Fatalf("PostGIS spatial capability was lost: %#v", resolved.Storage)
	}
}

func TestApplyPostgresInstanceCapabilitiesDetectsSDXPostGISWithoutSDXSchema(t *testing.T) {
	base := (&PostgreSQLPlugin{}).Capabilities()
	facts := postgresInstanceCapabilityFacts{
		InstalledExtensions: map[string]postgresExtensionFact{
			"postgis": {Name: "postgis", Version: "3.4.3", Schema: "public"},
		},
		AvailableExtensions:      map[string]string{"postgis": "3.4.3"},
		HasPostGISVersion:        true,
		HasSTExtent:              true,
		SuperMapSystemTableCount: superMapSDXSystemTableThreshold,
	}

	resolved := applyPostgresInstanceCapabilities(base, facts)
	workspaces, err := plugin.SpatialWorkspacesFromExtensions(resolved.Extensions)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 || workspaces[0].Kind != plugin.SpatialWorkspaceSuperMapSDXPostGIS || workspaces[0].State != plugin.SpatialWorkspaceStateDetected {
		t.Fatalf("workspaces = %#v, want detected SDX+ for PostGIS", workspaces)
	}
	if workspaces[1].State != plugin.SpatialWorkspaceStateUnavailable {
		t.Fatalf("SDX+ for PostgreSQL candidate = %#v, want unavailable", workspaces[1])
	}
}

func TestApplyPostgresInstanceCapabilitiesDisablesSpatialCapabilitiesWithoutPostGIS(t *testing.T) {
	base := (&PostgreSQLPlugin{}).Capabilities()
	facts := postgresInstanceCapabilityFacts{
		ServerVersion:       "15.8",
		ServerVersionNum:    150008,
		InstalledExtensions: map[string]postgresExtensionFact{},
		AvailableExtensions: map[string]string{},
	}

	resolved := applyPostgresInstanceCapabilities(base, facts)

	if resolved.Storage == nil || resolved.Storage.Facts == nil {
		t.Fatalf("storage facts missing: %#v", resolved.Storage)
	}
	if resolved.Storage.Facts.SpatialFacts {
		t.Fatal("spatial_facts = true, want false without PostGIS")
	}
	if resolved.Storage.Store == nil {
		t.Fatal("storage store missing")
	}
	if resolved.Storage.Store.TableReadSpatialTransform {
		t.Fatal("table_read_spatial_transform = true, want false without PostGIS")
	}
	if resolved.Storage.Store.TableSpatialEncoding != nil {
		t.Fatalf("table_spatial_encoding = %#v, want nil without PostGIS", resolved.Storage.Store.TableSpatialEncoding)
	}
}
