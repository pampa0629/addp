package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestExecutionReadAuthorizerBoundsConcurrencyAndKeepsEveryScope(t *testing.T) {
	const count = 9
	arrived := make(chan string, count)
	release := make(chan struct{}, count)
	var active, peak atomic.Int32
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
		}
		module := strings.Split(r.URL.Path, "/")[3]
		arrived <- module
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(execution.ReadScope{Module: module, TenantID: 7, PrincipalID: 9})
	}))
	defer owner.Close()
	modules := make([]*commonclient.ModuleInfo, count)
	owners := make([]string, count)
	for i := range owners {
		owners[i] = fmt.Sprintf("owner_%d", i)
		modules[i] = &commonclient.ModuleInfo{ModuleName: owners[i], Enabled: true, Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}}
	}
	auth := NewExecutionReadAuthorizer(executionTestModuleLister{modules})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type outcome struct {
		scopes []execution.ReadScope
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		scopes, err := auth.Resolve(ctx, owners, 7, 9, "Bearer verified-user")
		done <- outcome{scopes, err}
	}()
	for completed := 0; completed < count; {
		wave := min(4, count-completed)
		for i := 0; i < wave; i++ {
			select {
			case <-arrived:
			case <-ctx.Done():
				t.Fatal("independent Owner authorizations were serialized instead of sharing the bounded budget")
			}
		}
		if peak.Load() > 4 {
			t.Fatal("Owner request concurrency exceeded four")
		}
		for i := 0; i < wave; i++ {
			release <- struct{}{}
		}
		completed += wave
	}
	result := <-done
	if result.err != nil || len(result.scopes) != count || peak.Load() != 4 {
		t.Fatalf("scopes=%d peak=%d err=%v", len(result.scopes), peak.Load(), result.err)
	}
	for i, scope := range result.scopes {
		if scope.Module != owners[i] {
			t.Fatalf("scope order changed at %d: %s", i, scope.Module)
		}
	}
}

func TestExecutionReadAuthorizerFailureCancelsOtherOwnersAndReturnsNoPartialScopes(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/slow/") {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return
		}
		<-started
		http.Error(w, "private owner details", http.StatusServiceUnavailable)
	}))
	defer owner.Close()
	modules := []*commonclient.ModuleInfo{}
	for _, name := range []string{"slow", "failed"} {
		modules = append(modules, &commonclient.ModuleInfo{ModuleName: name, Enabled: true, Instances: []commonclient.ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	scopes, err := NewExecutionReadAuthorizer(executionTestModuleLister{modules}).Resolve(ctx, []string{"slow", "failed"}, 7, 9, "Bearer verified-user")
	if !errors.Is(err, ErrExecutionOwnerUnavailable) || scopes != nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("scopes=%#v err=%v", scopes, err)
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("pending Owner request was not cancelled")
	}
}
