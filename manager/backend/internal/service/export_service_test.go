package service

import (
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestBuildTableExportTaskConfigUsesTransferSyncShape(t *testing.T) {
	config := buildTableExportExecutionConfig(
		"addp://engine/8/path/public/roads?type=table&item_id=54",
		"addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix",
		"roads.geojson",
		format.FormatGeoJSON,
	)
	if config.Runtime.Boundary != "bounded" {
		t.Fatalf("runtime = %#v, want bounded", config.Runtime)
	}
	if config.Load.Mode != "snapshot" {
		t.Fatalf("load = %#v, want snapshot", config.Load)
	}

	source := config.Source
	if source.Locator != "addp://engine/8/path/public/roads?type=table&item_id=54" ||
		source.Representation != "native" || source.DataType != "table" {
		t.Fatalf("source config = %#v", source)
	}
	target := config.Target
	if target.ParentLocator != "addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix" ||
		target.Name != "roads.geojson" || target.Representation != "encoded" || target.Format != "geojson" {
		t.Fatalf("target config = %#v", target)
	}
	policy := target.Policy
	if _, ok := policy["write_mode"]; ok {
		t.Fatalf("policy = %#v, must not contain legacy write_mode", policy)
	}
	if policy["apply_mode"] != "replace" {
		t.Fatalf("policy = %#v, want replace", policy)
	}
}

func TestBuildEncodedRecordExportTaskConfigUsesNativeCollectionShape(t *testing.T) {
	config := buildEncodedRecordExportExecutionConfig(
		"addp://engine/11/path/Outdoor/Persons?type=collection&item_id=81",
		"addp-infra://minio/manager/tenant_7/export/20260902/session-1?type=prefix",
		"Persons.ejsonl",
		format.FormatMongoDBExtendedJSONL,
	)
	source := config.Source
	if source.Representation != "native" || source.DataType != "unknown" || source.Format != "" {
		t.Fatalf("source = %#v", source)
	}
	target := config.Target
	if target.Representation != "encoded" || target.Format != "mongodb_extended_jsonl" || target.Name != "Persons.ejsonl" {
		t.Fatalf("target = %#v", target)
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
