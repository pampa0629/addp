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
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func model3DExpectedSourceFixture(t *testing.T, texture string) ManagerModel3DExpectedSource {
	t.Helper()
	composition, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(33, "models/model.3ds"), Role: "model", Required: true, Primary: true},
		{Target: plugin.FileItemPath(33, "models/"+texture), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ManagerModel3DExpectedSource{EngineVersion: "5", FileComposition: *composition}
}

// A protocol fixture proves what Manager sends and how the shared SDK handles
// a same-decision source mismatch. Real Meta/Grant/IAM judgment is System T2/T4.
func TestManagerModel3DResultReadUsesExpectedSourceInSameHumanDecision(t *testing.T) {
	for _, outcome := range []string{"same source", "composition A to B", "engine version changed", "old strict server"} {
		t.Run(outcome, func(t *testing.T) {
			generated := model3DExpectedSourceFixture(t, "Original.PNG")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/read-checks/manager-preview" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer addp_at_current_human" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("Cookie") != "" {
					t.Error("changed fixed Human protocol")
				}
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				if outcome == "old strict server" {
					var oldRequest struct {
						Targets []plugin.EngineCatalogPath `json:"targets"`
					}
					if decoder.Decode(&oldRequest) == nil {
						t.Error("expected_source missing from request")
					}
					w.WriteHeader(400)
					return
				}
				var request struct {
					Targets        []plugin.EngineCatalogPath   `json:"targets"`
					ExpectedSource ManagerModel3DExpectedSource `json:"expected_source"`
				}
				primary, _ := generated.FileComposition.PrimaryTarget()
				if decoder.Decode(&request) != nil || len(request.Targets) != 1 || !reflect.DeepEqual(request.Targets[0], primary) || !reflect.DeepEqual(request.ExpectedSource, generated) {
					t.Error("generated complete source was not sent exactly")
					w.WriteHeader(400)
					return
				}
				// Manager has already observed A. Change current trusted facts at
				// the System decision boundary; B may have its own current Grant.
				current := model3DExpectedSourceFixture(t, "Original.PNG")
				if outcome == "composition A to B" {
					current = model3DExpectedSourceFixture(t, "New.PNG")
				}
				if outcome == "engine version changed" {
					current.EngineVersion = "6"
				}
				if !reflect.DeepEqual(request.ExpectedSource, current) {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"observed_at": time.Now().UTC()})
			}))
			t.Cleanup(server.Close)
			client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("must-not-be-used"), server.Client())
			err := client.CheckManagerModel3DResultRead(t.Context(), "addp_at_current_human", generated)
			switch outcome {
			case "same source":
				if err != nil {
					t.Fatal(err)
				}
			case "old strict server":
				if !errors.Is(err, ErrManagerPreviewReadUnavailable) {
					t.Fatalf("old contract allowed: %v", err)
				}
			default:
				if !errors.Is(err, ErrManagerPreviewReadDenied) {
					t.Fatalf("changed source allowed: %v", err)
				}
			}
			if calls != 1 {
				t.Fatalf("unexpected retry or primary-only fallback: %d", calls)
			}
		})
	}
}

func TestManagerModel3DResultReadRejectsTicketsDelegatesAndMalformedExpectedSource(t *testing.T) {
	generated := model3DExpectedSourceFixture(t, "Original.PNG")
	client := NewSystemServiceClient("http://must-not-be-contacted.invalid", staticSystemServiceTokenSource("must-not-be-used"), nil)
	for _, token := range []string{"", "addp_rat_ticket", "addp_dat_tool", "addp_at_", "addp_at_user\n"} {
		if err := client.CheckManagerModel3DResultRead(context.Background(), token, generated); !errors.Is(err, ErrManagerPreviewCredentialRejected) {
			t.Fatalf("credential accepted: %v", err)
		}
	}
	for _, version := range []string{"", "0", "-1", "05", "+5", "5.0", strings.Repeat("9", 40)} {
		invalid := generated
		invalid.EngineVersion = version
		if err := client.CheckManagerModel3DResultRead(t.Context(), "addp_at_current_human", invalid); !errors.Is(err, ErrManagerPreviewReadDenied) {
			t.Fatalf("invalid expected version accepted: %v", err)
		}
	}
	invalid := generated
	invalid.FileComposition = authorizationfacts.FileCompositionBoundary{}
	if err := client.CheckManagerModel3DResultRead(t.Context(), "addp_at_current_human", invalid); !errors.Is(err, ErrManagerPreviewReadDenied) {
		t.Fatalf("missing composition accepted: %v", err)
	}
}

func TestManagerModel3DExpectedObjectSource(t *testing.T) {
	for _, format := range []string{"ifc", "3ds", "dae", "obj"} {
		t.Run(format, func(t *testing.T) {
			refs := []authorizationfacts.FileCompositionRef{{Target: plugin.ObjectItemPath(33, "models", "scene/model."+format), Role: "model", Required: true, Primary: true}}
			if format != "ifc" {
				refs = append(refs, authorizationfacts.FileCompositionRef{Target: plugin.ObjectItemPath(33, "models", "scene/ Original.PNG "), Role: "texture", Required: true})
			}
			composition, err := authorizationfacts.NewFileCompositionBoundary(format, refs)
			if err != nil {
				t.Fatal(err)
			}
			source := ManagerModel3DExpectedSource{EngineVersion: "5", FileComposition: *composition}
			if source.Validate() != nil {
				t.Fatalf("unexpected object format admission: %s", format)
			}
			if format == "3ds" || format == "dae" {
				for i := range source.FileComposition.Refs {
					if !source.FileComposition.Refs[i].Primary {
						source.FileComposition.Refs[i].Target = plugin.ObjectItemPath(33, "other", "scene/ Original.PNG ")
					}
				}
				if source.Validate() == nil {
					t.Fatal("cross-bucket composition accepted")
				}
			}
		})
	}
}
