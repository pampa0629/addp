package iam

import (
	"fmt"
	"strings"
	"testing"
)

func TestSystemBasisCredentialIsOptionalAndIndependent(t *testing.T) {
	secrets := map[string]string{}
	for i, id := range builtinServiceClientIDs {
		secrets[id] = fmt.Sprintf("%032d", i+1)
	}
	secrets["addp-system"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err != nil {
		t.Fatalf("optional System credential: %v", err)
	}
	secrets["addp-system"] = strings.Repeat("s", 32)
	if err := validateBuiltinServiceSecrets(secrets); err != nil {
		t.Fatal(err)
	}
	secrets["addp-system"] = secrets["addp-catalog"]
	if err := validateBuiltinServiceSecrets(secrets); err == nil {
		t.Fatal("System must not borrow Catalog credential")
	}
	secrets["addp-system"] = ""
	secrets["addp-catalog"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err == nil {
		t.Fatal("business credentials remain required")
	}
}

func TestLogObserverCredentialIsOptionalAndIndependent(t *testing.T) {
	secrets := map[string]string{}
	for i, id := range builtinServiceClientIDs {
		secrets[id] = fmt.Sprintf("%032d", i+1)
	}
	secrets["addp-log-observer"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err != nil {
		t.Fatal(err)
	}
	secrets["addp-log-observer"] = secrets["addp-manager"]
	if err := validateBuiltinServiceSecrets(secrets); err == nil {
		t.Fatal("observer borrowed a business credential")
	}
	secrets["addp-log-observer"] = ""
	secrets["addp-manager"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err == nil {
		t.Fatal("required business credential became optional")
	}
}

func TestPrometheusCredentialIsOptionalAndIndependent(t *testing.T) {
	secrets := map[string]string{}
	for i, id := range builtinServiceClientIDs {
		secrets[id] = fmt.Sprintf("%032d", i+1)
	}
	secrets["addp-prometheus"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"addp-monitor", "addp-log-observer"} {
		secrets["addp-prometheus"] = secrets[other]
		if err := validateBuiltinServiceSecrets(secrets); err == nil {
			t.Fatalf("Prometheus borrowed %s credential", other)
		}
	}
	secrets["addp-prometheus"] = ""
	secrets["addp-monitor"] = ""
	if err := validateBuiltinServiceSecrets(secrets); err == nil {
		t.Fatal("required Monitor credential became optional")
	}
	for _, id := range builtinTenantRuntimeServiceClientIDs {
		if id == "addp-prometheus" {
			t.Fatal("Prometheus acquired tenant runtime scope")
		}
	}
}
