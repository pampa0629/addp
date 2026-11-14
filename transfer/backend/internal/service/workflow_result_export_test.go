package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
)

type workflowInputProvider struct {
	plugin.ContentReadableProvider
	data  string
	opens int
}

func (p *workflowInputProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.opens++
	return io.NopCloser(strings.NewReader(p.data)), nil
}
func TestWorkflowExportInputVerifiesConsumedBytesAndDeadline(t *testing.T) {
	provider := &workflowInputProvider{data: `[{"area":42.375}]`}
	path := plugin.FileItemPath(0, "input.json")
	source := &client.WorkflowResultExportSource{ExpiresAt: time.Now().Add(time.Minute), SHA256: client.WorkflowResultRowsDigest([]byte(provider.data))}
	reader := workflowInputReader{ContentReadableProvider: provider, binding: source, path: path}
	stream, err := reader.OpenContent(t.Context(), nil, path, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stream)
	stream.Close()
	if string(data) != provider.data {
		t.Fatal("changed bytes")
	}
	provider.data = `[{"area":99}]`
	if _, err := reader.OpenContent(t.Context(), nil, path, plugin.ReadOptions{}); err == nil {
		t.Fatal("accepted changed input")
	}
	source.ExpiresAt = time.Now().Add(-time.Second)
	before := provider.opens
	if _, err := reader.OpenContent(t.Context(), nil, path, plugin.ReadOptions{}); err == nil || provider.opens != before {
		t.Fatal("opened expired input")
	}
}

type workflowCSVTarget struct {
	plugin.StoreProvider
	buffer bytes.Buffer
}
type workflowCSVWriter struct{ *bytes.Buffer }

func (w workflowCSVWriter) Close() error { return nil }
func (p *workflowCSVTarget) CreateContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.WriteOptions) (io.WriteCloser, error) {
	return workflowCSVWriter{&p.buffer}, nil
}
func (p *workflowCSVTarget) DeleteResource(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) error {
	return nil
}
func TestWorkflowResultUsesExistingJSONToCSVTablePipeline(t *testing.T) {
	data := `[{"instance_id":1,"surface":42.375},{"instance_id":2,"surface":9.125}]`
	parent := exportartifact.InfraPrefixLocator("manager", "tenant_7/export/develop/20261114/"+uuid.NewString())
	source, _ := planner.ParseInfraLocatorURI(parent)
	input, _ := source.Child("workflow-result.json", resourcetree.TypeObject)
	request := client.TransferExecutionConfig{WorkflowResult: &client.WorkflowResultExportSource{ExecutionID: uuid.NewString(), Attempt: 1, ExpiresAt: time.Now().Add(time.Minute), SHA256: client.WorkflowResultRowsDigest([]byte(data)), Columns: []string{"instance_id", "surface"}, RowCount: 2, Targets: []dataprotection.ResourceReference{{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "source"}}}, Runtime: client.TransferExecutionRuntime{Boundary: "bounded"}, Load: client.TransferExecutionLoad{Mode: "snapshot"}, Source: client.TransferExecutionEndpoint{Locator: input.ToURI(), DataType: "table", Representation: "encoded", Format: "json"}, Target: client.TransferExecutionEndpoint{ParentLocator: parent, Name: "areas.csv", DataType: "table", Representation: "encoded", Format: "csv", Policy: map[string]interface{}{"apply_mode": "replace"}}, Transforms: []client.TransferExecutionTransform{{Type: "field_mapping", Mode: "project", Fields: []client.TransferExecutionFieldMapping{{Source: "instance_id", Target: "instance_id", TargetType: "unknown", Nullable: true}, {Source: "surface", Target: "surface", TargetType: "unknown", Nullable: true}}}}}
	raw, _ := json.Marshal(request)
	var config map[string]interface{}
	json.Unmarshal(raw, &config)
	spec, err := planner.ParseTableExportTaskSpec(config, 1000)
	if err != nil {
		t.Fatal(err)
	}
	build, err := planner.BuildTableTransferPlan(spec, planner.NewInfraEngineResolver(planner.InfraEngineConfig{MinioEndpoint: "127.0.0.1:1", MinioAccessKey: "test", MinioSecretKey: "test-secret"}))
	if err != nil {
		t.Fatal(err)
	}
	codec, err := executor.NewTableTransferExecutor(build.SourceEngineType, build.TargetEngineType, build.Plan.Source.Format, build.Plan.Target.Format)
	if err != nil {
		t.Fatal(err)
	}
	provider := &workflowInputProvider{data: data}
	codec.SourceContentReader = workflowInputReader{ContentReadableProvider: provider, binding: spec.WorkflowResult, path: build.Plan.Source.Path}
	target := &workflowCSVTarget{}
	codec.TargetContentWriter = target
	codec.TargetDeleteProvider = target
	metrics, err := codec.Execute(t.Context(), build.Plan)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(target.buffer.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || len(records) != 3 || strings.Join(records[0], ",") != "instance_id,surface" || records[1][1] != "42.375" || records[2][1] != "9.125" {
		t.Fatalf("pipeline changed complete result: %v", records)
	}
}
