package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func developQueryReadFixture(t *testing.T) DevelopQueryReadRequest {
	t.Helper()
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "people"))
	if err != nil {
		t.Fatal(err)
	}
	return DevelopQueryReadRequest{AuthorizationID: "8", ExecutionID: "b26d48a3-74e8-4339-a012-f29767b985ff", EngineID: "12", EngineVersion: "1", ReadSet: set, Attempt: 1, LeaseToken: "5441eed0-1caa-4229-8828-b84ff5c665f2"}
}

func TestDevelopQueryReadRequestBoundary(t *testing.T) {
	for _, scenario := range []string{"execution", "sample", "known-empty", "unknown", "omitted-paths", "mixed-mode", "no-claim", "other-engine", "duplicate", "noncanonical-id", "delegate"} {
		t.Run(scenario, func(t *testing.T) {
			request := developQueryReadFixture(t)
			switch scenario {
			case "sample":
				request.Attempt, request.LeaseToken, request.SampleCredential = 0, "", "addp_at_sample"
			case "known-empty":
				request.ReadSet, _ = plugin.NewQueryReadSet()
			case "unknown":
				request.ReadSet = nil
			case "omitted-paths":
				request.ReadSet = &plugin.QueryReadSet{}
			case "mixed-mode":
				request.SampleCredential = "addp_at_sample"
			case "no-claim":
				request.Attempt = 0
			case "other-engine":
				request.ReadSet.Paths[0].EngineID++
			case "duplicate":
				request.ReadSet.Paths = append(request.ReadSet.Paths, request.ReadSet.Paths[0])
			case "noncanonical-id":
				request.EngineID = "012"
			case "delegate":
				request.Attempt, request.LeaseToken, request.SampleCredential = 0, "", "addp_dat_sample"
			}
			valid := scenario == "execution" || scenario == "sample" || scenario == "known-empty"
			if (request.Validate() == nil) != valid {
				t.Fatal("incorrect request boundary", scenario)
			}
		})
	}
}

func TestDevelopQueryReadTransportIsUncachedAndDoesNotRetry(t *testing.T) {
	for _, status := range []int{200, 401, 403, 503, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			request := developQueryReadFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/engine-access/read-observations/develop-query" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer develop-service" || r.URL.RawQuery != "" {
					t.Error("incorrect fixed transport")
				}
				var actual DevelopQueryReadRequest
				if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || actual.Validate() != nil || actual.ExecutionID != request.ExecutionID || actual.LeaseToken != request.LeaseToken {
					t.Error("lost execution/source binding", err)
				}
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"observed_at":"2026-11-17T01:00:00Z","expires_at":"2026-11-17T01:01:00Z","error":"private source"}`))
			}))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("develop-service"), server.Client()).WithTenantID(3)
			for i := 0; i < 2; i++ {
				result, err := service.CheckDevelopQueryRead(context.Background(), request)
				if status == 200 {
					if err != nil || result == nil {
						t.Fatal(result, err)
					}
				} else {
					if result != nil || err == nil || strings.Contains(err.Error(), "private source") {
						t.Fatal("leaked/accepted failed observation", result, err)
					}
					if (status == 401 || status == 403) && !errors.Is(err, ErrDevelopQueryReadDenied) {
						t.Fatal(err)
					}
				}
			}
			if calls != 2 {
				t.Fatal("cached, retried or redirected source observation", calls)
			}
		})
	}
}
