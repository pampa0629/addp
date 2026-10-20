package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeInstanceReferencesUseOnlyCurrentUserAndExactIDs(t *testing.T) {
	yes := true
	now := time.Now().UTC()
	row := RuntimeInstanceReference{ID: 7, ModuleName: "monitor", InstanceID: "worker-a", Role: "worker", Status: "up", LeaseExpiresAt: now.Add(time.Minute), ProcessStartedAt: &now, ProcessMetricsDeclared: &yes}
	for _, tc := range []struct {
		name   string
		status int
		mutate func(*RuntimeInstanceReference)
		total  int
		valid  bool
	}{
		{"current unbound process", 200, nil, 1, true},
		{"wrong row", 200, func(r *RuntimeInstanceReference) { r.ID = 8 }, 1, false},
		{"missing declaration state", 200, func(r *RuntimeInstanceReference) { r.ProcessMetricsDeclared = nil }, 1, false},
		{"bad role", 200, func(r *RuntimeInstanceReference) { r.Role = "other" }, 1, false},
		{"control in identity", 200, func(r *RuntimeInstanceReference) { r.InstanceID = "bad\nworker" }, 1, false},
		{"incomplete owner response", 200, nil, 2, false},
		{"denied", 403, nil, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			current := row
			if tc.mutate != nil {
				tc.mutate(&current)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/platform/module-instances" || r.Header.Get("Authorization") != "Bearer addp_at_current_user" || r.URL.Query().Get("ids") != "7" || r.URL.Query().Get("page_size") != "100" {
					t.Error("owner path/token/scope changed")
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []RuntimeInstanceReference{current}, "total": tc.total, "page": 1, "page_size": 100})
			}))
			defer server.Close()
			c := NewSystemServiceClient(server.URL, nil, server.Client())
			refs, err := c.GetRuntimeInstancesForUser(context.Background(), []uint{7}, "addp_at_current_user")
			if (err == nil) != tc.valid || calls != 1 {
				t.Fatalf("valid=%v calls=%d refs=%+v err=%v", tc.valid, calls, refs, err)
			}
			if tc.status == 403 {
				var status *SystemAPIError
				if !errors.As(err, &status) || status.StatusCode != 403 {
					t.Fatal("owner denial lost", err)
				}
			}
			if _, err = c.GetRuntimeInstancesForUser(context.Background(), []uint{7, 7}, "addp_at_current_user"); err == nil || calls != 1 {
				t.Fatal("duplicate query sent")
			}
			if _, err = c.GetRuntimeInstancesForUser(context.Background(), []uint{7}, "addp_st_service"); err == nil || calls != 1 {
				t.Fatal("machine credential used")
			}
		})
	}
}

func TestHistoricalProcessReferencesUseCurrentUserAndExactProcessIDs(t *testing.T) {
	for _, mode := range []string{"valid", "foreign", "duplicate", "denied"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer addp_at_current_user" || r.URL.Query().Get("instance_ids") != "old-one" || r.URL.Query().Has("status") {
					t.Error("history query changed current User or imposed a live lease")
				}
				if mode == "denied" {
					w.WriteHeader(403)
					return
				}
				row := RuntimeInstanceReference{ID: 1, ModuleName: "monitor", InstanceID: "old-one", Role: "backend", Status: "down"}
				if mode == "foreign" {
					row.InstanceID = "foreign"
				}
				rows := []RuntimeInstanceReference{row}
				if mode == "duplicate" {
					rows = append(rows, row)
				}
				json.NewEncoder(w).Encode(map[string]any{"data": rows, "total": len(rows), "page": 1, "page_size": 100})
			}))
			defer upstream.Close()
			c := NewSystemServiceClient(upstream.URL, nil, upstream.Client())
			rows, e := c.GetProcessInstancesForUser(context.Background(), []string{"old-one"}, "addp_at_current_user")
			if (e == nil) != (mode == "valid") || calls != 1 {
				t.Fatal(rows, e, calls)
			}
			if _, e = c.GetProcessInstancesForUser(context.Background(), []string{"old-one"}, "addp_st_machine"); e == nil || calls != 1 {
				t.Fatal("machine token reached current User resolver")
			}
		})
	}
}
