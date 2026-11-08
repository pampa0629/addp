package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizationBasisRevisionUsesBoundedUncachedSystemOwner(t *testing.T) {
	for _, body := range []string{
		`{"schema_version":"addp.authorization_basis_revision/v1","generation":"03c40e08-fcbf-468c-932e-d54da85ca8e7","version":"3"}`,
		`{"schema_version":"addp.authorization_basis_revision/v1","generation":"03c40e08-fcbf-468c-932e-d54da85ca8e7","version":"3","ids":[]}`,
		`{"schema_version":"addp.authorization_basis_revision/v1","generation":"03c40e08-fcbf-468c-932e-d54da85ca8e7","version":"03"}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/meta/runtime/data-items/authorization-facts/revision" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer current-system" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("wrong owner transport")
				}
				fmt.Fprint(w, body)
			}))
			defer owner.Close()
			tokens := ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
				if tenant != 7 {
					t.Error("wrong Tenant")
				}
				return "current-system", nil
			})
			c := NewMetaClient(owner.URL, tokens)
			for i := 0; i < 2; i++ {
				got, err := c.ReadAuthorizationBasisRevision(t.Context(), 7)
				if body == `{"schema_version":"addp.authorization_basis_revision/v1","generation":"03c40e08-fcbf-468c-932e-d54da85ca8e7","version":"3"}` {
					if err != nil || got.Version != "3" {
						t.Fatal(got, err)
					}
				} else if err == nil {
					t.Fatal("accepted invalid revision")
				}
			}
			if calls != 2 {
				t.Fatal("revision was cached or retried")
			}
		})
	}
}
