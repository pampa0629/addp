package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestGetItemSnapshotUsesTenantServiceAndExactPersistedIdentity(t *testing.T) {
	for _, itemID := range []uint{0, 19} {
		t.Run(string(rune('a'+itemID)), func(t *testing.T) {
			var tokenTenant uint
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer synthetic-service" || r.Header.Get("X-Tenant-ID") != "" {
					t.Errorf("invalid service request %s %s", r.Method, r.URL)
				}
				if itemID != 0 {
					if r.URL.Path != "/api/v1/meta/items/19" || r.URL.RawQuery != "" {
						t.Errorf("item request %s", r.URL)
					}
				} else if r.URL.Path != "/api/v1/meta/items/by-catalog-path" || r.URL.Query().Get("catalog_path") != "synthetic.source" || r.URL.Query().Get("engine_id") != "4" {
					t.Errorf("path request %s", r.URL)
				}
				_, _ = w.Write([]byte(`{"id":19,"tenant_id":7,"engine_id":4,"full_name":"synthetic.source","attributes":{"type_info":{"table":{"fields":[{"name":"value","precision":9007199254740993}]}}}}`))
			}))
			defer server.Close()
			client := NewMetaClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
				tokenTenant = tenantID
				return "synthetic-service", nil
			}))
			item, err := client.GetItemSnapshot(context.Background(), 7, 4, itemID, "synthetic.source")
			if err != nil || item.ID != 19 || tokenTenant != 7 {
				t.Fatalf("item=%+v err=%v tokenTenant=%d", item, err, tokenTenant)
			}
			payload, _ := json.Marshal(item.Attributes)
			if !strings.Contains(string(payload), "9007199254740993") {
				t.Fatal("structure fact numeric precision lost")
			}
		})
	}
}
func TestGetItemSnapshotDistinguishesMissingFromFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{"missing", 404, `private upstream details`, true},
		{"denied", 403, `private upstream details`, false},
		{"unauthenticated", 401, `private upstream details`, false},
		{"server", 500, `private upstream details`, false},
		{"tenant_mismatch", 200, `{"id":19,"tenant_id":8,"engine_id":4,"full_name":"synthetic.source"}`, false},
		{"path_mismatch", 200, `{"id":19,"tenant_id":7,"engine_id":4,"full_name":"synthetic.other"}`, false},
		{"id_mismatch", 200, `{"id":20,"tenant_id":7,"engine_id":4,"full_name":"synthetic.source"}`, false},
		{"engine_mismatch", 200, `{"id":19,"tenant_id":7,"engine_id":5,"full_name":"synthetic.source"}`, false},
		{"multiple_items", 200, `{"id":19,"tenant_id":7,"engine_id":4,"full_name":"synthetic.source"}{}`, false},
		{"invalid_utf8", 200, `{"id":19,"tenant_id":7,"engine_id":4,"full_name":"synthetic.source","attributes":{"value":"` + string([]byte{0xff}) + `"}}`, false},
		{"oversize", 200, strings.Repeat(" ", (2<<20)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newTestMetaClient(server.URL).GetItemSnapshot(context.Background(), 7, 4, 19, "synthetic.source")
			if err == nil || errors.Is(err, commonapi.ErrNotFound) != tc.missing || strings.Contains(err.Error(), "private upstream details") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTestMetaClient("http://127.0.0.1:1").GetItemSnapshot(ctx, 7, 4, 19, "synthetic.source"); err == nil {
		t.Fatal("cancelled request accepted")
	}
}
