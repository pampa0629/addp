package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/infra/objectstore"
	objectcontent "github.com/addp/common/infra/objectstore/contentadapter"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
)

type workflowInputFixture struct {
	data  string
	opens int
	conn  plugin.ConnectionInfo
}

func newWorkflowInputFixture(t *testing.T, data string) *workflowInputFixture {
	t.Helper()
	fixture := &workflowInputFixture{data: data}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
			return
		}
		fixture.opens++
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", fmt.Sprint(len(fixture.data)))
		fmt.Fprint(w, fixture.data)
	}))
	t.Cleanup(server.Close)
	fixture.conn = plugin.ConnectionInfo{"endpoint": server.URL, "access_key": "test", "secret_key": "test-secret"}
	return fixture
}
func TestWorkflowExportInputVerifiesConsumedBytesAndDeadline(t *testing.T) {
	provider := newWorkflowInputFixture(t, `[{"area":42.375}]`)
	path := plugin.ObjectItemPath(0, "manager", "input.json")
	source := &client.WorkflowResultExportSource{ExpiresAt: time.Now().Add(time.Minute), SHA256: client.WorkflowResultRowsDigest([]byte(provider.data))}
	reader := workflowInputReader{ContentReadableProvider: &objectcontent.Provider{Name: objectstore.Manager, Store: testInfraArea(t, plugin.GetString(provider.conn, "endpoint"))}, binding: source, path: path}
	stream, err := reader.OpenContent(t.Context(), provider.conn, path, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stream)
	stream.Close()
	if string(data) != provider.data {
		t.Fatal("changed bytes")
	}
	provider.data = `[{"area":99}]`
	if _, err := reader.OpenContent(t.Context(), provider.conn, path, plugin.ReadOptions{}); err == nil {
		t.Fatal("accepted changed input")
	}
	source.ExpiresAt = time.Now().Add(-time.Second)
	before := provider.opens
	if _, err := reader.OpenContent(t.Context(), provider.conn, path, plugin.ReadOptions{}); err == nil || provider.opens != before {
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
	provider := newWorkflowInputFixture(t, data)
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
	build, err := planner.BuildTableTransferPlan(spec, planner.NewInfraEngineResolver(planner.InfraEngineConfig{ObjectArea: testInfraArea(t, plugin.GetString(provider.conn, "endpoint"))}))
	if err != nil {
		t.Fatal(err)
	}
	codec, err := executor.NewTableTransferExecutorWithProviders(build.SourceEngine.BoundProvider, build.TargetEngine.BoundProvider, build.Plan.Source.Format, build.Plan.Target.Format)
	if err != nil {
		t.Fatal(err)
	}
	codec.SourceContentReader = workflowInputReader{ContentReadableProvider: codec.SourceContentReader, binding: spec.WorkflowResult, path: build.Plan.Source.Path}
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
