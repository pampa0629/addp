package metaenrich

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

const observedGeoPackagePath = "sources/ 原名 %.GPKG"

type geoPackageSourceFixture struct {
	bytesContentReader
	fault  string
	opens  int
	probes []string
}

func (p *geoPackageSourceFixture) Type() string { return "nfs" }
func (p *geoPackageSourceFixture) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	if target.StringPath() != observedGeoPackagePath {
		return nil, fmt.Errorf("unexpected content open")
	}
	p.opens++
	if p.fault == "open error" {
		return nil, io.ErrUnexpectedEOF
	}
	data := p.content
	if p.fault == "short read" {
		data = data[:len(data)-1]
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (p *geoPackageSourceFixture) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.probes = append(p.probes, target.StringPath())
	if target.StringPath() != observedGeoPackagePath {
		if p.fault == "sidecar unknown" {
			return nil, fmt.Errorf("source facts unavailable")
		}
		if p.fault == "sidecar present" || (p.fault == "late sidecar" && p.opens > 0) {
			return &plugin.EngineCatalogFacts{Path: target, Kind: "file"}, nil
		}
		return nil, fmt.Errorf("missing sidecar: %w", os.ErrNotExist)
	}
	if p.fault == "main error" {
		return nil, io.ErrUnexpectedEOF
	}
	size := int64(len(p.content))
	updated := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	if p.fault == "changed during read" && p.opens > 0 {
		updated = updated.Add(time.Second)
	}
	f := &plugin.EngineCatalogFacts{Path: target, Kind: "file", Storage: &plugin.EngineCatalogStorageFacts{Path: observedGeoPackagePath, SizeBytes: &size}, UpdatedAt: &updated}
	switch p.fault {
	case "no version":
		f.UpdatedAt = nil
	case "wrong type":
		f.Kind = "directory"
	case "wrong target":
		f.Path = plugin.FileItemPath(14, "sources/other.GPKG")
	case "wrong storage":
		f.Storage.Path = "sources/other.GPKG"
	}
	return f, nil
}

func geoPackageDeepBytes(t *testing.T, extra ...string) []byte {
	t.Helper()
	return sqliteDatabaseBytes(t, func(db *sql.DB) {
		for _, query := range append([]string{
			`PRAGMA application_id=1196444487`, `PRAGMA user_version=10300`,
			`CREATE TABLE gpkg_contents(table_name TEXT PRIMARY KEY,data_type TEXT,identifier TEXT,srs_id INTEGER,min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE)`,
			`CREATE TABLE gpkg_geometry_columns(table_name TEXT,column_name TEXT,geometry_type_name TEXT,srs_id INTEGER,z INTEGER,m INTEGER)`,
			`CREATE TABLE gpkg_spatial_ref_sys(srs_id INTEGER PRIMARY KEY,organization TEXT,organization_coordsys_id INTEGER,definition TEXT)`,
			`INSERT INTO gpkg_spatial_ref_sys VALUES(4326,'EPSG',4326,'GEOGCS["WGS84"]')`,
			`CREATE TABLE roads(fid INTEGER PRIMARY KEY,actual_shape BLOB,geometry TEXT)`,
			`INSERT INTO gpkg_contents VALUES('roads','features','Roads',4326,NULL,NULL,NULL,NULL)`,
			`INSERT INTO gpkg_geometry_columns VALUES('roads','actual_shape','LINESTRING',4326,0,0)`,
		}, extra...) {
			if _, err := db.Exec(query); err != nil {
				t.Fatal(query, err)
			}
		}
	})
}

