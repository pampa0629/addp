package postgresql

import "testing"

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
