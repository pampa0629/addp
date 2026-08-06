package supermap_workflow

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

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestSDXPostgreSQLTableProviderReadAndWriteSessions(t *testing.T) {
	var mutex sync.Mutex
	requests := make(map[string][]map[string]interface{})
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
		case operatorTableReadOpen:
			_, _ = w.Write([]byte(`{"status":"success","session_id":"read-1"}`))
		case operatorTableReadBatch:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"batch": map[string]interface{}{
					"fields": []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true}},
					"rows":   []map[string]interface{}{{"id": 7, "shape": []byte{1, 2, 3}}},
				},
			})
		case operatorTableWriteOpen:
			_, _ = w.Write([]byte(`{"status":"success","session_id":"write-1"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success"}`))
		}
	}))
	defer server.Close()

	runtimeConn := runtimeConnectionInfo(t, server.URL)
	provider, err := NewSDXPostgreSQLTableProvider(plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow"), runtimeConn)
	if err != nil {
		t.Fatalf("NewSDXPostgreSQLTableProvider() error = %v", err)
	}
	databaseConn := plugin.ConnectionInfo{"host": "postgres", "port": 5432, "database": "business", "user": "business"}
	path := plugin.CatalogPath{Segments: []plugin.CatalogSegment{{Name: "sdx"}, {Name: "roads"}}}

	readSession, err := provider.OpenTableReadSession(context.Background(), databaseConn, path, plugin.TableReadSessionOptions{})
	if err != nil {
		t.Fatalf("OpenTableReadSession() error = %v", err)
	}
	batch, err := readSession.ReadBatch(context.Background(), 128)
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if got, ok := batch.Rows[0]["shape"].([]byte); !ok || string(got) != string([]byte{1, 2, 3}) {
		t.Fatalf("geometry = %#v, want EWKB bytes", batch.Rows[0]["shape"])
	}
	if err := readSession.Close(context.Background()); err != nil {
		t.Fatalf("read Close() error = %v", err)
	}

	spatial := datatype.NewSingleGeometrySpatialInfo("shape", "multipolygon", 4549, 2)
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true}}
	if err := provider.PrepareTableWrite(context.Background(), databaseConn, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatial}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}
	writeSession, err := provider.OpenTableWriteSession(context.Background(), databaseConn, path, plugin.TableWriteSessionOptions{Fields: fields, SpatialInfo: spatial, Replace: true})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	if err := writeSession.WriteBatch(context.Background(), batch); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := writeSession.Close(context.Background()); err != nil {
		t.Fatalf("write Close() error = %v", err)
	}

	mutex.Lock()
	defer mutex.Unlock()
	openParams := requests[operatorTableWriteOpen][0]["params"].(map[string]interface{})
	if openParams["replace"] != true || openParams["protocol"] != SuperMapTableBatchProtocol {
		t.Fatalf("write_open params = %#v", openParams)
	}
	writeParams := requests[operatorTableWriteBatch][0]["params"].(map[string]interface{})
	if writeParams["session_id"] != "write-1" || writeParams["batch"] == nil {
		t.Fatalf("write_batch params = %#v", writeParams)
	}
}

func TestSuperMapTablePathRejectsNonSDXSchema(t *testing.T) {
	_, _, err := superMapTablePath(plugin.CatalogPath{Segments: []plugin.CatalogSegment{{Name: "public"}, {Name: "roads"}}})
	if err == nil || !strings.Contains(err.Error(), "must be sdx") {
		t.Fatalf("superMapTablePath() error = %v, want fixed sdx schema error", err)
	}
}

