package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type notebookSessionAuthorizationTestTokenSource struct{}

func (notebookSessionAuthorizationTestTokenSource) Token(_ context.Context, tenantID uint) (string, error) {
	if tenantID != 5 {
		return "", context.Canceled
	}
	return "addp_at_develop_service", nil
}

func (notebookSessionAuthorizationTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "addp_at_platform_service", nil
}

func TestSystemNotebookSessionAuthorizationClientsKeepUserAndServiceCredentialsSeparated(t *testing.T) {
	t.Parallel()
	const (
		authorizationID = "00000000-0000-0000-0000-000000000010"
		sessionID       = "00000000-0000-0000-0000-000000000011"
	)
	expiresAt := time.Now().UTC().Add(time.Minute)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Notebook Catalog request sent legacy authentication headers")
		}
		switch r.URL.Path {
		case "/api/v1/system/auth/notebook-session-authorizations":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer addp_at_user" {
				t.Fatalf("issue request = %s Authorization=%q", r.Method, r.Header.Get("Authorization"))
			}
			var request IssueNotebookSessionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.SessionID != sessionID || request.TaskID != 17 || request.ExpiresIn != 600 {
				t.Fatalf("issue request = %#v", request)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(IssuedNotebookSessionAuthorization{
				ID: authorizationID, SessionID: sessionID, TaskID: 17, ExpiresAt: expiresAt,
			})
		case "/api/v1/system/notebook-session-authorizations/" + authorizationID + "/catalog/children":
			if r.Header.Get("Authorization") != "Bearer addp_at_develop_service" {
				t.Fatalf("consume Authorization = %q", r.Header.Get("Authorization"))
			}
			var request NotebookEngineCatalogChildrenRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.SessionID != sessionID || request.EngineID != 21 || request.Path.EngineID != 21 {
				t.Fatalf("consume request = %#v", request)
			}
			_ = json.NewEncoder(w).Encode(EngineCatalogListChildrenResponse{Nodes: []EngineCatalogEntry{{
				Name: "public", Term: "schema", Kind: "schema", Role: "branch",
				Path: EngineCatalogPath{Version: "catalog.path/v1", EngineID: 21, Segments: []EngineCatalogSegment{}},
			}}})
		case "/api/v1/system/notebook-session-authorizations/" + authorizationID + "/revocations":
			if r.Header.Get("Authorization") != "Bearer addp_at_develop_service" {
				t.Fatalf("revoke Authorization = %q", r.Header.Get("Authorization"))
			}
			var request RevokeNotebookSessionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.SessionID != sessionID {
				t.Fatalf("revoke request = %#v", request)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	issuer := NewSystemNotebookSessionAuthorizationClient(server.URL, server.Client())
	issued, err := issuer.Issue(context.Background(), "addp_at_user", IssueNotebookSessionAuthorizationRequest{
		SessionID: sessionID, TaskID: 17, ExpiresIn: 600,
	})
	if err != nil || issued.ID != authorizationID {
		t.Fatalf("Issue() response=%#v error=%v", issued, err)
	}

	serviceClient := NewSystemServiceClient(server.URL, notebookSessionAuthorizationTestTokenSource{}, server.Client()).WithTenantID(5)
	nodes, err := serviceClient.ListNotebookEngineCatalogChildren(context.Background(), authorizationID, NotebookEngineCatalogChildrenRequest{
		SessionID: sessionID, EngineID: 21,
		Path:    EngineCatalogPath{Version: "catalog.path/v1", EngineID: 21, Segments: []EngineCatalogSegment{}},
		Options: EngineCatalogListOptions{Limit: 1000},
	})
	if err != nil || len(nodes) != 1 || nodes[0].Name != "public" {
		t.Fatalf("ListNotebookEngineCatalogChildren() nodes=%#v error=%v", nodes, err)
	}
	if err := serviceClient.RevokeNotebookSessionAuthorization(context.Background(), authorizationID,
		RevokeNotebookSessionAuthorizationRequest{SessionID: sessionID}); err != nil {
		t.Fatalf("RevokeNotebookSessionAuthorization() error=%v", err)
	}
	if requests != 3 {
		t.Fatalf("request count = %d, want 3", requests)
	}
}

func TestSystemNotebookSessionAuthorizationClientPreservesStableErrorCode(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "forbidden", "error_code": "notebook_session_authorization_forbidden",
		})
	}))
	t.Cleanup(server.Close)

	client := NewSystemServiceClient(server.URL, notebookSessionAuthorizationTestTokenSource{}, server.Client()).WithTenantID(5)
	_, err := client.ListNotebookEngineCatalogChildren(context.Background(),
		"00000000-0000-0000-0000-000000000010", NotebookEngineCatalogChildrenRequest{
			SessionID: "00000000-0000-0000-0000-000000000011", EngineID: 21,
		})
	if code, ok := SystemAPIErrorCode(err); !ok || code != "notebook_session_authorization_forbidden" {
		t.Fatalf("SystemAPIErrorCode() = %q, %t, error=%v", code, ok, err)
	}
}
