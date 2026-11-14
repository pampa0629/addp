package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestManagerObjectPreviewUsesFixedOriginalCredentialAndStrictObservation(t *testing.T) {
	target := plugin.FileItemPath(14, "images/private.tif")
	for _, tc := range []struct {
		name, corrupt string
		status        int
		want          error
	}{
		{"hidden", "hidden", 200, nil}, {"metadata only", "", 200, nil}, {"read", "read", 200, nil},
		{"wrong target", "target", 200, ErrMetadataQueryUnavailable}, {"missing result", "missing", 200, ErrMetadataQueryUnavailable},
		{"hidden fingerprint", "hidden-fp", 200, ErrMetadataQueryUnavailable}, {"hidden readable", "hidden-read", 200, ErrMetadataQueryUnavailable},
		{"bad fingerprint", "fp", 200, ErrMetadataQueryUnavailable}, {"no time", "time", 200, ErrMetadataQueryUnavailable},
		{"unauthorized", "", 401, ErrMetadataCredentialRejected}, {"forbidden", "", 403, ErrMetadataQueryDenied},
		{"outage", "", 503, ErrMetadataQueryUnavailable}, {"redirect", "", 307, ErrMetadataQueryUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/metadata-checks/manager-object-preview" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer addp_dat_preview" {
					t.Error("wrong preview credential/route")
				}
				var request authorization.SourceMetadataCheckRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil || !request.ValidObjectPreview() {
					t.Fatal("invalid request")
				}
				result := authorization.SourceObjectPreviewCheck{ObservedAt: time.Now(), Targets: []authorization.SourceVisibilityTargetCheck{{SourceMetadataTargetCheck: authorization.SourceMetadataTargetCheck{Target: target, Visible: true}, ItemFingerprint: strings.Repeat("a", 64)}}}
				switch tc.corrupt {
				case "hidden":
					result.Targets[0].Visible = false
					result.Targets[0].ItemFingerprint = ""
				case "read":
					result.Targets[0].ContentReadable = true
				case "target":
					result.Targets[0].Target.EngineID++
				case "missing":
					result.Targets = nil
				case "hidden-fp":
					result.Targets[0].Visible = false
				case "hidden-read":
					result.Targets[0].Visible = false
					result.Targets[0].ItemFingerprint = ""
					result.Targets[0].ContentReadable = true
				case "fp":
					result.Targets[0].ItemFingerprint = "private"
				case "time":
					result.ObservedAt = time.Time{}
				}
				w.Header().Set("Location", "/never")
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("must-not-substitute"), server.Client())
			for i := 1; i <= 2; i++ {
				result, err := c.CheckManagerObjectPreview(t.Context(), "addp_dat_preview", []plugin.EngineCatalogPath{target})
				if !errors.Is(err, tc.want) || calls != i || tc.want != nil && result != nil {
					t.Fatalf("err=%v calls=%d result=%+v", err, calls, result)
				}
			}
			for _, targets := range [][]plugin.EngineCatalogPath{nil, {target, target}, {plugin.FileDirectoryPath(14, "images")}, {plugin.TabularItemPath(2, "schema", "public", "people")}} {
				if _, err := c.CheckManagerObjectPreview(t.Context(), "addp_at_user", targets); !errors.Is(err, ErrMetadataQueryDenied) {
					t.Fatal("non-single-leaf accepted", err)
				}
			}
			if _, err := c.CheckManagerObjectPreview(t.Context(), "addp_sat_service", []plugin.EngineCatalogPath{target}); !errors.Is(err, ErrMetadataCredentialRejected) {
				t.Fatal("machine substituted", err)
			}
			if calls != 2 {
				t.Fatal("invalid input reached transport")
			}
		})
	}
}

func TestNavigationClientRetainsUserAndBindsEveryNode(t *testing.T) {
	target := plugin.ObjectRootPath(9)
	query := authorization.SourceNavigationCheckRequest{Targets: []plugin.EngineCatalogPath{target}}
	for _, mode := range []string{"ok", "wrong-target", "missing", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/metadata-checks/manager-navigation" || r.Header.Get("Authorization") != "Bearer addp_at_user" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("navigation identity replaced")
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				result := authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: target, Visible: true}}}
				if mode == "wrong-target" {
					result.Targets[0].Target.EngineID = 10
				}
				if mode == "missing" {
					result.Targets = nil
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			client := &SystemServiceClient{baseURL: server.URL, httpClient: server.Client()}
			got, err := client.CheckManagerNavigation(t.Context(), "addp_at_user", query)
			if (mode == "ok") != (err == nil) {
				t.Fatal(got, err)
			}
		})
	}
}
