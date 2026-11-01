package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	commoni18n "github.com/addp/common/middleware/i18n"
)

func TestFederatedHTTPResultPreservesNativeSchemaAndNumericPrecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer private-fixture" || r.Header.Get("Accept-Language") != "en" || strings.Contains(string(body), "private-fixture") {
			t.Error("service credential or language boundary is invalid")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"columns":["id","amount","small"],"fields":[{"name":"id","type":"bigint"},{"name":"amount","type":"decimal","precision":38,"scale":18},{"name":"small","type":"int"}],"rows":[{"id":9007199254740993,"amount":100.123456789012345678,"small":1}]}`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(endpoint.Port())
	ctx, _ := commoni18n.WithLanguage(context.Background(), "en")
	result, err := HTTPExecuteFederatedQuery(ctx, ConnectionInfo{"protocol": "http", "host": endpoint.Hostname(), "port": port}, FederatedQueryRequest{CallerAccessToken: "private-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields[1].Precision != 38 || result.Fields[1].Scale != 18 {
		t.Fatalf("fields = %#v", result.Fields)
	}
	if result.Rows[0]["id"] != json.Number("9007199254740993") || result.Rows[0]["amount"] != json.Number("100.123456789012345678") {
		t.Fatalf("numbers = %#v", result.Rows)
	}
	rows, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["id"] != "9007199254740993" || rows[0]["amount"] != "100.123456789012345678" || rows[0]["small"] != json.Number("1") {
		t.Fatalf("typed public rows = %#v", rows)
	}
}

func TestFederatedHTTPResultRejectsMissingOrMismatchedFieldFacts(t *testing.T) {
	for _, body := range []string{
		`{"columns":["id"],"rows":[]}`,
		`{"columns":["id"],"fields":[{"name":"other","type":"bigint"}],"rows":[]}`,
		`{"columns":["id"],"fields":[{"name":"id","type":"invented"}],"rows":[]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		endpoint, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(endpoint.Port())
		_, err := HTTPExecuteFederatedQuery(context.Background(), ConnectionInfo{"protocol": "http", "host": endpoint.Hostname(), "port": port}, FederatedQueryRequest{CallerAccessToken: "private-fixture"})
		server.Close()
		if err == nil {
			t.Fatalf("invalid schema admitted: %s", body)
		}
	}
}
