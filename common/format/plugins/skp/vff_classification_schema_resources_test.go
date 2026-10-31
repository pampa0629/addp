package skp

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestVFFSKCSchemaResourceRolesAcrossOpaqueSubtrees(t *testing.T) {
	raw := []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:annotation><xs:appinfo source="file:///not-read"/><xs:documentation source="../not-guessed">text</xs:documentation></xs:annotation><xs:notation name="n" public="urn:public" system="https://not-fetched"/><xs:element name="body"><xs:annotation><xs:documentation source=""/></xs:annotation><xs:complexType><xs:attribute name="href" type="xs:anyURI" default="https://data-only"/></xs:complexType></xs:element><xs:simpleType name="data"><xs:restriction base="xs:string"><xs:enumeration value="https://literal-only"/></xs:restriction></xs:simpleType></xs:schema>`)
	remaining := 5
	got, err := readVFFSKCSchema(context.Background(), "Schemas/resource.xsd", map[string][]byte{"Schemas/resource.xsd": raw}, &vffXMLBudget{limits: skcDocumentLimits().xml}, &remaining)
	if err != nil || len(got.resources) != 5 || len(got.dependencies) != 0 || remaining != 0 || len(got.opaque) != 4 {
		t.Fatalf("resource roles/counts: %#v %v", got, err)
	}
	want := []struct{ kind, attribute, value string }{{"appinfo", "source", "file:///not-read"}, {"documentation", "source", "../not-guessed"}, {"notation", "system", "https://not-fetched"}, {"notation", "public", "urn:public"}, {"documentation", "source", ""}}
	for i, resource := range got.resources {
		if resource.kind != want[i].kind || resource.attribute != want[i].attribute || resource.value != want[i].value || resource.sha256 != sha256.Sum256(raw[resource.begin:resource.end]) {
			t.Fatal("typed resource identity/source location lost")
		}
	}
	remaining = 4
	if got, err := readVFFSKCSchema(context.Background(), "schema.xsd", map[string][]byte{"schema.xsd": raw}, &vffXMLBudget{limits: skcDocumentLimits().xml}, &remaining); err == nil || got != nil {
		t.Fatal("resource declaration budget bypass")
	}
}

func TestVFFSKCSchemaRejectsMisplacedLoaderRoles(t *testing.T) {
	for _, fragment := range []string{`<xs:annotation><xs:include schemaLocation="same.xsd"/></xs:annotation>`, `<xs:element name="x"><xs:import namespace="urn:foreign"/></xs:element>`, `<xs:annotation><xs:schema><xs:redefine schemaLocation="same.xsd"/></xs:schema></xs:annotation>`, `<xs:documentation source="http://invalid-placement"/>`, `<xs:annotation><xs:notation name="x" system="http://invalid-placement"/></xs:annotation>`} {
		raw := []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `">` + fragment + `</xs:schema>`)
		remaining := 100
		if got, err := readVFFSKCSchema(context.Background(), "schema.xsd", map[string][]byte{"schema.xsd": raw}, &vffXMLBudget{limits: skcDocumentLimits().xml}, &remaining); err == nil || got != nil {
			t.Fatalf("misplaced loader role accepted: %s", fragment)
		}
	}
	// Explicitly unknown attributes remain opaque. Their URI-like values do
	// not magically acquire a declared resource role or prove resource absence.
	raw := []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:element name="x" unknownResource="http://unproved"/></xs:schema>`)
	remaining := 100
	got, err := readVFFSKCSchema(context.Background(), "schema.xsd", map[string][]byte{"schema.xsd": raw}, &vffXMLBudget{limits: skcDocumentLimits().xml}, &remaining)
	if err != nil || len(got.resources) != 0 || len(got.opaque) != 1 || !strings.Contains(string(raw[got.opaque[0].begin:got.opaque[0].end]), "http://unproved") {
		t.Fatal("unknown semantic subtree was silently claimed")
	}
}
