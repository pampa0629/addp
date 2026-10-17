package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTaskAuthorizationNeverFollowsCredentialRedirects(t *testing.T) {
	for _, method := range []string{"authorize", "resolve"} {
		for _, code := range []int{302, 307, 308} {
			t.Run(method+http.StatusText(code), func(t *testing.T) {
				request := TaskAuthorizationSubjectRequest{OwnerModule: "transfer", TaskType: "sync", TaskRef: "32000000-0000-0000-0000-000000000216", DefinitionHash: strings.Repeat("a", 64)}
				forwarded := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/forwarded" {
						forwarded++
						_ = json.NewEncoder(w).Encode(TaskAuthorizationSubject{ID: "81", OwnerModule: "transfer", TaskType: "sync", TaskRef: request.TaskRef, DefinitionHash: request.DefinitionHash, TenantID: "7", PrincipalID: "31", TenantMembershipID: "32", AuthorizationVersion: "1", AuthorizedAt: time.Now().UTC()})
						return
					}
					expected := "Bearer addp_at_user_subject"
					if method == "resolve" {
						expected = "Bearer addp_at_machine_subject"
					}
					if r.Header.Get("Authorization") != expected {
						t.Error("wrong initial credential")
					}
					http.Redirect(w, r, "/forwarded", code)
				}))
				defer server.Close()
				transport := server.Client()
				var err error
				if method == "authorize" {
					_, err = NewSystemExecutionAuthorizationClient(server.URL, transport).AuthorizeTaskSubject(context.Background(), "addp_at_user_subject", request)
				} else {
					_, err = NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_machine_subject"), transport).WithTenantID(7).ResolveTaskAuthorizationSubject(context.Background(), "81", request)
				}
				status, ok := SystemAPIStatusCode(err)
				if err == nil || !ok || status != code || forwarded != 0 {
					t.Fatalf("redirect was not rejected: code=%d status=%d forwarded=%d error=%v", code, status, forwarded, err)
				}
				if transport.CheckRedirect != nil {
					t.Fatal("shared transport was mutated")
				}
			})
		}
	}
}
