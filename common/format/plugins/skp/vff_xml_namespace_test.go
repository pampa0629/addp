package skp

import (
	"context"
	"testing"
)

func TestVFFXMLRejectsReservedNamespaceBindingConfusion(t *testing.T) {
	for _, binding := range []string{`xmlns:xml="urn:wrong"`, `xmlns:other="http://www.w3.org/XML/1998/namespace"`, `xmlns:other="http://www.w3.org/2000/xmlns/"`, `xmlns:xmlns="urn:wrong"`, `xmlns:other=""`, `xmlns:a:b="urn:wrong"`} {
		raw := []byte(`<root xmlns="urn:test" ` + binding + `/>`)
		if got, err := readVFFXML(context.Background(), raw, &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil); err == nil || got != nil {
			t.Fatalf("invalid namespace binding accepted: %s", binding)
		}
	}
	for _, binding := range []string{`xmlns:xml="http://www.w3.org/XML/1998/namespace"`, `xmlns:unused="urn:valid"`} {
		if _, err := readVFFXML(context.Background(), []byte(`<root xmlns="urn:test" `+binding+`/>`), &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil); err != nil {
			t.Fatalf("legal namespace declaration rejected: %s: %v", binding, err)
		}
	}
}
