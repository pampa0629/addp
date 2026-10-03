package elasticsearch

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func conn(endpoint string) plugin.ConnectionInfo {
	return plugin.ConnectionInfo{"endpoint": endpoint, "user": "reader", "password": "secret"}
}
func TestCapabilitiesAndPath(t *testing.T) {
	p := &ElasticsearchPlugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"*", "a,b", "alias:orders", ".system", "A", "orders/x"} {
		path := p.indexEntry(1, name).Path
		if _, err := indexName(path); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	path := p.indexEntry(1, "orders.v1").Path
	name, err := indexName(path)
	if err != nil || name != "orders.v1" || len(path.Segments) != 2 {
		t.Fatal(path, err)
	}
	for _, endpoint := range []string{"ftp://localhost", "http://user:secret@localhost", "http://localhost/foo", "http://localhost?x=1"} {
		if err := p.ValidateConnectionInfo(conn(endpoint)); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
}
func TestDSLRejectsUnprovableReadsAndUnsafeOptions(t *testing.T) {
	for _, raw := range []string{
		`{"query":{"terms":{"id":{"index":"private","id":"1","path":"ids"}}}}`,
		`{"query":{"script":{"script":"true"}}}`, `{"runtime_mappings":{}}`, `{"aggs":{}}`, `{"pit":{"id":"other"}}`, `{"search_after":[1]}`, `{"query":{"bool":{"must":[{"match_all":{}},{"percolate":{}}]}}}`, `{"_source":["customer.*"]}`, `{"sort":[{"id":{"order":"asc","nested":{"path":"private"}}}]}`, `{"query":{"range":{"id":{"gte":{"script":"x"}}}}}`, `{} {}`, `null`,
	} {
		if _, err := decodeDSL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"query":{"match_all":{}},"size":10}`, `{"query":{"bool":{"must":[{"term":{"tags":"sample"}},{"range":{"order_id":{"gte":9007199254740993}}}],"must_not":{"exists":{"field":"optional"}}}},"sort":[{"order_id":"desc"}],"_source":["customer","items"]}`} {
		if _, err := decodeDSL(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
}
func TestHTTPSAuthenticationAndCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "reader" || p != "secret" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		fmt.Fprint(w, `{"indices":[]}`)
	}))
	defer server.Close()
	p := &ElasticsearchPlugin{}
	c := conn(server.URL)
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("untrusted CA accepted")
	}
	c["tls_ca_cert"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if _, err := x509.ParseCertificate(server.Certificate().Raw); err != nil {
		t.Fatal(err)
	}
	if err := p.TestConnection(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c["password"] = "wrong"
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("bad auth accepted")
	}
}
func TestPITPaginationPartialFailureAndCleanup(t *testing.T) {
	for _, failure := range []string{"", "partial", "timeout", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			searches, closes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				switch {
				case strings.Contains(r.URL.Path, "_resolve/index"):
					fmt.Fprint(w, `{"indices":[{"name":"orders.v1","attributes":["open"]}]}`)
				case strings.Contains(r.URL.Path, "_settings/index.uuid"):
					fmt.Fprint(w, `{"orders.v1":{"settings":{"index.uuid":"test-uuid"}}}`)
				case r.URL.Path == "/orders.v1/_pit":
					fmt.Fprint(w, `{"id":"pit-1","_shards":{"failed":0}}`)
				case r.Method == "DELETE" && r.URL.Path == "/_pit":
					var body map[string]string
					json.NewDecoder(r.Body).Decode(&body)
					if searches > 0 && body["id"] != "pit-2" {
						t.Error("latest PIT not closed", body)
					}
					closes++
					fmt.Fprint(w, `{"succeeded":true}`)
				case r.URL.Path == "/_search":
					searches++
					var body map[string]interface{}
					json.NewDecoder(r.Body).Decode(&body)
					if searches == 2 {
						if body["search_after"] == nil {
							t.Error("missing search_after")
						}
						fmt.Fprint(w, `{"pit_id":"pit-2","hits":{"hits":[]}}`)
						return
					}
					if failure == "partial" {
						fmt.Fprint(w, `{"pit_id":"pit-2","_shards":{"failed":1}}`)
						return
					}
					if failure == "timeout" {
						fmt.Fprint(w, `{"pit_id":"pit-2","timed_out":true}`)
						return
					}
					fmt.Fprint(w, `{"pit_id":"pit-2","_shards":{"failed":0},"hits":{"hits":[{"_index":"orders.v1","_source":{"id":9007199254740993,"items":[{"sku":"a"}],"optional":null},"sort":[1]}]}}`)
				default:
					t.Error("unexpected request", r.Method, r.URL)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			p := &ElasticsearchPlugin{}
			s, err := p.OpenRecordReadSession(context.Background(), conn(server.URL), p.indexEntry(1, "orders.v1").Path, plugin.RecordReadSessionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if failure == "cancel" {
				cancel()
			}
			defer cancel()
			batch, err := s.ReadBatch(ctx, 1)
			if failure != "" {
				if err == nil {
					t.Fatal("failure accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if batch.Records[0]["id"] != "9007199254740993" {
					t.Fatal(batch)
				}
				if _, exists := batch.Records[0]["missing"]; exists {
					t.Fatal("missing replaced")
				}
				next, err := s.ReadBatch(ctx, 1)
				if err != nil || len(next.Records) != 0 {
					t.Fatal(next, err)
				}
			}
			if err := s.Close(ctx); err != nil {
				t.Fatal(err)
			}
			s.Close(ctx)
			if closes != 1 {
				t.Fatalf("close count %d", closes)
			}
		})
	}
}
func TestIntegrationElasticsearch(t *testing.T) {
	if os.Getenv("ADDP_ELASTICSEARCH_INTEGRATION") != "1" {
		t.Skip("owned Elasticsearch T2 required")
	}
	p := &ElasticsearchPlugin{}
	c := plugin.ConnectionInfo{"endpoint": os.Getenv("ELASTICSEARCH_ENDPOINT"), "user": os.Getenv("ELASTICSEARCH_READER_USER"), "password": os.Getenv("ELASTICSEARCH_READER_PASSWORD")}
	ctx := context.Background()
	if err := p.TestConnection(ctx, c); err != nil {
		t.Fatal(err)
	}
	wrong := conn(os.Getenv("ELASTICSEARCH_ENDPOINT"))
	if err := p.TestConnection(ctx, wrong); err == nil {
		t.Fatal("wrong password accepted")
	}
	entries, err := p.ListChildren(ctx, c, plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 91), plugin.ListOptions{})
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	path := p.indexEntry(91, "addp_orders.v1").Path
	facts, err := p.DescribeEngineCatalogFacts(ctx, c, path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true})
	if err != nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 25 {
		t.Fatal(facts, err)
	}
	empty, err := p.DescribeEngineCatalogFacts(ctx, c, p.indexEntry(91, "addp_empty.v1").Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true})
	if err != nil || len(empty.Table.Fields) == 0 || *empty.Table.RowCount != 0 {
		t.Fatal(empty, err)
	}
	prepared, err := p.PrepareQuery(ctx, c, plugin.QueryRequest{EngineID: 91, Language: "es_dsl", TargetPath: &path, Query: `{"query":{"term":{"tags":"sample"}},"size":25,"sort":[{"order_id":"asc"}]}`})
	if err != nil {
		t.Fatal(err)
	}
	path.Segments[1].Name = "forbidden"
	c["password"] = "wrong" // Plan must be immutable.
	reads, err := prepared.ReadSet(ctx)
	if err != nil || len(reads.Paths) != 1 || reads.Paths[0].Segments[1].Name != "addp_orders.v1" {
		t.Fatal(reads, err)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil || !lineage.Sources[0].IdentityOutput {
		t.Fatal(lineage, err)
	}
	analysis, err := prepared.Analysis(ctx)
	if err != nil || analysis.SchemaCoverage != plugin.QuerySchemaCoverageUnknown {
		t.Fatal(analysis, err)
	}
	result, err := prepared.Execute(ctx)
	if err != nil || len(result.Rows) != 25 {
		t.Fatal(result, err)
	}
	if result.Rows[0]["order_id"] != "9007199254740993" {
		t.Fatal(result.Rows[0])
	}
	if _, err := prepared.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatal(err)
	}
	c["password"] = os.Getenv("ELASTICSEARCH_READER_PASSWORD")
	client, err := p.client(c)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient(client)
	if err = request(ctx, client, "PUT", "/addp_forbidden", map[string]interface{}{}, nil); err == nil {
		t.Fatal("reader can write")
	}
	session, err := p.OpenRecordReadSession(ctx, c, p.indexEntry(91, "addp_orders.v1").Path, plugin.RecordReadSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for {
		batch, err := session.ReadBatch(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Records) == 0 {
			break
		}
		for _, row := range batch.Records {
			key := row["order_id"].(string)
			if seen[key] {
				t.Fatal("duplicate", key)
			}
			seen[key] = true
		}
	}
	if len(seen) != 25 {
		t.Fatal(seen)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	admin := plugin.ConnectionInfo{"endpoint": os.Getenv("ELASTICSEARCH_ENDPOINT"), "user": "elastic", "password": os.Getenv("ELASTICSEARCH_PASSWORD")}
	adminClient, err := p.client(admin)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient(adminClient)
	var stats struct {
		Nodes map[string]struct {
			Indices struct {
				Search struct {
					Open int `json:"open_contexts"`
				} `json:"search"`
			} `json:"indices"`
		} `json:"nodes"`
	}
	if err = request(ctx, adminClient, "GET", "/_nodes/stats/indices/search", nil, &stats); err != nil {
		t.Fatal(err)
	}
	for _, node := range stats.Nodes {
		if node.Indices.Search.Open != 0 {
			t.Fatal("PIT context leaked", node)
		}
	}
}

func TestAnalysisDoesNotNeedCredentialsAndReadSetUsesExactIndex(t *testing.T) {
	p := &ElasticsearchPlugin{}
	path := p.indexEntry(91, "orders.v1").Path
	prepared, err := p.PrepareQuery(context.Background(), nil, plugin.QueryRequest{EngineID: 91, Language: "es_dsl", TargetPath: &path, Query: `{"query":{"match_all":{}}}`})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := prepared.Analysis(context.Background())
	if err != nil || analysis.SchemaCoverage != plugin.QuerySchemaCoverageUnknown {
		t.Fatal(analysis, err)
	}
	reads, err := prepared.ReadSet(context.Background())
	if err != nil || reads.Paths[0].Segments[1].Name != "orders.v1" {
		t.Fatal(reads, err)
	}
}

func TestPITRejectsNativeIndexReplacementAndClosesContext(t *testing.T) {
	settings, closed := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		switch {
		case strings.Contains(r.URL.Path, "_resolve/index"):
			fmt.Fprint(w, `{"indices":[{"name":"orders.v1","attributes":["open"]}]}`)
		case strings.Contains(r.URL.Path, "_settings/index.uuid"):
			settings++
			fmt.Fprintf(w, `{"orders.v1":{"settings":{"index.uuid":"uuid-%d"}}}`, settings)
		case r.URL.Path == "/orders.v1/_pit":
			fmt.Fprint(w, `{"id":"pit-1"}`)
		case r.Method == "DELETE" && r.URL.Path == "/_pit":
			closed++
			fmt.Fprint(w, `{"succeeded":true}`)
		default:
			t.Error("unexpected read", r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	p := &ElasticsearchPlugin{}
	if _, err := p.OpenRecordReadSession(context.Background(), conn(server.URL), p.indexEntry(91, "orders.v1").Path, plugin.RecordReadSessionOptions{}); err == nil {
		t.Fatal("replaced index accepted")
	}
	if closed != 1 {
		t.Fatal("PIT leaked", closed)
	}
}

func TestMappingFactsSupportObjectProtectionWithoutInventingArrayShape(t *testing.T) {
	var fields []datatype.FieldInfo
	mappingFields(map[string]interface{}{"customer": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "keyword"}}}}, nil, false, &fields)
	component := dataprotection.Component{Key: "customer.name", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "customer", Container: "object"}, {Name: "name", Container: "scalar"}}}
	if _, err := dataprotection.ComponentSchemaFingerprint(fields, component); err != nil {
		t.Fatal(err)
	}
	row := map[string]interface{}{"customer": map[string]interface{}{"name": "customer-0"}}
	result := &plugin.QueryResult{Columns: []string{"customer"}, Rows: []map[string]interface{}{row}}
	source := plugin.QueryOutputSource{Fields: fields, IdentityOutput: true}
	rules := []dataprotection.Rule{{Action: "preview", Component: component, Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress}}}
	if err := dataprotection.ProtectQueryResultSource(result, source, "preview", rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatal(err)
	}
	if _, exists := row["customer"].(map[string]interface{})["name"]; exists {
		t.Fatal("protected field leaked")
	}
	component.Path[0].Container = "array"
	if _, err := dataprotection.ComponentSchemaFingerprint(fields, component); err == nil {
		t.Fatal("Mapping was treated as proof of source array shape")
	}
}
