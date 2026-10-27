package planner

import (
	"github.com/addp/common/format"
	"testing"
)

func TestORCSingleFilePlanRequiresScannedVersion(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Source.Locator = "addp://engine/1/path/imports/table.orc?type=object"
	spec.Source.Format = format.FormatORC
	spec.Source.Attributes = tableSourceAttributes("single", "orc", "imports/table.orc", nil, []map[string]interface{}{{"name": "id", "type": "bigint", "nullable": true}}, nil)
	spec.Source.Attributes["format_info"] = map[string]interface{}{"orc": map[string]interface{}{"source_version": "bound-version"}}
	spec.Target.Format = format.FormatParquet
	spec.Target.Name = "table.parquet"
	resolver := StaticEngineResolver{1: {Type: "minio"}, 2: {Type: "minio"}}
	result, err := BuildTableTransferPlan(spec, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Source.ParseOptions.ExtraParams["source_version"] != "bound-version" || result.Plan.Source.Format != format.FormatORC {
		t.Fatal("source version or format was dropped")
	}
	delete(spec.Source.Attributes, "format_info")
	if _, err = BuildTableTransferPlan(spec, resolver); err == nil {
		t.Fatal("unscanned source accepted")
	}
}
