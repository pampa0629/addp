package gdalvector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

func TestRuntimeProviderReadAndWriteScope(t *testing.T) {
	var mutex sync.Mutex
	requests := map[string][]map[string]interface{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/operators/"), "/invoke")
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		requests[operator] = append(requests[operator], request)
		mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch operator {
		case operatorInspect:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"result": map[string]interface{}{
					"schema_version": ContainerInspectionSchema,
					"format":         "filegdb",
					"container": datatype.ContainerInfo{
						ChildCount: 1, DefaultChild: "regions", ResourceCount: 1,
						Children: []datatype.ContainerChildInfo{{
							Name: "regions", ChildKind: "feature_class", DataType: datatype.Table,
							Native: map[string]interface{}{"table": "regions"},
						}},
					},
					"format_info": map[string]interface{}{"driver": "OpenFileGDB"},
				},
			})
		case operatorReadOpen:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"result": map[string]interface{}{
					"fields":  []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: true}},
					"spatial": datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("SHAPE", "MultiPolygon", 3424, 2)),
				},
			})
		case operatorReadBatch:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"result": map[string]interface{}{
					"rows": []map[string]interface{}{{"id": 1, "SHAPE": []byte{1, 2, 3}}},
				},
			})
		default:
			_, _ = w.Write([]byte(`{"status":"success","result":{"protocol":"gdal.vector-batch/v1"}}`))
		}
	}))
	defer server.Close()

	runtime := engineplugin.NewHTTPWorkflowRuntimeProvider("geopython_workflow", "GeoPython")
	runtimeConn := runtimeConnectionInfo(t, server.URL)
	descriptor := format.FormatDescriptor{ID: "test-filegdb", Format: format.FormatFileGDB, DataType: datatype.Container, Layouts: []string{format.LayoutWhole}}
	plugin := NewReadWritePlugin(descriptor)

	sourcePlan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{
		Kind: workflowaccess.KindDirectory, Format: "filegdb",
		Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/source.gdb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readerProvider, err := plugin.BindScopeTableReader(runtime, runtimeConn, sourcePlan)
	if err != nil {
		t.Fatal(err)
	}
	containerProvider, err := plugin.BindContainerInfoProvider(runtime, runtimeConn, sourcePlan)
	if err != nil {
		t.Fatal(err)
	}
	containerResult, err := containerProvider.DescribeContainer(context.Background(), format.ContainerParseOptions(25, 0))
	if err != nil {
		t.Fatal(err)
	}
	if containerResult.Container.DefaultChild != "regions" || containerResult.FormatInfo["driver"] != "OpenFileGDB" {
		t.Fatalf("container result = %#v", containerResult)
	}
	readOptions := format.DefaultParseOptions()
	readOptions.GeometryEncoding = format.GeometryEncodingEWKB
	readOptions.ExtraParams = map[string]interface{}{"layer": "regions"}
	reader, err := readerProvider.OpenTableScopeReader(context.Background(), nil, contentio.Ref{}, readOptions)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if geometry, ok := rows[0]["SHAPE"].([]byte); !ok || string(geometry) != string([]byte{1, 2, 3}) {
		t.Fatalf("geometry = %#v, want EWKB bytes", rows[0]["SHAPE"])
	}
	if err := reader.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	targetPlan, err := workflowaccess.NewTargetPlan(workflowaccess.Target{
		Kind: workflowaccess.KindDirectory, Format: "filegdb", Name: "target.gdb", WriteMode: workflowaccess.WriteModeReplace,
		Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/target.gdb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writerProvider, err := plugin.BindScopeTableWriter(runtime, runtimeConn, targetPlan)
	if err != nil {
		t.Fatal(err)
	}
	spatial := datatype.NewSingleGeometrySpatialInfo("SHAPE", "MultiPolygon", 3424, 2)
	tableInfo := &datatype.TableInfo{Name: "regions", Fields: reader.Fields()}
	writer, err := writerProvider.OpenTableScopeWriter(context.Background(), nil, contentio.Ref{}, tableInfo, &format.WriteOptions{SpatialInfo: spatial})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRows(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	mutex.Lock()
	defer mutex.Unlock()
	batchParams := requests[operatorWriteBatch][0]["params"].(map[string]interface{})
	if batchParams["offset"] != float64(0) || batchParams["rows"] == nil {
		t.Fatalf("write batch params = %#v", batchParams)
	}
	closeParams := requests[operatorWriteClose][0]["params"].(map[string]interface{})
	if closeParams["expected_row_count"] != float64(1) {
		t.Fatalf("write close params = %#v, want expected_row_count=1", closeParams)
	}
	inspectParams := requests[operatorInspect][0]["params"].(map[string]interface{})
	if inspectParams["child_limit"] != float64(25) {
		t.Fatalf("inspect params = %#v, want child_limit=25", inspectParams)
	}
}

func runtimeConnectionInfo(t *testing.T, rawURL string) engineplugin.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return engineplugin.ConnectionInfo{"protocol": parsed.Scheme, "host": host, "port": port}
}

func TestReadOnlyPluginDoesNotImplementWriterFactory(t *testing.T) {
	plugin := NewReadOnlyPlugin(format.FormatDescriptor{ID: "test-pgeo", Format: format.FormatPGeo, DataType: datatype.Container, Layouts: []string{format.LayoutSingle}})
	if _, ok := interface{}(plugin).(format.RuntimeScopeTableWriterFactory); ok {
		t.Fatal("read-only PGeo plugin must not implement runtime writer factory")
	}
}
