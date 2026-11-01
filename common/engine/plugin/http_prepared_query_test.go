package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

type preparedCatalogFixture struct{ MockPlugin }

func (*preparedCatalogFixture) EngineCatalogModel() EngineCatalogModelSpec {
	return TabularCatalogModel(EngineCatalogTermSchema)
}

func TestPreparedHTTPQueryRetainsExactReadFactsAndExecutesOnce(t *testing.T) {
	Register(&preparedCatalogFixture{MockPlugin{TypeValue: "fixture_catalog"}})
	id := "9a21ab1a-2900-42a5-ae91-821339b3fcdd"
	gets, execs, closes := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing trusted caller")
		}
		switch {
		case r.Method == http.MethodDelete:
			closes++
			w.WriteHeader(204)
		case r.URL.Path == "/api/v1/queries":
			gets++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"query_id": id, "sources": []map[string]interface{}{{"engine_id": 11, "engine_type": "fixture_catalog"}}, "engine_read_scope": map[string]interface{}{"engine_ids": []uint{11}}, "read_set": map[string]interface{}{"paths": []EngineCatalogPath{TabularItemPath(11, "schema", " exact.schema ", " table.name ")}}})
		case r.URL.Path == "/api/v1/queries/"+id+"/execute":
			execs++
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 0 {
				t.Error("execute accepts no replacement SQL")
			}
			_, _ = w.Write([]byte(`{"columns":["id"],"fields":[{"name":"id","type":"bigint"}],"rows":[{"id":"9007199254740993"}]}`))
		default:
			t.Error("unexpected route")
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(endpoint.Port())
	prepared, err := HTTPPrepareFederatedQuery(t.Context(), ConnectionInfo{"protocol": "http", "host": endpoint.Hostname(), "port": port}, FederatedQueryRequest{Language: "sql", Query: "SELECT * FROM source", SourceEngineIDs: []uint{11}, CallerAccessToken: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if gets != 1 || execs != 0 {
		t.Fatal("prepare collected rows")
	}
	read, err := prepared.ReadSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if read.Paths[0].Segments[1].Name != " exact.schema " || read.Paths[0].Segments[2].Name != " table.name " {
		t.Fatal(read)
	}
	result, err := prepared.Execute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0]["id"] != "9007199254740993" {
		t.Fatal(result)
	}
	if _, err := prepared.Execute(t.Context()); err != ErrPreparedQueryConsumed {
		t.Fatal(err)
	}
	_ = prepared.Close(context.Background())
	_ = prepared.Close(context.Background())
	if execs != 1 || closes != 1 {
		t.Fatalf("execute=%d close=%d", execs, closes)
	}
}

func TestPreparedHTTPRejectsFactsOutsideFrozenSourcesAndReleasesSession(t *testing.T) {
	Register(&preparedCatalogFixture{MockPlugin{TypeValue: "fixture_catalog"}})
	for _, tc := range []string{"foreign_scope", "foreign_table", "missing_source", "duplicate_source"} {
		t.Run(tc, func(t *testing.T) {
			scope := &QueryEngineReadScope{EngineIDs: []uint{11}}
			paths := []EngineCatalogPath{TabularItemPath(11, "schema", "db", "table")}
			sources := []map[string]interface{}{{"engine_id": 11, "engine_type": "fixture_catalog"}}
			switch tc {
			case "foreign_scope":
				scope.EngineIDs = []uint{12}
			case "foreign_table":
				paths[0].EngineID = 12
			case "missing_source":
				sources = nil
			case "duplicate_source":
				sources = append(sources, sources[0])
			}
			closed := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					closed++
					w.WriteHeader(204)
					return
				}
				if r.URL.Path != "/api/v1/queries" {
					t.Error("rejected plan must never execute")
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"query_id": "9a21ab1a-2900-42a5-ae91-821339b3fcdd", "sources": sources, "engine_read_scope": scope, "read_set": &QueryReadSet{Paths: paths}})
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(endpoint.Port())
			_, err := HTTPPrepareFederatedQuery(t.Context(), ConnectionInfo{"protocol": "http", "host": endpoint.Hostname(), "port": port}, FederatedQueryRequest{Language: "sql", SourceEngineIDs: []uint{11}, CallerAccessToken: "fixture"})
			if err == nil || closed != 1 {
				t.Fatalf("error=%v close=%d", err, closed)
			}
		})
	}
}
