package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

func developOwnerFacts(t *testing.T) *DevelopFileExecutionFacts {
	t.Helper()
	raw := json.RawMessage(`{"value":9007199254740993}`)
	intent, err := execution.NewDevelopFileIntent(raw, 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "sources/A.gpkg")}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &DevelopFileExecutionFacts{SchemaVersion: DevelopFileExecutionSchema, ExecutionID: developFileClientExecution, TenantID: "3", Actor: DevelopFileExecutionActor{PrincipalID: "31", TenantMembershipID: "41", AuthorizationVersion: "5"}, ConfigJSON: raw, Intent: *intent, AdmissionState: "pending"}
}

func TestDevelopFileExecutionProjectionRejectsIncompleteOrInventedBindings(t *testing.T) {
	base := developOwnerFacts(t)
	for _, mutate := range []func(*DevelopFileExecutionFacts){
		func(f *DevelopFileExecutionFacts) { f.TenantID = "03" },
		func(f *DevelopFileExecutionFacts) { f.Actor.AuthorizationVersion = "0" },
		func(f *DevelopFileExecutionFacts) { f.ConfigJSON = json.RawMessage(`{"value":9007199254740992}`) },
		func(f *DevelopFileExecutionFacts) { f.ConfigJSON = json.RawMessage(`{"value":1,"value":2}`) },
		func(f *DevelopFileExecutionFacts) {
			f.Intent.Sources[0].NodeID = "different"
			f.Intent.ConfigDigest = "invalid"
		},
		func(f *DevelopFileExecutionFacts) { f.AdmissionState = "ready" },
		func(f *DevelopFileExecutionFacts) { f.AuthorizationID = "1" },
		func(f *DevelopFileExecutionFacts) { f.ScopeDigest = strings.Repeat("a", 64) },
	} {
		bad := base.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Fatal("invalid Owner projection accepted")
		}
	}
	for _, state := range []string{"pending", "rejected"} {
		f := base.Clone()
		f.AdmissionState = state
		if f.Validate() != nil {
			t.Fatal("valid unissued lifecycle rejected", state)
		}
	}
	copy := base.Clone()
	copy.ConfigJSON[0] = '['
	copy.Intent.Sources[0].Target.Segments[1].Name = "other"
	if base.Validate() != nil {
		t.Fatal("clone changed original frozen values")
	}
}

func TestDevelopFileExecutionProjectionBindsPublicationToSealedTarget(t *testing.T) {
	base := developOwnerFacts(t)
	target := &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(11, "outputs/result.gpkg"), Mode: "fail"}
	intent, err := execution.NewDevelopFileIntent(base.ConfigJSON, 1, 45, base.Intent.Sources, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := execution.NewDevelopBoundFileScope(*intent, []execution.DevelopFileSourceReference{{
		Target: intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), Format: "geopackage", BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2",
	}}, "2")
	if err != nil {
		t.Fatal(err)
	}
	base.Intent, base.BoundScope, base.AuthorizationID, base.AdmissionState = *intent, scope, "9", "ready"
	base.ScopeDigest, _ = scope.Digest()
	base.Publication = &DevelopFilePublicationFacts{ApprovalID: developFileClientApproval, Request: plugin.NewFilePublicationRequest{
		OperationID: developFileClientLease, Target: target.Target, SizeBytes: 8192, SHA256: strings.Repeat("c", 64),
	}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*DevelopFileExecutionFacts){
		func(f *DevelopFileExecutionFacts) { f.AdmissionState = "pending" },
		func(f *DevelopFileExecutionFacts) { f.Publication.ApprovalID = "81" },
		func(f *DevelopFileExecutionFacts) { f.Publication.Request.Target.Segments[1].Name = "other" },
		func(f *DevelopFileExecutionFacts) { f.Publication.Request.SizeBytes = 0 },
		func(f *DevelopFileExecutionFacts) { f.Publication.PreparationDigest = strings.Repeat("F", 64) },
	} {
		bad := base.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Fatal("invalid publication accepted")
		}
	}
	base.Publication.PreparationDigest = strings.Repeat("d", 64)
	if err := base.Validate(); err != nil {
		t.Fatal("valid confirmed publication rejected", err)
	}
}

func TestDevelopFileExecutionReaderUsesOneBoundedMachineRequest(t *testing.T) {
	for _, fault := range []string{"valid", "tenant", "execution", "unknown", "duplicate", "redirect", "unauthorized", "too-large", "large-valid"} {
		t.Run(fault, func(t *testing.T) {
			facts := developOwnerFacts(t)
			if fault == "large-valid" {
				facts.ConfigJSON = json.RawMessage(`{"large":"` + strings.Repeat("c", 350<<10) + `"}`)
				intent, err := execution.NewDevelopFileIntent(facts.ConfigJSON, 1, 45, []execution.DevelopFileSourceIntent{{NodeID: strings.Repeat("n", 200<<10), Target: plugin.FileItemPath(11, "sources/A.gpkg")}}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				facts.Intent = *intent
			}
			if fault == "tenant" {
				facts.TenantID = "4"
			}
			if fault == "execution" {
				facts.ExecutionID = developFileClientLease
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.URL.Path != "/api/v1/develop/runtime/file-executions/"+developFileClientExecution || r.Header.Get("Authorization") != "Bearer addp_at_service" || r.URL.RawQuery != "" {
					t.Error("unbound Owner read")
				}
				if fault == "redirect" {
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(302)
					return
				}
				if fault == "unauthorized" {
					w.WriteHeader(401)
					_, _ = w.Write([]byte(`{"error":"private-service-secret"}`))
					return
				}
				if fault == "too-large" {
					_, _ = w.Write([]byte(`{"huge":"` + strings.Repeat("x", MaxDevelopFileExecutionFactsBytes) + `"}`))
					return
				}
				encoded, _ := json.Marshal(facts)
				if fault == "large-valid" && len(encoded) <= execution.MaxExecutionConfigBytes {
					t.Error("large test did not exceed single-value budget")
				}
				if fault == "unknown" {
					encoded = append([]byte(`{"lease_token":"secret",`), encoded[1:]...)
				}
				if fault == "duplicate" {
					encoded = append([]byte(`{"schema_version":"duplicate",`), encoded[1:]...)
				}
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			value, err := NewDevelopClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).ReadFileExecution(t.Context(), 3, developFileClientExecution)
			want := fault == "valid" || fault == "large-valid"
			if (err == nil) != want || (value != nil) != want || calls.Load() != 1 {
				t.Fatal("Owner request changed boundary, replayed or accepted invalid wire", fault, err, calls.Load())
			}
			if err != nil && strings.Contains(err.Error(), "private-service-secret") {
				t.Fatal("leaked Owner failure")
			}
		})
	}
}
