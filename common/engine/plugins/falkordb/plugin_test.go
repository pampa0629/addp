package falkordb

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

func testConnection() plugin.ConnectionInfo {
	return plugin.ConnectionInfo{"host": "127.0.0.1", "password": "test-secret"}
}
func TestFalkorDBContracts(t *testing.T) {
	p := &FalkorDBPlugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	path := p.graphPath(9, "业务.图")
	if got := resourcetree.ParseFullNamePath(p.Type(), "graph", "业务.图/a"); len(got) != 1 || got[0] != "业务.图/a" {
		t.Fatalf("opaque name split: %v", got)
	}
	loc := &resourcetree.ResourceLocator{EngineID: 9, Path: []string{"业务.图"}, Type: resourcetree.TypeGraph}
	converted, err := resourcetree.EngineCatalogPathFromLocator(p.EngineCatalogModel(), loc)
	if err != nil {
		t.Fatal(err)
	}
	if name, err := p.graphName(converted); err != nil || name != "业务.图" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	for _, bad := range []plugin.EngineCatalogPath{{}, plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 9), {Version: path.Version, EngineID: 9, Segments: path.Segments[1:]}} {
		if _, err := p.graphName(bad); err == nil {
			t.Fatal("accepted malformed graph path")
		}
	}
	if _, err = p.ExecuteGraphQuery(t.Context(), testConnection(), "MATCH (n) RETURN n", plugin.QueryOptions{}); err == nil {
		t.Fatal("silently selected graph")
	}
}
func TestFalkorDBReadProofRejectsEffectsBeforeConnecting(t *testing.T) {
	p := &FalkorDBPlugin{}
	path := p.graphPath(9, "social")
	for _, query := range []string{"CREATE (n)", "CALL db.labels()", "LOAD CSV FROM 'https://example.invalid' AS r RETURN r", "RETURN custom.fetch()", "RETURN startswith('a','b')", "RETURN endswith('a','b')", "RETURN `count.evil`(1)", "RETURN 1; RETURN 2", "USE remote MATCH(n) RETURN n"} {
		_, err := p.PrepareGraphQuery(t.Context(), testConnection(), plugin.QueryRequest{EngineID: 9, Language: "cypher", TargetPath: &path, Query: query})
		if plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeLocalReadUnsupported {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for _, query := range []string{"MATCH (n) RETURN n", "MATCH (n) RETURN count(n)", "MATCH (n) RETURN typeOf(n.name)", "RETURN [x IN [1,2] WHERE x>1]"} {
		if _, err := p.PrepareGraphQuery(t.Context(), testConnection(), plugin.QueryRequest{EngineID: 9, Language: "cypher", TargetPath: &path, Query: query}); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
}
func TestFalkorDBPreparedTargetIsImmutable(t *testing.T) {
	p := &FalkorDBPlugin{}
	path := p.graphPath(9, "social")
	connection := testConnection()
	params := map[string]interface{}{"row_limit": int64(10)}
	conn, name, query, values, _, err := p.prepare(connection, plugin.QueryRequest{EngineID: 9, Language: "cypher", TargetPath: &path, Query: "MATCH(n) RETURN n LIMIT $row_limit", Options: plugin.QueryOptions{Limit: 3, Parameters: params}})
	if err != nil {
		t.Fatal(err)
	}
	path.Segments[1].Name = "changed"
	connection["host"] = "changed"
	params["row_limit"] = int64(0)
	if name != "social" || conn["host"] != "127.0.0.1" || values["row_limit"] != int64(10) || query != "MATCH(n) RETURN n LIMIT 3" {
		t.Fatal("plan inputs retained caller aliases")
	}
}
func TestFalkorDBParametersAreTypedAndFrozen(t *testing.T) {
	values := []int64{1, 2}
	original := map[string]interface{}{"list": values, "big": json.Number("9007199254740993"), "text": "\" RETURN 1 //中文"}
	params, err := normalizeParameters(original)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 99
	if params["list"].([]interface{})[0] != int64(1) || params["big"] != int64(9007199254740993) {
		t.Fatal(params)
	}
	for _, bad := range []map[string]interface{}{{"x y": 1}, {"x": math.NaN()}, {"x": uint64(math.MaxUint64)}, {"x": map[string]interface{}{"bad-key": 1}}} {
		if _, err := normalizeParameters(bad); err == nil {
			t.Fatal("invalid parameter accepted")
		}
	}
}
func TestFalkorDBConnectionValidation(t *testing.T) {
	p := &FalkorDBPlugin{}
	for _, bad := range []plugin.ConnectionInfo{{"host": "localhost"}, {"host": "http://localhost", "password": "p"}, {"host": "localhost", "password": "p", "database": -1}, {"host": "localhost", "password": "p", "port": 1.5}, {"host": "localhost", "password": "p", "tls_ca_cert": "not PEM"}} {
		if err := p.ValidateConnectionInfo(bad); err == nil {
			t.Fatal("invalid connection accepted")
		}
	}
}
