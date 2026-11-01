package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

func TestMetadataQueryUsesExactCurrentUserAndRejectsBrokenObservation(t *testing.T) {
	targets := []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "C"), plugin.TabularItemPath(12, "schema", "public", "D")}
	for _, tc := range []struct {
		name    string
		status  int
		corrupt string
		want    error
	}{
		{"mixed visibility", 200, "", nil},
		{"wrong target", 200, "target", ErrMetadataQueryUnavailable},
		{"missing target", 200, "missing", ErrMetadataQueryUnavailable},
		{"missing time", 200, "time", ErrMetadataQueryUnavailable},
		{"unauthorized", 401, "", ErrMetadataCredentialRejected},
		{"forbidden", 403, "", ErrMetadataQueryDenied},
		{"dependency", 503, "", ErrMetadataQueryUnavailable},
		{"redirect", 307, "", ErrMetadataQueryUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/metadata-checks/meta-query" || (r.Header.Get("Authorization") != "Bearer addp_at_user" && r.Header.Get("Authorization") != "Bearer addp_dat_tool") || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("Cookie") != "" {
					t.Error("unexpected identity or route")
				}
				var request authorization.SourceMetadataCheckRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil || !request.Valid() {
					t.Error("bad request")
				}
				result := authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: targets[0], Visible: true}, {Target: targets[1], Visible: false}}}
				switch tc.corrupt {
				case "target":
					result.Targets[0].Target = targets[1]
				case "missing":
					result.Targets = result.Targets[:1]
				case "time":
					result.ObservedAt = time.Time{}
				}
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			httpClient := server.Client()
			c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("never-used"), httpClient)
			for i := 0; i < 2; i++ {
				token := "addp_at_user"
				if i == 1 {
					token = "addp_dat_tool"
				}
				got, err := c.CheckMetaMetadataRead(t.Context(), token, targets)
				if !errors.Is(err, tc.want) || calls != i+1 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
				if tc.want == nil && (!got.Targets[0].Visible || got.Targets[1].Visible) {
					t.Fatal("visibility changed")
				}
				if tc.want != nil && got != nil {
					t.Fatal("partial result escaped")
				}
			}
			if httpClient.CheckRedirect != nil {
				t.Fatal("mutated client")
			}
			for _, token := range []string{"", "addp_at_", "addp_sat_service", "addp_dat_", "addp_at_user extra"} {
				if _, err := c.CheckMetaMetadataRead(t.Context(), token, targets); !errors.Is(err, ErrMetadataCredentialRejected) {
					t.Fatal(err)
				}
			}
			if _, err := c.CheckMetaMetadataRead(t.Context(), "addp_at_user", []plugin.EngineCatalogPath{targets[0], targets[0]}); !errors.Is(err, ErrMetadataQueryDenied) {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal("invalid request sent")
			}
		})
	}
}

func TestLineageVisibilityUsesFixedPurposeAndRejectsDelegates(t *testing.T) {
	targets := []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", "public", "C")}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/metadata-checks/meta-lineage" || r.Header.Get("Authorization") != "Bearer addp_at_user" {
			t.Error("lineage identity/purpose changed")
		}
		json.NewEncoder(w).Encode(authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: targets[0], Visible: true}}})
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("never-used"), server.Client())
	if result, err := c.CheckMetaLineageRead(t.Context(), "addp_at_user", targets); err != nil || !result.Targets[0].Visible {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, token := range []string{"addp_dat_tool", "addp_sat_machine", "addp_at_user extra"} {
		if _, err := c.CheckMetaLineageRead(t.Context(), token, targets); !errors.Is(err, ErrMetadataCredentialRejected) {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("unexpected requests: %d", calls)
	}
}
