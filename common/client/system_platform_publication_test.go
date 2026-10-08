package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
)

func TestSystemPlatformPublicationCheckUsesOnlyFreshBoundPlatformIdentity(t *testing.T) {
	binding := authorization.PlatformPublicationCheck{Capability: "transfer.task.create", Revision: "2", Digest: strings.Repeat("a", 64)}
	result := authorization.PlatformPublicationObservation{PlatformPublicationCheck: binding, ContextType: "platform", ClientID: "addp-ontology", PrincipalID: "41", PrincipalType: "service_principal", AuthorizationVersion: "3"}
	data, _ := json.Marshal(result)
	body, status, requests := string(data), 200, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/runtime/platform-definition-publication-checks" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer platform-token" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		var got authorization.PlatformPublicationCheck
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != binding {
			t.Errorf("binding=%+v %v", got, err)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	for i := 0; i < 2; i++ {
		if got, err := c.CheckPlatformPublication(context.Background(), binding); err != nil || got == nil || *got != result {
			t.Fatalf("got=%+v %v", got, err)
		}
	}
	if requests != 2 {
		t.Fatal("observation was cached")
	}
	if got, err := c.WithTenantID(1).CheckPlatformPublication(context.Background(), binding); err == nil || got != nil || requests != 2 {
		t.Fatal("Tenant client sent platform check")
	}
	invalid := binding
	invalid.Revision = "01"
	if got, err := c.CheckPlatformPublication(context.Background(), invalid); err == nil || got != nil || requests != 2 {
		t.Fatal("invalid binding sent")
	}
	for _, invalidBody := range []string{"{}", string(data) + "{}", strings.Replace(string(data), `"revision":"2"`, `"revision":"3"`, 1), strings.Replace(string(data), `"principal_type":"service_principal"`, `"principal_type":"user"`, 1), strings.TrimSuffix(string(data), "}") + `,"token":"secret"}`, string(data) + strings.Repeat(" ", 2048)} {
		body = invalidBody
		if got, err := c.CheckPlatformPublication(context.Background(), binding); err == nil || got != nil {
			t.Fatal("invalid observation accepted")
		}
	}
	for _, code := range []int{401, 403, 500, 503} {
		status, body = code, `{"error_code":"permission_denied","error":"safe"}`
		before := requests
		if got, err := c.CheckPlatformPublication(context.Background(), binding); got != nil {
			t.Fatal("error returned observation")
		} else if actual, ok := SystemAPIStatusCode(err); !ok || actual != code {
			t.Fatalf("status=%d err=%v", actual, err)
		}
		if requests != before+1 {
			t.Fatal("noninvalidating token source retried")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := requests
	if got, err := c.CheckPlatformPublication(ctx, binding); err == nil || got != nil || requests != before {
		t.Fatal("cancelled request performed")
	}
}
