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
	"github.com/google/uuid"
)

func TestMosaicScanResultUsesBoundTenantService(t *testing.T) {
	scanID, parentID := uuid.NewString(), uuid.NewString()
	var tokenTenant uint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/meta/runtime/mosaic-scans/"+scanID+"/result" || r.URL.Query().Get("parent_execution_id") != parentID || len(r.URL.Query()) != 1 || r.Header.Get("Authorization") != "Bearer synthetic-service" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("invalid bound service request: %s %s", r.Method, r.URL)
		}
		_ = json.NewEncoder(w).Encode(MetaMosaicScanResult{ExecutionID: scanID, ParentExecutionID: parentID, Status: "pending"})
	}))
	defer server.Close()
	client := NewMetaClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		tokenTenant = tenantID
		return "synthetic-service", nil
	}))
	result, err := client.GetMosaicScanResult(t.Context(), 7, scanID, parentID)
	if err != nil || result.Status != "pending" || tokenTenant != 7 {
		t.Fatalf("result=%+v tenant=%d err=%v", result, tokenTenant, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.GetMosaicScanResult(ctx, 7, scanID, parentID); err == nil {
		t.Fatal("cancelled request accepted")
	}
}

func TestMosaicScanResultRejectsUnboundOrUnclosedResponses(t *testing.T) {
	scanID, parentID := uuid.NewString(), uuid.NewString()
	base := `{"execution_id":"` + scanID + `","parent_execution_id":"` + parentID + `","status":"pending"}`
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{"missing", 404, "private-upstream-detail", true},
		{"denied", 403, "private-upstream-detail", false},
		{"failed", 500, "private-upstream-detail", false},
		{"wrong_scan", 200, strings.ReplaceAll(base, scanID, uuid.NewString()), false},
		{"wrong_parent", 200, strings.ReplaceAll(base, parentID, uuid.NewString()), false},
		{"extra_metadata", 200, strings.TrimSuffix(base, "}") + `,"metadata":{"secret":"private-upstream-detail"}}`, false},
		{"multiple_values", 200, base + `{}`, false},
		{"oversize", 200, base + strings.Repeat(" ", 64<<10), false},
		{"invalid_utf8", 200, base + string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newTestMetaClient(server.URL).GetMosaicScanResult(t.Context(), 7, scanID, parentID)
			if err == nil || errors.Is(err, commonapi.ErrNotFound) != tc.missing || strings.Contains(err.Error(), "private-upstream-detail") {
				t.Fatalf("unsafe or accepted response: %v", err)
			}
		})
	}
	if _, err := newTestMetaClient("http://127.0.0.1:1").GetMosaicScanResult(t.Context(), 7, "not-a-uuid", parentID); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid request: %v", err)
	}
}
