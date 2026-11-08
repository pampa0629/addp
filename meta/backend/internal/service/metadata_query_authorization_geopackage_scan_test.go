package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanprocessor"
	"gorm.io/gorm"
)

type scannedGeoPackageSource struct {
	plugin.EnginePlugin
	data    []byte
	sidecar bool
	opens   int
}

func (p *scannedGeoPackageSource) Type() string { return "nfs" }
func (p *scannedGeoPackageSource) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *scannedGeoPackageSource) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	if target.StringPath() != "sources/observed.GPKG" {
		return nil, fmt.Errorf("unexpected source content path")
	}
	p.opens++
	return io.NopCloser(bytes.NewReader(p.data)), nil
}
func (p *scannedGeoPackageSource) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if target.StringPath() != "sources/observed.GPKG" {
		if p.sidecar {
			return &plugin.EngineCatalogFacts{Path: target, Kind: "file"}, nil
		}
		return nil, os.ErrNotExist
	}
	size := int64(len(p.data))
	modified := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	return &plugin.EngineCatalogFacts{Path: target, Kind: "file", Storage: &plugin.EngineCatalogStorageFacts{Path: target.StringPath(), SizeBytes: &size}, UpdatedAt: &modified}, nil
}

func TestGeoPackageDeepScanProducesAuthorizationFacts(t *testing.T) {
	exerciseGeoPackageDeepScanAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func exerciseGeoPackageDeepScanAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "observed.gpkg")
	native, err := sql.Open("sqlite3", name)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`PRAGMA application_id=1196444487`, `PRAGMA user_version=10300`,
		`CREATE TABLE gpkg_contents(table_name TEXT PRIMARY KEY,data_type TEXT,identifier TEXT,srs_id INTEGER,min_x DOUBLE,min_y DOUBLE,max_x DOUBLE,max_y DOUBLE)`,
		`CREATE TABLE gpkg_geometry_columns(table_name TEXT,column_name TEXT,geometry_type_name TEXT,srs_id INTEGER,z INTEGER,m INTEGER)`,
		`CREATE TABLE gpkg_spatial_ref_sys(srs_id INTEGER PRIMARY KEY,organization TEXT,organization_coordsys_id INTEGER,definition TEXT)`,
		`INSERT INTO gpkg_spatial_ref_sys VALUES(4326,'EPSG',4326,'GEOGCS["WGS84"]')`,
		`CREATE TABLE roads(fid INTEGER PRIMARY KEY,actual_shape BLOB,geometry TEXT)`,
		`INSERT INTO gpkg_contents VALUES('roads','features','Roads',4326,NULL,NULL,NULL,NULL)`,
		`INSERT INTO gpkg_geometry_columns VALUES('roads','actual_shape','LINESTRING',4326,0,0)`,
	} {
		if _, err := native.Exec(q); err != nil {
			native.Close()
			t.Fatal(err)
		}
	}
	if err := native.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	root := models.MetaNode{TenantID: 7, EngineID: 856, NodeType: "root", Name: "NFS", FullName: ""}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.MetaNode{TenantID: 7, EngineID: 856, ParentNodeID: &root.ID, NodeType: "dir", Name: "sources", FullName: "sources"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	p := &scannedGeoPackageSource{data: raw}
	file := metaitem.StorageFileRef{Name: "observed.GPKG", Path: "sources/observed.GPKG", EngineCatalogPath: plugin.FileItemPath(856, "sources/observed.GPKG"), Size: int64(len(raw))}
	processor := scanprocessor.New(metaRepo.NewScanRepository(db), nil, nil)
	var originalID uint
	for _, stage := range []string{"basic", "deep", "sidecar", "restored"} {
		depth := "deep"
		if stage == "basic" {
			depth = "basic"
		}
		p.sidecar = stage == "sidecar"
		result, err := processor.Process(t.Context(), scanprocessor.FileSingleInput(&commonModels.Engine{ID: 856, EngineType: "nfs"}, 7, &parent, file, metaitem.InferSingleResourceItem(file), "file", file.Name, file.Path, p, nil, depth))
		if err != nil {
			t.Fatal(stage, err)
		}
		if originalID == 0 {
			originalID = result.Item.ID
		}
		if result.Item.ID != originalID {
			t.Fatal("scan created a different DataItem")
		}
		if stage == "basic" && p.opens != 0 {
			t.Fatal("basic read the entire file")
		}
		facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(t.Context(), 7, result.Item.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		_, bindingErr := facts.ApprovedBinding()
		complete := stage == "deep" || stage == "restored"
		if complete != (bindingErr == nil) || (commonJSON.Section(result.Item.Attributes, "format_info.geopackage")["composition_complete"] == true) != complete {
			t.Fatal(stage, "real deep observation did not reach the committed projection", facts, result.Item.Attributes, bindingErr)
		}
		var count int64
		if err := db.Model(&models.MetaItem{}).Where("engine_id=?", 856).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("internal layer became an independent DataItem", count, err)
		}
	}
}
