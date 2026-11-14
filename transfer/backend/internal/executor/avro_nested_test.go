package executor

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/format"
	avroformat "github.com/addp/common/format/plugins/avro"
	parquetformat "github.com/addp/common/format/plugins/parquet"
)

func TestNestedAvroRejectsTransferBeforeOpeningOrReplacingTargets(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-nested.avro")
	if err != nil {
		t.Fatal(err)
	}
	p := avroformat.NewPlugin()
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []bool{false, true} {
		for _, project := range []bool{false, true} {
			options := format.DefaultParseOptions()
			if project {
				options.FieldSelection = &format.FieldSelectionOptions{Include: []string{"id"}}
			}
			source := &headerTrackedContent{fakeContentReader: fakeContentReader{content: string(data)}}
			native := &fakeNativeTableWriter{}
			output := &headerTargetContent{}
			exec := &TableTransferExecutor{SourceContentReader: source, SourceTableReadProvider: p, TargetNativePreparer: native, TargetNativeWriter: native, TargetDeleteProvider: native}
			plan := TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatAvro, ParseOptions: options, TableInfo: info.Table}, Target: TableTargetPlan{Kind: TableEndpointNative, DeleteBeforeWrite: true}}
			if encoded {
				plan.Target.Kind = TableEndpointEncoded
				plan.Target.Format = format.FormatParquet
				exec.TargetContentWriter = output
				exec.TargetTableWriterProvider = parquetformat.NewPlugin()
			}
			_, err := exec.Execute(t.Context(), plan)
			if err == nil || !strings.Contains(err.Error(), "preview only") {
				t.Fatalf("wrong nested rejection: %v", err)
			}
			if native.deleted || len(native.preparedFields) > 0 || len(native.batches) > 0 || output.creates > 0 {
				t.Fatal("target effects before nested source refusal")
			}
			if source.closed != 1 {
				t.Fatalf("source closes %d", source.closed)
			}
		}
	}
}
