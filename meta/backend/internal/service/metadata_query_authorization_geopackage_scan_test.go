package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanprocessor"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
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

func TestGeoPackageBasicScanProducesAuthorizationFacts(t *testing.T) {
	exerciseGeoPackageBasicScanAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func exerciseGeoPackageBasicScanAuthorizationFacts(t *testing.T, db *gorm.DB) {
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
	var boundaryHash string
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
		if bindingErr != nil || (commonJSON.Section(result.Item.Attributes, "format_info.geopackage")["integrity_verified"] == true) != complete {
			t.Fatal(stage, "integrity and authorization were coupled", facts, result.Item.Attributes, bindingErr)
		}
		if facts.BoundaryHash == nil {
			t.Fatal("missing basic boundary hash")
		}
		if stage == "basic" {
			boundaryHash = *facts.BoundaryHash
			// The actual basic processor output must also satisfy the unified
			// producer identification path without a deep or integrity marker.
			ref := production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
			receipt := production.Receipt{Reference: ref, TenantID: 7, Attempt: 1, TargetEngineVersion: 1, OperationID: uuid.NewString(), CommittedAt: result.Item.CreatedAt.Add(-2 * time.Second), Primary: plugin.FileItemPath(856, file.Path), Origin: &production.UserOrigin{PrincipalID: 31, MembershipID: 41, AuthorizationVersion: 1}, Sources: production.SourceEvidence{Kind: production.SourcesNone}, Objects: []production.CommittedObject{{Target: plugin.FileItemPath(856, file.Path), Created: true}}}
			request, requestErr := productionScanRequest(receipt)
			if requestErr != nil || request.ScanDepth != "basic" {
				t.Fatal("production identification requires deep", requestErr)
			}
			run := scantask.NewManualExecution(7, 31, 856, 0, "nfs", request.Targets, nil, request.RefGroups, "basic", true, "develop", result.Item.CreatedAt.Add(-time.Second))
			run.ParentExecutionID = &ref.ExecutionID
			run.ActorPrincipalID = &receipt.Origin.PrincipalID
			run.ActorTenantMembershipID = &receipt.Origin.MembershipID
			run.IssuedAuthorizationVersion = &receipt.Origin.AuthorizationVersion
			tenant := uint(7)
			freezeScanEngineSnapshot(run, &commonModels.Engine{ID: 856, TenantID: &tenant, Version: 1, EngineType: "nfs"})
			run.ExecutionConfig["initial_production_receipt"], run.ExecutionConfig["initial_production_new_candidate"], run.Status = receipt, true, "success"
			identified, identifyErr := (&ScanExecutionService{db: db}).productionIdentification(t.Context(), db, run, 7, ref)
			if identifyErr != nil || identified.Binding == nil || identified.LogicalItemState != "new" || identified.Binding.BoundaryHash != boundaryHash {
				t.Fatal("basic result did not support initial production identification", identified, identifyErr)
			}
		} else if *facts.BoundaryHash != boundaryHash {
			t.Fatal("deep observation changed the file authorization boundary")
		}
		if len(facts.Boundary.FileComposition.Refs) != 1 || !reflect.DeepEqual(facts.Boundary.FileComposition.Refs[0].Target, plugin.FileItemPath(856, file.Path)) {
			t.Fatal("basic did not preserve exact single-file composition")
		}
		var count int64
		if err := db.Model(&models.MetaItem{}).Where("engine_id=?", 856).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("internal layer became an independent DataItem", count, err)
		}
	}
	p.data = []byte("invalid database bytes")
	if _, err := processor.Process(t.Context(), scanprocessor.FileSingleInput(&commonModels.Engine{ID: 856, EngineType: "nfs"}, 7, &parent, file, metaitem.InferSingleResourceItem(file), "file", file.Name, file.Path, p, nil, "deep")); err == nil {
		t.Fatal("corrupt deep input was accepted")
	}
	var stored models.MetaItem
	if err := db.First(&stored, originalID).Error; err != nil {
		t.Fatal(err)
	}
	facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(t.Context(), 7, stored.Fingerprint)
	if err != nil || facts.BoundaryHash == nil || *facts.BoundaryHash != boundaryHash {
		t.Fatal("failed deep scan changed authorization", err)
	}
	if _, err := facts.ApprovedBinding(); err != nil {
		t.Fatal("failed deep scan removed authorization basis", err)
	}
}
