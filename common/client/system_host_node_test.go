package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHostNodeReferenceUsesCurrentUserWithoutMachineRetry(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"valid", fmt.Sprintf(`{"node_id":%q,"version":3,"enabled":false,"addresses":["private"]}`, id), 200, true},
		{"missing enabled", fmt.Sprintf(`{"node_id":%q,"version":3}`, id), 200, false},
		{"wrong node", fmt.Sprintf(`{"node_id":%q,"version":3,"enabled":true}`, uuid.NewString()), 200, false},
		{"revoked", `{"error":"private upstream detail"}`, 401, false},
		{"denied", `{}`, 403, false},
		{"oversize", strings.Repeat(" ", 64<<10) + `{}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/platform/host_nodes/"+id || r.Header.Get("Authorization") != "Bearer addp_at_current_user" {
					t.Error("wrong owner path or token")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := NewSystemServiceClient(server.URL, nil, server.Client())
			ref, err := client.GetHostNodeForUser(context.Background(), id, "addp_at_current_user")
			if (err == nil) != tc.valid || calls != 1 {
				t.Fatalf("valid=%v calls=%d err=%v", tc.valid, calls, err)
			}
			if tc.valid && (ref.Version != 3 || ref.Enabled) {
				t.Fatal("owner facts changed")
			}
		})
	}
}

func TestHostNodeListAuthorizationVerifiesEmptyOwnerResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "page=1&page_size=1" || r.Header.Get("Authorization") != "Bearer addp_at_current_user" {
			t.Error("wrong list authorization request")
		}
		fmt.Fprint(w, `{"data":[],"total":0,"page":1,"page_size":1,"total_pages":0}`)
	}))
	defer server.Close()
	if err := NewSystemServiceClient(server.URL, nil, server.Client()).AuthorizeHostNodesForUser(context.Background(), "addp_at_current_user"); err != nil {
		t.Fatal(err)
	}
}
