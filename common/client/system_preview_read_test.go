package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestManagerPreviewReadUsesCurrentCredentialAndCompleteTargets(t *testing.T) {
	targets := []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "C"), plugin.TabularItemPath(12, "schema", "public", "D")}
	calls := 0
	currentToken := "addp_at_user"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/engine-access/read-checks/manager-preview" || r.URL.RawQuery != "" ||
			r.Header.Get("Authorization") != "Bearer "+currentToken || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("unexpected preview check contract: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
			t.Fatalf("unexpected self-reported authority in request: %v", err)
		}
		var got []plugin.EngineCatalogPath
		if err := json.Unmarshal(body["targets"], &got); err != nil || !reflect.DeepEqual(got, targets) {
			t.Errorf("incomplete read set: %+v %v", got, err)
		}
		_, _ = w.Write([]byte(`{"observed_at":"2026-10-05T12:00:00Z"}`))
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("must-not-be-used"), server.Client())
	for _, token := range []string{"addp_at_user", "addp_dat_tool"} {
		currentToken = token
		for i := 0; i < 2; i++ {
			if err := c.CheckManagerPreviewRead(context.Background(), token, targets); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 4 {
		t.Fatalf("check was cached: calls=%d", calls)
	}
	for _, token := range []string{"", "addp_at_", "addp_dat_", "machine", "addp_at_user extra"} {
		if err := c.CheckManagerPreviewRead(context.Background(), token, targets); !errors.Is(err, ErrManagerPreviewCredentialRejected) {
			t.Fatalf("invalid token accepted: %v", err)
		}
	}
	for _, invalid := range [][]plugin.EngineCatalogPath{nil, {{}}, make([]plugin.EngineCatalogPath, 201)} {
		if err := c.CheckManagerPreviewRead(context.Background(), "addp_at_user", invalid); !errors.Is(err, ErrManagerPreviewReadDenied) {
			t.Fatalf("invalid targets accepted: %v", err)
		}
	}
	if calls != 4 {
		t.Fatal("invalid input sent to System")
	}
}

func TestManagerPreviewReadFailsClosedWithoutRetryOrDiagnosticLeak(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unauthorized", 401, `{"error":"secret addp_at_user"}`, ErrManagerPreviewCredentialRejected},
		{"forbidden", 403, `{"error":"native source details"}`, ErrManagerPreviewReadDenied},
		{"not found", 404, `{}`, ErrManagerPreviewReadUnavailable},
		{"unavailable", 503, `{}`, ErrManagerPreviewReadUnavailable},
		{"missing observation", 200, `{}`, ErrManagerPreviewReadUnavailable},
		{"bad JSON", 200, `not JSON`, ErrManagerPreviewReadUnavailable},
		{"oversized response", 200, strings.Repeat(" ", 4097), ErrManagerPreviewReadUnavailable},
		{"wrong success status", 201, `{"observed_at":"2026-10-05T12:00:00Z"}`, ErrManagerPreviewReadUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("unused-machine"), server.Client())
			err := c.CheckManagerPreviewRead(context.Background(), "addp_at_user", []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "C")})
			if !errors.Is(err, test.want) || err.Error() != test.want.Error() || calls != 1 {
				t.Fatalf("unsafe failure: %v calls=%d", err, calls)
			}
		})
	}
}

func TestManagerPreviewReadRejectsRedirectWithoutForwardingCredential(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "/another-operation")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	httpClient := server.Client()
	c := NewSystemServiceClient(server.URL, nil, httpClient)
	err := c.CheckManagerPreviewRead(context.Background(), "addp_dat_tool", []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "C")})
	if !errors.Is(err, ErrManagerPreviewReadUnavailable) || calls != 1 || httpClient.CheckRedirect != nil {
		t.Fatalf("redirect followed or shared client mutated: %v calls=%d", err, calls)
	}
}
