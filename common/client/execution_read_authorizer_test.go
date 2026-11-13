package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"go.opentelemetry.io/otel/trace"
)

type executionTestModuleLister struct{ modules []*ModuleInfo }

func (l executionTestModuleLister) ListActiveModules(context.Context) ([]*ModuleInfo, error) {
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

		if mode == "extra" {
			_ = json.NewEncoder(w).Encode(map[string]any{"module": "transfer", "tenant_id": 7, "principal_id": 9, "grant_all": true})
			return
		}
		if mode == "multiple" {
			_, _ = w.Write([]byte(`{"module":"transfer","tenant_id":7,"principal_id":9} {}`))
			return
		}
		_ = json.NewEncoder(w).Encode(scope)
	}))
	defer owner.Close()
	registry := executionTestModuleLister{[]*ModuleInfo{{ModuleName: "transfer", Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}}}}
	auth := NewExecutionReadAuthorizer(registry)
	for _, value := range []string{"ok", "tenant", "subject", "owner", "redirect", "extra", "multiple"} {
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
	modules := make([]*ModuleInfo, count)
	owners := make([]string, count)
	for i := range owners {
		owners[i] = fmt.Sprintf("owner_%d", i)
		modules[i] = &ModuleInfo{ModuleName: owners[i], Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}}
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
	modules := []*ModuleInfo{}
	for _, name := range []string{"slow", "failed"} {
		modules = append(modules, &ModuleInfo{ModuleName: name, Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}})
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

type executionReadTransport func(*http.Request) (*http.Response, error)

func (f executionReadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type executionReadRegistry func(context.Context) ([]*ModuleInfo, error)

func (f executionReadRegistry) ListActiveModules(ctx context.Context) ([]*ModuleInfo, error) {
	return f(ctx)
}

type executionReadBrokenBody struct{}

func (executionReadBrokenBody) Read([]byte) (int, error) { return 0, errors.New("private body error") }
func (executionReadBrokenBody) Close() error             { return nil }

func captureExecutionReadLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	output := &bytes.Buffer{}
	logger.Init(logger.Options{Writer: output})
	t.Cleanup(func() { logger.Init(logger.Options{Writer: os.Stdout}) })
	return output
}

func TestExecutionReadFailureDiagnosticsAreBoundedAndCorrelated(t *testing.T) {
	for _, tc := range []struct {
		name, stage, reason string
		status              int
	}{
		{"success", "", "", 0}, {"empty", "", "", 0},
		{"input", "input", "invalid_input", 0},
		{"registry", "registry", "unavailable", 0},
		{"dispatch", "dispatch", "cancelled", 0},
		{"invalid_owner", "owner", "invalid_owner", 0},
		{"no_instance", "owner", "no_active_instance", 0},
		{"transport", "transport", "unavailable", 0},
		{"timeout", "transport", "timeout", 0},
		{"cancelled", "transport", "cancelled", 0},
		{"status", "response", "unexpected_status", 503},
		{"read", "response", "read_failed", 200},
		{"too_large", "response", "too_large", 200},
		{"invalid_scope", "response", "invalid_scope", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := captureExecutionReadLog(t)
			tid, _ := trace.TraceIDFromHex(strings.Repeat("1", 32))
			sid, _ := trace.SpanIDFromHex(strings.Repeat("2", 16))
			ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			modules := []*ModuleInfo{{ModuleName: "transfer", Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: "http://private-host.example", LeaseExpiresAt: time.Now().Add(time.Hour)}}}}
			auth := NewExecutionReadAuthorizer(executionReadRegistry(func(context.Context) ([]*ModuleInfo, error) {
				if tc.name == "registry" {
					return nil, errors.New("private registry error")
				}
				if tc.name == "dispatch" {
					cancel()
				}
				return modules, nil
			}))
			owners := []string{"transfer"}
			bearer := "Bearer private-token"
			switch tc.name {
			case "empty":
				owners = nil
			case "input":
				bearer = ""
			case "invalid_owner":
				owners = []string{"../private-owner"}
			case "no_instance":
				modules = nil
			}
			auth.httpClient.Transport = executionReadTransport(func(*http.Request) (*http.Response, error) {
				switch tc.name {
				case "transport":
					return nil, errors.New("private transport error")
				case "timeout":
					return nil, context.DeadlineExceeded
				case "cancelled":
					return nil, context.Canceled
				}
				status := http.StatusOK
				body := io.ReadCloser(io.NopCloser(strings.NewReader(`{"module":"transfer","tenant_id":7,"principal_id":9,"grants":[]}`)))
				switch tc.name {
				case "status":
					status = 503
					body = io.NopCloser(strings.NewReader("private response error"))
				case "read":
					body = executionReadBrokenBody{}
				case "too_large":
					body = io.NopCloser(strings.NewReader(strings.Repeat("private", 6000)))
				case "invalid_scope":
					body = io.NopCloser(strings.NewReader(`{"private":"secret"}`))
				}
				return &http.Response{StatusCode: status, Body: body, Header: make(http.Header)}, nil
			})
			scopes, err := auth.Resolve(ctx, owners, 7, 9, bearer)
			if tc.stage == "" {
				if err != nil || scopes == nil || output.Len() != 0 {
					t.Fatalf("success produced error or logs: %v %s", err, output.String())
				}
				return
			}
			if !errors.Is(err, ErrExecutionOwnerUnavailable) || scopes != nil {
				t.Fatalf("failure changed: %v %#v", err, scopes)
			}
			var record map[string]any
			decoder := json.NewDecoder(output)
			if e := decoder.Decode(&record); e != nil {
				t.Fatal(e)
			}
			if decoder.Decode(new(any)) != io.EOF {
				t.Fatal("more than one failure record")
			}
			if record["msg"] != "execution.owner_authorization.failed" || record["stage"] != tc.stage || record["reason"] != tc.reason ||
				record["observation"] != "first_observed" || record["trace_id"] != tid.String() || record["span_id"] != sid.String() {
				t.Fatalf("wrong diagnostic: %#v", record)
			}
			if tc.status == 0 && record["http_status"] != nil || tc.status != 0 && record["http_status"] != float64(tc.status) {
				t.Fatalf("wrong HTTP status: %#v", record)
			}
			expectedOwner := "transfer"
			if tc.stage == "input" || tc.stage == "registry" || tc.stage == "dispatch" || tc.name == "invalid_owner" {
				expectedOwner = ""
			}
			if record["owner_module"] != expectedOwner {
				t.Fatalf("wrong owner: %#v", record)
			}
			encoded, _ := json.Marshal(record)
			if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret") {
				t.Fatalf("private details in log: %s", encoded)
			}
		})
	}
}

