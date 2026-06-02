package planner

import (
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/docx"
	_ "github.com/addp/common/format/plugins/pdf"
)

func TestBuildRawCopyPlanInheritsTargetIdentity(t *testing.T) {
	spec := minimalRawCopySpec()
	spec.Target.DataType = ""
	spec.Target.Format = ""
	result, err := BuildRawCopyPlan(spec, StaticEngineResolver{
		1: {Type: "minio"},
		2: {Type: "nfs"},
	})
	if err != nil {
		t.Fatalf("BuildRawCopyPlan failed: %v", err)
	}
	if result.SourceEngineType != "minio" || result.TargetEngineType != "nfs" {
		t.Fatalf("engine types = %q -> %q, want minio -> nfs", result.SourceEngineType, result.TargetEngineType)
	}
	if result.Plan.DataType != "document" || result.Plan.Format != format.FormatPDF {
		t.Fatalf("plan identity = %s/%s, want document/pdf", result.Plan.DataType, result.Plan.Format)
	}
	if got := result.Plan.Source.Path.StringPath(); got != "docs/a.pdf" {
		t.Fatalf("source path = %q, want docs/a.pdf", got)
	}
	if got := result.Plan.Target.Path.StringPath(); got != "backup/a.pdf" {
		t.Fatalf("target path = %q, want backup/a.pdf", got)
	}
	if !result.Plan.Target.DeleteBeforeWrite {
		t.Fatal("target delete before write = false, want true")
	}
	if result.Plan.Target.ContentWrite.Overwrite {
		t.Fatal("content write overwrite = true, want delete-before-write instead")
	}
}

func TestBuildRawCopyPlanRejectsMultiLayoutSource(t *testing.T) {
	spec := minimalRawCopySpec()
	spec.Source.Attributes = map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "document",
			"format":    "pdf",
			"layout":    string(dataitem.LayoutMulti),
		},
	}
	_, err := BuildRawCopyPlan(spec, StaticEngineResolver{1: {Type: "minio"}, 2: {Type: "nfs"}})
	if err == nil || !strings.Contains(err.Error(), "layout=\"single\"") {
		t.Fatalf("BuildRawCopyPlan error = %v, want layout single error", err)
	}
}

func TestBuildRawCopyPlanRejectsTargetFormatConflict(t *testing.T) {
	spec := minimalRawCopySpec()
	spec.Target.Format = format.FormatDOCX
	_, err := ParseRawCopyTaskSpec(map[string]interface{}{
		"mode":   spec.Mode,
		"source": spec.Source,
		"target": spec.Target,
	})
	if err == nil || !strings.Contains(err.Error(), "must match source format") {
		t.Fatalf("ParseRawCopyTaskSpec error = %v, want format conflict", err)
	}
}

func minimalRawCopySpec() RawCopyTaskSpec {
	return RawCopyTaskSpec{
		Mode: modeBatch,
		Source: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 1},
			DataType:       "document",
			Representation: representationEncoded,
			Format:         format.FormatPDF,
			EndpointResource: EndpointResourceSpec{
				Kind: EndpointResourceKindObject,
				Path: map[string]interface{}{"bucket": "docs", "path": "a.pdf"},
			},
		},
		Target: EndpointSpec{
			Engine:         EngineRef{Scope: "system", ID: 2},
			DataType:       "document",
			Representation: representationEncoded,
			Format:         format.FormatPDF,
			EndpointResource: EndpointResourceSpec{
				Kind: EndpointResourceKindFile,
				Path: map[string]interface{}{"path": "backup/a.pdf"},
			},
			Policy: map[string]interface{}{"write_mode": "overwrite"},
		},
	}
}
