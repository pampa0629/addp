package planner

import (
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/pdf"
)

func TestBuildTableExportPlanForNativeTableToCSVFile(t *testing.T) {
	spec := TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"schema": "public", "table": "roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: "exports/roads.csv"},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
			Policy:         map[string]interface{}{"write_mode": "overwrite"},
		},
		BatchSize: 500,
	}
	resolver := StaticEngineResolver{
		1: {Type: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"database": "gis"}},
		2: {Type: "nfs", ConnInfo: engineplugin.ConnectionInfo{"server": "127.0.0.1", "export_path": "/data"}},
	}

	result, err := BuildTableExportPlan(spec, resolver)
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}

	if result.SourceEngineType != "postgresql" || result.TargetEngineType != "nfs" {
		t.Fatalf("engine types = %q -> %q, want postgresql -> nfs", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Format != format.FormatCSV || result.Plan.Format != format.FormatCSV {
		t.Fatalf("format = %q / %q, want csv", result.Format, result.Plan.Format)
	}
	if result.Plan.BatchSize != 500 {
		t.Fatalf("batch size = %d, want 500", result.Plan.BatchSize)
	}
	if got := result.Plan.SourcePath.StringPath(); got != "public/roads" {
		t.Fatalf("source path = %q, want public/roads", got)
	}
	if got := result.Plan.TargetPath.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
	if !result.Plan.TargetWrite.Overwrite {
		t.Fatal("target overwrite = false, want true")
	}
	if result.Plan.WriteOptions == nil {
		t.Fatal("write options is nil")
	}
}

func TestBuildTableExportPlanForObjectTarget(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Resource = ResourceSpec{Kind: resourceKindObject, Path: map[string]interface{}{"bucket": "exports", "path": "roads.csv"}}

	result, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "s3"},
	})
	if err != nil {
		t.Fatalf("BuildTableExportPlan failed: %v", err)
	}
	if got := result.Plan.TargetPath.StringPath(); got != "exports/roads.csv" {
		t.Fatalf("target path = %q, want exports/roads.csv", got)
	}
}

func TestBuildTableExportPlanRejectsNonTableFormat(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Target.Format = format.FormatPDF

	_, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableExportPlan succeeded, want format data type error")
	}
	if !strings.Contains(err.Error(), "data type") {
		t.Fatalf("error = %q, want data type error", err)
	}
}

func TestBuildTableExportPlanRejectsEncodedSource(t *testing.T) {
	spec := minimalTableExportSpec()
	spec.Source.Representation = representationEncoded

	_, err := BuildTableExportPlan(spec, StaticEngineResolver{
		1: {Type: "postgresql"},
		2: {Type: "nfs"},
	})
	if err == nil {
		t.Fatal("BuildTableExportPlan succeeded, want representation error")
	}
	if !strings.Contains(err.Error(), "source representation") {
		t.Fatalf("error = %q, want source representation error", err)
	}
}

func minimalTableExportSpec() TableExportTaskSpec {
	return TableExportTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{ID: 1},
			Resource:       ResourceSpec{Kind: resourceKindNativeTable, Path: map[string]interface{}{"name": "public.roads"}},
			DataType:       dataTypeTable,
			Representation: representationNative,
		},
		Target: EndpointSpec{
			Engine:         EngineRef{ID: 2},
			Resource:       ResourceSpec{Kind: resourceKindFile, Path: map[string]interface{}{"path": "exports/roads.csv"}},
			DataType:       dataTypeTable,
			Representation: representationEncoded,
			Format:         format.FormatCSV,
		},
	}
}
