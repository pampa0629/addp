package service

import (
	"context"
	commonclient "github.com/addp/common/client"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCleanupSourceAbsenceRequiresExactAuthority(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/meta/items/99" {
					t.Errorf("identity fallback: %s", r.URL.Path)
				}
				w.WriteHeader(code)
			}))
			defer server.Close()
			svc := &CleanupService{metaClient: commonclient.NewMetaClient(server.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "fixture", nil }))}
			missing, err := svc.cleanupMissingSource(t.Context(), 7, "addp://engine/9/path/other?type=table", 99)
			if code == http.StatusNotFound {
				if err != nil || !missing {
					t.Fatalf("missing=%v err=%v", missing, err)
				}
			} else if err == nil || missing {
				t.Fatalf("unavailable was treated as absent: %v %v", missing, err)
			}
			if calls != 1 {
				t.Fatalf("authority read count=%d", calls)
			}
		})
	}
}