func TestGeoPackageDeepObservationBindsExactStableSource(t *testing.T) {
	for _, fault := range []string{"none", "sidecar present", "late sidecar", "sidecar unknown", "main error", "open error", "changed during read", "no version", "wrong type", "wrong target", "wrong storage", "short read", "external extension"} {
		t.Run(fault, func(t *testing.T) {
			var extra []string
			if fault == "external extension" {
				extra = []string{`CREATE TABLE gpkg_extensions(table_name TEXT,column_name TEXT,extension_name TEXT,definition TEXT,scope TEXT)`, `INSERT INTO gpkg_extensions VALUES('roads','actual_shape','vendor_external','https://example.invalid','read-write')`}
			}
			data := geoPackageDeepBytes(t, extra...)
			p := &geoPackageSourceFixture{bytesContentReader: bytesContentReader{content: data}, fault: fault}
			item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: "single", DataType: datatype.Container, Format: "geopackage", PrimaryContentPath: observedGeoPackagePath}}
			attrs := models.JSONMap{"format_info": map[string]interface{}{"geopackage": map[string]interface{}{"composition_complete": true, "old_claim": "must disappear"}}}
			_, _, err := EnrichResourceAttributes(t.Context(), attrs, ResourceAttributesInput{ContentReader: p, EngineID: 14, Item: item, PhysicalPath: observedGeoPackagePath, EngineCatalogPathFor: plugin.FileItemPathForEngine(14)})
			native := commonJSON.Section(attrs, "format_info.geopackage")
			if fault == "none" {
				if err != nil || native["standalone_vector"] != true || native["composition_complete"] != true {
					t.Fatal("stable NFS standalone observation lost", native, err)
				}
				want := []dataitem.ItemRef{{Path: observedGeoPackagePath, Role: "container", Primary: true, Required: true, Extension: ".gpkg"}}
				if !reflect.DeepEqual(item.RefList, want) || len(p.probes) != 8 || p.opens != 1 {
					t.Fatal("identity/refs or bounded exact I/O differ", item.RefList, p.probes, p.opens)
				}
				refs := commonJSON.InterfaceSlice(commonJSON.Section(attrs, "item")["refs"])
				if len(refs) != 1 || refs[0].(map[string]interface{})["path"] != observedGeoPackagePath {
					t.Fatal("complete source refs were not persisted", attrs)
				}
				if commonJSON.Section(attrs, "type_info.container")["child_count"] != 1 {
					t.Fatal("existing container metadata was lost", attrs)
				}
			} else if native["composition_complete"] == true {
				t.Fatal("stale complete state survived unsuccessful observation", native, err)
			}
			if native["old_claim"] != nil {
				t.Fatal("stale format namespace retained")
			}
			for _, target := range p.probes {
				if target != observedGeoPackagePath && target != observedGeoPackagePath+"-wal" && target != observedGeoPackagePath+"-shm" && target != observedGeoPackagePath+"-journal" {
					t.Fatal("scan expanded beyond exact four targets", target)
				}
			}
			if !bytes.Equal(p.content, data) {
				t.Fatal("source bytes changed")
			}
		})
	}
}

func TestGeoPackageByteOnlyEnrichmentCannotProduceCompleteSource(t *testing.T) {
	item := detectedItemForTest(dataitem.ResolvedItem{Layout: "single", DataType: datatype.Container, Format: "geopackage"})
	attrs := models.JSONMap{}
	data := geoPackageDeepBytes(t)
	if err := EnrichContainerChildren(t.Context(), attrs, item, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	native := commonJSON.Section(attrs, "format_info.geopackage")
	if native["standalone_vector"] != true || native["composition_complete"] != false {
		t.Fatal("byte-only metadata became source authorization facts", native)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := &geoPackageSourceFixture{bytesContentReader: bytesContentReader{content: data}}
	_, _, err := EnrichResourceAttributes(ctx, attrs, ResourceAttributesInput{ContentReader: p, EngineID: 14, Item: item, PhysicalPath: observedGeoPackagePath, EngineCatalogPathFor: plugin.FileItemPathForEngine(14)})
	if !errors.Is(err, context.Canceled) && (err == nil || !strings.Contains(err.Error(), "cancel")) {
		t.Fatal("cancellation was lost", err)
	}
}
