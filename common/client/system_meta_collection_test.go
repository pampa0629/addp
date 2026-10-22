package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

type collectionTokenProbe struct{ metaScanTokenProbe }

func (p *collectionTokenProbe) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform credential must not be requested")
}

func TestMetaCollectionCheckSingleBoundedRequest(t *testing.T) {
	request := execution.MetaCollectionAccessRequest{ExecutionID: uuid.NewString(), Attempt: 1, LeaseToken: uuid.NewString(), ExecutionPlanDigest: strings.Repeat("a", 64)}
	response := execution.MetaCollectionAccessObservation{ExecutionID: request.ExecutionID, ExecutionPlanDigest: request.ExecutionPlanDigest, QualificationID: uuid.NewString(), ObservedAt: time.Now().UTC()}
	raw, _ := json.Marshal(response)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{"observed", 200, string(raw), true},
		{"wrong execution", 200, strings.Replace(string(raw), request.ExecutionID, uuid.NewString(), 1), false},
		{"wrong plan", 200, strings.Replace(string(raw), request.ExecutionPlanDigest, strings.Repeat("b", 64), 1), false},
		{"empty result", 200, `{}`, false},
		{"unknown", 200, strings.Replace(string(raw), `"observed_at":`, `"token":"secret","observed_at":`, 1), false},
		{"oversized", 200, strings.Repeat(" ", 8192) + string(raw), false},
		{"denied", 403, `{"error":"secret"}`, false},
		{"expired", 401, `{"error":"secret"}`, false},
		{"redirect", 307, `{"error":"secret"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			tokens := &collectionTokenProbe{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/runtime/meta_collection_checks" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("escaped fixed service contract")
				}
				var got execution.MetaCollectionAccessRequest
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != request {
					t.Error("changed read binding", err)
				}
				if tc.status == 307 {
					w.Header().Set("Location", "/never-follow")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := NewSystemServiceClient(server.URL, tokens, nil).CheckMetaCollection(context.Background(), 7, request)
			if (err == nil) != tc.ok || calls != 1 || tokens.calls != 1 || tokens.invalidations != 0 || tokens.tenant != 7 {
				t.Fatalf("result=%+v err=%v calls=%d tokens=%+v", result, err, calls, tokens)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("error leaked upstream body")
			}
		})
	}
}
