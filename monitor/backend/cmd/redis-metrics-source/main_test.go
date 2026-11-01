package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceConfigurationRejectsUnknownDuplicateAndOversizedInput(t *testing.T) {
	password := strings.Repeat("a", 64)
	for _, value := range []string{
		`{"scope":"business","password":"` + password + `","Scope":"infra"}`,
		`{"scope":"business","scope":"infra","password":"` + password + `"}`,
		`{"scope":"tenant","password":"` + password + `"}`,
		`{"scope":"business","password":"bad"}`,
		`{"scope":"business","password":"` + password + `"}` + strings.Repeat(" ", 32768),
		`{"scope":"business","password":"` + password + `"} {}`,
	} {
		if _, err := decode(strings.NewReader(value)); err == nil {
			t.Fatal("invalid private configuration accepted")
		}
	}
	if _, err := decode(strings.NewReader(`{"scope":"business","password":"` + password + `","address":"127.0.0.1:6379"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAccountPlanUsesTheUniqueACLRecipeAndNoPlaintextSecret(t *testing.T) {
	password := strings.Repeat("b", 64)
	for _, scope := range []string{"infra", "business"} {
		var output bytes.Buffer
		if err := plan(configuration{Scope: scope, Password: password, Owner: strings.Repeat("c", 64)}, &output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), password) {
			t.Fatal("plaintext credential escaped account plan")
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(output.Bytes(), &result); err != nil || len(result) != 5 {
			t.Fatal("invalid account plan")
		}
		var disabled string
		_ = json.Unmarshal(result["disabled_record"], &disabled)
		if !strings.HasSuffix(disabled, "\nuser addp_"+scope+"_observer reset off\n") {
			t.Fatal("disabled record retained permissions")
		}
	}
}
