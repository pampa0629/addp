package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUserModuleProjectionIsBoundedAndNeverUsesMachineAuthority(t *testing.T) {
	body := `{"modules":[{"module_name":"meta","version":1,"enabled":true,"private_endpoint":"must-not-project"}],"count":1}`
	status, calls := 200, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer addp_at_human" || r.URL.Path != "/api/v1/system/platform/modules" || r.URL.RawQuery != "" {
			t.Error("unexpected authority or route")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("machine-must-not-be-used"), server.Client())
	rows, err := c.GetModulesForUser(context.Background(), "addp_at_human")
	if err != nil || len(rows) != 1 || rows[0].ModuleName != "meta" {
		t.Fatal(rows, err)
	}
	for _, bad := range []string{`{}`, `{"modules":null,"count":0}`, `{"modules":[],"count":1}`, `{"modules":[{"module_name":"meta","version":1}],"count":1}`, `{"modules":[{"module_name":"meta","version":1,"enabled":true},{"module_name":"meta","version":1,"enabled":true}],"count":2}`} {
		body = bad
		if rows, err := c.GetModulesForUser(context.Background(), "addp_at_human"); err == nil || rows != nil {
			t.Fatal("invalid projection accepted")
		}
	}
	status = 403
	body = `{"error_code":"permission_denied","error":"denied"}`
	before := calls
	_, err = c.GetModulesForUser(context.Background(), "addp_at_human")
	code, ok := SystemAPIStatusCode(err)
	if !ok || code != 403 || calls != before+1 {
		t.Fatal("permission failure was masked or retried", err, calls)
	}
	before = calls
	if _, err := c.GetModulesForUser(context.Background(), "machine-token"); err == nil || calls != before {
		t.Fatal("non-human credential reached transport")
	}
}