func TestSDXPostgreSQLTableProviderDescribesSDKCatalogFacts(t *testing.T) {
	var mutex sync.Mutex
	var operators []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/operators/"), "/invoke")
		mutex.Lock()
		operators = append(operators, operator)
		mutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch operator {
		case operatorTableReadOpen:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "success",
				"session_id": "facts-1",
				"fields": []datatype.FieldInfo{
					{Name: "id", Type: datatype.FieldTypeInt},
					{Name: "SmGeometry", Type: datatype.FieldTypeGeometry, Nullable: true},
				},
				"spatial":   datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("SmGeometry", "geometry", 4326, 2)),
				"row_count": 7,
			})
		default:
			_, _ = w.Write([]byte(`{"status":"success"}`))
		}
	}))
	defer server.Close()

	provider, err := NewSDXPostgreSQLTableProvider(plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow"), runtimeConnectionInfo(t, server.URL))
	if err != nil {
		t.Fatalf("NewSDXPostgreSQLTableProvider() error = %v", err)
	}
	path := plugin.CatalogPath{Segments: []plugin.CatalogSegment{{Name: "sdx"}, {Name: "regions"}}}
	facts, err := provider.DescribeCatalogFacts(context.Background(), plugin.ConnectionInfo{
		"host": "postgres", "port": 5432, "database": "business", "user": "business",
	}, path, plugin.CatalogFactsOptions{IncludeSpatialFacts: true})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 7 {
		t.Fatalf("table facts = %#v, want row_count=7", facts.Table)
	}
	if len(facts.Table.Fields) != 2 || facts.Table.Fields[1].Name != "SmGeometry" || facts.Table.Fields[1].Type != datatype.FieldTypeGeometry {
		t.Fatalf("table fields = %#v, want virtual SmGeometry", facts.Table.Fields)
	}
	if facts.Spatial == nil || facts.Spatial.PrimaryGeometryColumn != "SmGeometry" {
		t.Fatalf("spatial facts = %#v, want primary SmGeometry", facts.Spatial)
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := []string{operatorTableReadOpen, operatorTableReadClose}
	if len(operators) != len(want) || operators[0] != want[0] || operators[1] != want[1] {
		t.Fatalf("operators = %#v, want %#v", operators, want)
	}
}

func TestSDXPostgreSQLWriteSessionCanAbortAfterCloseFailure(t *testing.T) {
	var mutex sync.Mutex
	var operators []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/operators/"), "/invoke")
		mutex.Lock()
		operators = append(operators, operator)
		mutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch operator {
		case operatorTableWriteOpen:
			_, _ = w.Write([]byte(`{"status":"success","session_id":"write-close-failure"}`))
		case operatorTableWriteClose:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"failed","error":"spatial index verification failed"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success"}`))
		}
	}))
	defer server.Close()

	provider, err := NewSDXPostgreSQLTableProvider(plugin.NewHTTPWorkflowRuntimeProvider("supermap_workflow", "SuperMap Workflow"), runtimeConnectionInfo(t, server.URL))
	if err != nil {
		t.Fatalf("NewSDXPostgreSQLTableProvider() error = %v", err)
	}
	path := plugin.CatalogPath{Segments: []plugin.CatalogSegment{{Name: "sdx"}, {Name: "roads"}}}
	session, err := provider.OpenTableWriteSession(context.Background(), plugin.ConnectionInfo{
		"host": "postgres", "port": 5432, "database": "business", "user": "business",
	}, path, plugin.TableWriteSessionOptions{})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	if err := session.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "spatial index verification failed") {
		t.Fatalf("Close() error = %v, want spatial index verification failure", err)
	}
	if err := session.Abort(context.Background()); err != nil {
		t.Fatalf("Abort() after failed Close() error = %v", err)
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := []string{operatorTableWriteOpen, operatorTableWriteClose, operatorTableWriteAbort}
	if len(operators) != len(want) {
		t.Fatalf("operators = %#v, want %#v", operators, want)
	}
	for index := range want {
		if operators[index] != want[index] {
			t.Fatalf("operators = %#v, want %#v", operators, want)
		}
	}
}

func runtimeConnectionInfo(t *testing.T, rawURL string) plugin.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return plugin.ConnectionInfo{"protocol": parsed.Scheme, "host": parsed.Hostname(), "port": port}
}
