package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func managerPGSources(t *testing.T) []authorizationfacts.Binding {
	t.Helper()
	result := []authorizationfacts.Binding{}
	for _, name := range []string{"C", "D"} {
		b, err := authorizationfacts.NativeCandidateBinding(plugin.TabularItemPath(12, "schema", "public", name))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, b)
	}
	return result
}
func TestManagerPGReadHandoffUsesServiceAndSameRequestUserWithoutCaching(t *testing.T) {
	sources := managerPGSources(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer manager-service" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("wrong service transport")
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/system/engine-access/read-checks/manager-pg-") {
			t.Error("wrong route")
		}
		var request ManagerPGReadCheckRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.UserCredential != "addp_at_user" || !reflect.DeepEqual(request.Sources, sources) {
			t.Error("lost original user or complete ReadSet", err)
		}
		_, _ = w.Write([]byte(`{"observed_at":"2026-11-13T12:00:00Z"}`))
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("manager-service"), server.Client())
	for i := 0; i < 2; i++ {
		for _, check := range []func(context.Context, uint, string, []authorizationfacts.Binding) error{c.CheckManagerPGPreviewRead, c.CheckManagerPGProfileResultRead} {
			if err := check(t.Context(), 3, "addp_at_user", sources); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 4 {
		t.Fatal("cached a read decision", calls)
	}
}
func TestManagerPGReadFailureIsSingleRequestAndDoesNotLeak(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"error":"addp_at_user"}`, ErrManagerPreviewCredentialRejected},
		{403, `{"error":"private source"}`, ErrManagerPreviewReadDenied},
		{503, `{"error":"private dependency"}`, ErrManagerPreviewReadUnavailable},
		{307, ``, ErrManagerPreviewReadUnavailable},
		{200, `{}`, ErrManagerPreviewReadUnavailable},
		{200, `not json`, ErrManagerPreviewReadUnavailable},
		{200, strings.Repeat(" ", 4097), ErrManagerPreviewReadUnavailable},
		{201, `{"observed_at":"2026-11-13T12:00:00Z"}`, ErrManagerPreviewReadUnavailable},
	} {
		t.Run(http.StatusText(tc.status)+tc.body[:min(len(tc.body), 10)], func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			transport := server.Client()
			c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("manager-service"), transport)
			err := c.CheckManagerPGPreviewRead(t.Context(), 3, "addp_at_user", managerPGSources(t))
			if !errors.Is(err, tc.want) || err.Error() != tc.want.Error() || calls != 1 || transport.CheckRedirect != nil {
				t.Fatal("retry, leak or shared-client mutation", err, calls)
			}
		})
	}
}
func TestManagerPGHandoffRejectsInvalidEvidenceBeforeTransport(t *testing.T) {
	c := NewSystemServiceClient("http://must-not-be-contacted.invalid", nil, nil)
	for _, mutate := range []func(*ManagerPGReadCheckRequest){
		func(r *ManagerPGReadCheckRequest) { r.Sources = nil },
		func(r *ManagerPGReadCheckRequest) { r.Sources = append(r.Sources, r.Sources[0]) },
		func(r *ManagerPGReadCheckRequest) { r.Sources[0].BoundaryHash = strings.Repeat("a", 64) },
		func(r *ManagerPGReadCheckRequest) { r.UserCredential = "addp_sat_machine" },
		func(r *ManagerPGReadCheckRequest) { r.UserCredential = "addp_at_" },
		func(r *ManagerPGReadCheckRequest) { r.UserCredential = "addp_at_user extra" },
		func(r *ManagerPGReadCheckRequest) {
			b, _ := authorizationfacts.NativeCandidateBinding(plugin.TabularItemPath(13, "schema", "public", "D"))
			r.Sources[1] = b
		},
		func(r *ManagerPGReadCheckRequest) {
			b, _ := authorizationfacts.NativeCandidateBinding(plugin.TabularItemPath(12, "database", "public", "D"))
			r.Sources[1] = b
		},
	} {
		r := ManagerPGReadCheckRequest{UserCredential: "addp_at_user", Sources: managerPGSources(t)}
		mutate(&r)
		if r.Validate() == nil || !errors.Is(c.CheckManagerPGPreviewRead(t.Context(), 3, r.UserCredential, r.Sources), ErrManagerPreviewReadDenied) {
			t.Fatal("invalid evidence accepted")
		}
	}
}