func TestExecutionReadFirstFailureSurvivesPeerCancellation(t *testing.T) {
	output := captureExecutionReadLog(t)
	started := make(chan struct{})
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/slow/") {
			close(started)
			<-r.Context().Done()
			return
		}
		<-started
		http.Error(w, "private owner body", 503)
	}))
	defer owner.Close()
	modules := []*ModuleInfo{}
	for _, name := range []string{"slow", "failed"} {
		modules = append(modules, &ModuleInfo{ModuleName: name, Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: "backend", Status: "up", ModuleURL: owner.URL, LeaseExpiresAt: time.Now().Add(time.Hour)}}})
	}
	scopes, err := NewExecutionReadAuthorizer(executionTestModuleLister{modules}).Resolve(context.Background(), []string{"slow", "failed"}, 7, 9, "Bearer private-token")
	if !errors.Is(err, ErrExecutionOwnerUnavailable) || scopes != nil {
		t.Fatalf("wrong result: %v", err)
	}
	var record map[string]any
	decoder := json.NewDecoder(output)
	if e := decoder.Decode(&record); e != nil {
		t.Fatal(e)
	}
	if decoder.Decode(new(any)) != io.EOF || record["owner_module"] != "failed" || record["reason"] != "unexpected_status" || record["http_status"] != float64(503) {
		t.Fatalf("first failure overwritten or duplicated: %#v", record)
	}
}
