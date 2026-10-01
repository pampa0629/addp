package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
)

type executionTestModuleLister struct{ modules []*commonclient.ModuleInfo }

func (l executionTestModuleLister) ListActiveModules(context.Context) ([]*commonclient.ModuleInfo, error) {
	return l.modules, nil
}

func TestExecutionReadAuthorizerBindsOwnerTenantSubjectAndNeverFollowsRedirect(t *testing.T) {
	mode := "ok"
	var forwarded bool
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get("Authorization") == "Bearer verified-user" && r.URL.Path == "/api/v1/transfer/execution-read-scope" && r.URL.RawQuery == ""
		if mode == "redirect" {
			http.Redirect(w, r, "http://invalid.example/credential", 302)
			return
		}
		scope := execution.ReadScope{Module: "transfer", TenantID: 7, PrincipalID: 9, Grants: []execution.ReadGrant{{TaskType: "sync", TaskHistory: true}}}
		if mode == "tenant" {
			scope.TenantID = 8
		}
		if mode == "subject" {
			scope.PrincipalID = 10
		}
		if mode == "owner" {
			scope.Module = "quality"
		}
		_ = json.NewEncoder(w).Encode(scope)
	}))
	defer owner.Close()
	registry := executionTestModuleLister{[]*commonclient.ModuleInfo{{ModuleName: "transfer", Enabled: true, Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}}}}
	auth := NewExecutionReadAuthorizer(registry)
	for _, value := range []string{"ok", "tenant", "subject", "owner", "redirect"} {
		mode = value
		scopes, err := auth.Resolve(context.Background(), []string{"transfer"}, 7, 9, "Bearer verified-user")
		if value == "ok" {
			if err != nil || len(scopes) != 1 || !forwarded {
				t.Fatalf("scopes=%#v err=%v forwarded=%v", scopes, err, forwarded)
			}
		} else if err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if _, err := auth.Resolve(context.Background(), []string{"../system"}, 7, 9, "Bearer verified-user"); err == nil {
		t.Fatal("accepted owner traversal")
	}
}
