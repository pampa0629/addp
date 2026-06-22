package service

import (
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/manager/internal/models"
)

func TestBuildTableExportTaskConfigUsesTransferSyncShape(t *testing.T) {
	config := buildTableExportTaskConfig(
		"addp://engine/8/path/public/roads?type=table&item_id=54",
		"addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix",
		"roads.geojson",
		format.FormatGeoJSON,
	)

	source := config["source"].(map[string]interface{})
	if source["locator"] != "addp://engine/8/path/public/roads?type=table&item_id=54" ||
		source["representation"] != "native" ||
		source["data_type"] != "table" {
		t.Fatalf("source config = %#v", source)
	}
	target := config["target"].(map[string]interface{})
	if target["parent_locator"] != "addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix" ||
		target["name"] != "roads.geojson" ||
		target["representation"] != "encoded" ||
		target["format"] != "geojson" {
		t.Fatalf("target config = %#v", target)
	}
	policy := target["policy"].(map[string]interface{})
	if policy["write_mode"] != "overwrite" {
		t.Fatalf("policy = %#v, want overwrite", policy)
	}
}

func TestSupportedExportFormatIncludesSingleAndMultiRefWriters(t *testing.T) {
	if !supportedExportFormat(format.FormatCSV) {
		t.Fatal("csv should be supported")
	}
	if !supportedExportFormat(format.FormatShapefile) {
		t.Fatal("shapefile multi-ref export should be supported")
	}
}

func TestExportFileNamesKeepTransferTargetAndUserDownloadSeparate(t *testing.T) {
	if got := withExportExtension("roads", format.FormatShapefile); got != "roads.shp" {
		t.Fatalf("withExportExtension(shapefile) = %q, want roads.shp", got)
	}
	if got := exportDownloadFileName("roads", format.FormatShapefile); got != "roads.zip" {
		t.Fatalf("exportDownloadFileName(shapefile) = %q, want roads.zip", got)
	}
	if got := exportDownloadFileName("roads", format.FormatCSV); got != "roads.csv" {
		t.Fatalf("exportDownloadFileName(csv) = %q, want roads.csv", got)
	}
}

func TestExportArtifactManifestJSONBuildsZipEntriesFromTargetRefs(t *testing.T) {
	session := &models.ExportSession{
		Format:        string(format.FormatShapefile),
		FileName:      "roads.zip",
		TargetLocator: "addp-infra://minio/manager/tenant_7/export/20260621/session/roads.shp?type=object",
	}
	manifestJSON := exportArtifactManifestJSON(session, models.JSONMap{
		"target_refs": []interface{}{
			map[string]interface{}{"path": "tenant_7/export/20260621/session/roads.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
			map[string]interface{}{"path": "tenant_7/export/20260621/session/roads.shx", "role": "shx", "required": true, "primary": false, "extension": ".shx"},
			map[string]interface{}{"path": "tenant_7/export/20260621/session/roads.dbf", "role": "dbf", "required": true, "primary": false, "extension": ".dbf"},
		},
	})
	manifest := exportArtifactManifestFromJSON(manifestJSON)
	if manifest.SchemaVersion != exportArtifactManifestVersion || manifest.Layout != format.LayoutMulti {
		t.Fatalf("manifest = %#v, want multi manifest", manifest)
	}
	if manifest.Download.Kind != "zip" || manifest.Download.FileName != "roads.zip" {
		t.Fatalf("download = %#v, want zip roads.zip", manifest.Download)
	}
	wantEntries := []string{"roads.shp", "roads.shx", "roads.dbf"}
	if len(manifest.Refs) != len(wantEntries) {
		t.Fatalf("refs = %#v, want %d refs", manifest.Refs, len(wantEntries))
	}
	for i, want := range wantEntries {
		if manifest.Refs[i].Entry != want {
			t.Fatalf("entry[%d] = %q, want %q", i, manifest.Refs[i].Entry, want)
		}
	}
}

func TestExportStatusFromTransferStatus(t *testing.T) {
	cases := map[string]string{
		"pending":   models.ExportSessionStatusPending,
		"running":   models.ExportSessionStatusRunning,
		"success":   models.ExportSessionStatusSuccess,
		"failed":    models.ExportSessionStatusFailed,
		"timeout":   models.ExportSessionStatusFailed,
		"cancelled": models.ExportSessionStatusFailed,
	}
	for input, want := range cases {
		if got := exportStatusFromTransferStatus(input); got != want {
			t.Fatalf("exportStatusFromTransferStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestManagerInfraObjectPathValidatesBucket(t *testing.T) {
	got, err := managerInfraObjectPath("addp-infra://minio/manager/tenant_7/export/20260620/session/roads.csv?type=object", "manager")
	if err != nil {
		t.Fatalf("managerInfraObjectPath() error = %v", err)
	}
	if got != "tenant_7/export/20260620/session/roads.csv" {
		t.Fatalf("object path = %q", got)
	}
	if _, err := managerInfraObjectPath("addp-infra://minio/other/tenant_7/export/roads.csv?type=object", "manager"); err == nil {
		t.Fatal("managerInfraObjectPath() accepted different bucket")
	}
}
