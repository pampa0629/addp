package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/workbench/internal/models"
)

func TestHTTPDescriptorReaderForwardsOnlyUserRequestContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/service/consumer/services/query/23" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer user-token" || r.Header.Get("Accept-Language") != "zh-CN" || r.Header.Get("X-Request-ID") != "request-1" {
			t.Fatalf("headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "schema_version":"addp.service_consumer/v1",
          "ref":{"service_type":"query","service_id":23},
          "title":"Orders","description":"Order list","status":"active","access_mode":"private",
          "contract_fingerprint":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
          "operations":[{"key":"query","method":"POST","path":"/api/query/orders/query","input_kind":"structured_query","output_kind":"tabular"}],
          "input_contract":{"kind":"structured_query","fields":[{"name":"id","type":"string","nullable":false,"selectable":true,"filterable":false,"operators":[],"sortable":true}],"named_parameters":[{"name":"person_id","type":"string","required":true,"description":"Person"}],"default_selection":["id"],"filter":{"combinators":["and","or","not"],"max_depth":16,"max_nodes":256,"max_in_values":1000},"order":{"directions":["asc","desc"],"stable_key":["id"]},"page":{"kind":"cursor","default_limit":50,"max_limit":1000},"formats":["json","csv"],"intent":{"header":"X-ADDP-Query-Intent","allowed_values":["query","export"],"default_value":"query"}},
          "output_contract":{"kind":"tabular","fields":[{"name":"id","type":"string","nullable":false,"comment":"Stable ID"}]}
        }`))
	}))
	defer server.Close()
	reader, err := NewHTTPDescriptorReader(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := reader.GetDescriptor(context.Background(), DescriptorRequest{
		BearerToken: "user-token", AcceptLanguage: "zh-CN", RequestID: "request-1",
		Ref: models.ServiceReference{ServiceType: "query", ServiceID: 23},
	})
	if err != nil {
		t.Fatalf("GetDescriptor() error = %v", err)
	}
	if descriptor.Ref.ServiceID != 23 || descriptor.InputContract.Intent.Header != "X-ADDP-Query-Intent" || len(descriptor.InputContract.NamedParameters) != 1 || descriptor.InputContract.NamedParameters[0].Name != "person_id" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestHTTPDescriptorReaderFailsClosedOnOwnerResponses(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			reader, _ := NewHTTPDescriptorReader(server.URL, server.Client())
			_, err := reader.GetDescriptor(context.Background(), DescriptorRequest{BearerToken: "token", Ref: models.ServiceReference{ServiceType: "query", ServiceID: 23}})
			want := ErrServiceAccessDenied
			if status == http.StatusInternalServerError {
				want = ErrServiceUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("GetDescriptor() error = %v, want %v", err, want)
			}
		})
	}
}
