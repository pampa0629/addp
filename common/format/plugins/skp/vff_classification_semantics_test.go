package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"strings"
	"testing"
)

func skcSemanticsRead(t *testing.T, schema, filter string) []vffSKCFilterBindings {
	t.Helper()
	f := skcDocumentFiles(t)
	f["Schemas/root.xsd"] = []byte(schema)
	f["Schemas/root.xsd.filter"] = []byte(filter)
	got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestVFFSKCBlacklistScopesExcludeOnlyExplicitWhiteTypes(t *testing.T) {
	schema := `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" targetNamespace="urn:t"><x:attributeGroup name="Audit"><x:attribute name="Created" type="x:dateTime"/></x:attributeGroup><x:complexType name="Base"><x:attribute name="Secret" type="x:string"/><x:attributeGroup ref="t:Audit"/></x:complexType><x:complexType name="Child"><x:complexContent><x:extension base="t:Base"><x:attribute name="Own" type="x:int"/></x:extension></x:complexContent></x:complexType><x:complexType name="White"><x:attribute name="Secret" type="x:string"/></x:complexType></x:schema>`
	got := skcSemanticsRead(t, schema, "Base {}\nWhite {}\n{ Secret Audit Own }")
	scopes := got[0].rules[2].scopes
	if len(scopes) != 1 || scopes[0].typeDeclaration.name != "Child" || len(scopes[0].issues) != 0 {
		t.Fatal("wrong blacklist declared universe or inherited white exclusion")
	}
	scope := scopes[0]
	if len(scope.fields[0].candidates) != 1 || len(scope.fields[1].candidates) != 0 || len(scope.fields[2].candidates) != 1 || len(scope.groups[1].uses) != 1 {
		t.Fatal("field/group application candidates lost")
	}
	use := scope.groups[1].uses[0]
	if use.declaration.kind != "attributeGroup" || use.declaration.name != "Audit" || use.reference.sha256 != sha256.Sum256([]byte(schema)[use.reference.begin:use.reference.end]) || !strings.Contains(schema[use.reference.begin:use.reference.end], `ref="t:Audit"`) {
		t.Fatal("exact group reference use not preserved")
	}
	value := scope.fields[0].candidates[0].value
	if value == nil || len(value.issues) != 0 || len(value.terminals) != 1 || value.terminals[0] != (xml.Name{Space: vffXSDNS, Local: "string"}) {
		t.Fatal("inherited blacklist field value reference")
	}
	// Swapping which derived type is whitelisted changes the declared scope;
	// it cannot reuse an earlier scope result.
	got = skcSemanticsRead(t, schema, "Child {}\nWhite {}\n{ Secret Audit Own }")
	if len(got[0].rules[2].scopes) != 1 || got[0].rules[2].scopes[0].typeDeclaration.name != "Base" {
		t.Fatal("old whitelist scope reused")
	}
}

func TestVFFSKCBlacklistSeparatesAnonymousAndGlobalDeclarations(t *testing.T) {
	schema := `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" targetNamespace="urn:t">
<x:annotation><x:appinfo><x:element name="Example"><x:complexType/></x:element></x:appinfo></x:annotation>
<x:attribute name="Clash" type="x:string"/><x:element name="Clash" type="x:int"/>
<x:attributeGroup name="Audit"><x:attribute name="Shared" type="x:string"/></x:attributeGroup>
<x:complexType name="Named"><x:attribute name="Shared" type="x:boolean"/></x:complexType>
<x:complexType name="White"><x:sequence><x:element name="Owner"><x:complexType>
<x:sequence><x:element name="Nested"><x:complexType><x:attribute name="Shared" type="x:int"/></x:complexType></x:element></x:sequence>
<x:attributeGroup ref="t:Audit"/></x:complexType></x:element></x:sequence></x:complexType>
</x:schema>`
	got := skcSemanticsRead(t, schema, "White {}\n{ Shared Audit Clash Missing }")
	rule := got[0].rules[1]
	if len(rule.scopes) != 3 || len(rule.fields[0].candidates) != 0 || len(rule.fields[2].candidates) != 2 ||
		strings.Join(rule.issues, "|") != "blacklist global declaration ambiguous: Clash|blacklist declaration not found: Missing" {
		t.Fatalf("unexpected blacklist witnesses: %#v", rule)
	}
	for i, scope := range rule.scopes {
		field := scope.fields[0]
		wantType := []string{"boolean", "string", "int"}[i]
		if len(scope.issues) != 0 || len(field.candidates) != 1 || field.candidates[0].value == nil || len(field.candidates[0].value.terminals) != 1 ||
			field.candidates[0].value.terminals[0].Local != wantType {
			t.Fatalf("scope %d mixed local fields: %#v", i, field)
		}
		if i == 0 {
			if scope.element != nil || scope.typeDeclaration.name != "Named" {
				t.Fatal("named scope replaced by anonymous owner")
			}
			continue
		}
		owner := scope.element
		if owner == nil || owner.name != []string{"", "Owner", "Nested"}[i] || scope.typeDeclaration.name != "" || owner.member != scope.typeDeclaration.member ||
			owner.begin >= scope.typeDeclaration.begin || owner.end <= scope.typeDeclaration.end || owner.sha256 != sha256.Sum256([]byte(schema[owner.begin:owner.end])) {
			t.Fatalf("missing precise anonymous scope owner: %#v", owner)
		}
	}
	if len(rule.scopes[1].groups[1].uses) != 1 || len(rule.scopes[2].groups[1].uses) != 0 {
		t.Fatal("group use leaked across nested scopes")
	}
}

func TestVFFSKCBlacklistRejectsConflictingAnonymousTypeOwners(t *testing.T) {
	for _, element := range []string{
		`<x:element name="Owner" type="x:string"><x:complexType/></x:element>`,
		`<x:element name="Owner"><x:complexType/><x:simpleType/></x:element>`,
	} {
		files := skcDocumentFiles(t)
		files["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `">` + element + `</x:schema>`)
		files["Schemas/root.xsd.filter"] = []byte("{ Shared }")
		got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, files), skcBindingLimits())
		if err == nil || got != nil {
			t.Fatalf("conflicting anonymous owner accepted: %s", element)
		}
	}
}

func TestVFFSKCValueGraphsRetainNamedInlineUnionAndFacets(t *testing.T) {
	schema := `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" targetNamespace="urn:t"><x:simpleType name="Label"><x:restriction base="x:string"><x:enumeration value="not validated"/></x:restriction></x:simpleType><x:simpleType name="Numbers"><x:list itemType="x:int"/></x:simpleType><x:complexType name="Holder"><x:attribute name="A" type="t:Label"/><x:attribute name="B" type="t:Numbers"/><x:attribute name="C"><x:simpleType><x:union memberTypes="t:Label x:boolean"><x:simpleType><x:restriction base="x:decimal"/></x:simpleType></x:union></x:simpleType></x:attribute></x:complexType></x:schema>`
	got := skcSemanticsRead(t, schema, "Holder { A B C }")
	fields := got[0].rules[0].fields
	if len(fields) != 3 {
		t.Fatal("field count")
	}
	wants := [][]string{{"string"}, {"int"}, {"string", "boolean", "decimal"}}
	for i, f := range fields {
		v := f.candidates[0].value
		if v == nil || len(v.issues) != 0 || len(v.terminals) != len(wants[i]) {
			t.Fatalf("value reference graph %d", i)
		}
		for j, q := range v.terminals {
			if q.Space != vffXSDNS || q.Local != wants[i][j] {
				t.Fatal("wrong builtin QName")
			}
		}
		for _, r := range v.references {
			if r.source.sha256 != sha256.Sum256([]byte(schema)[r.source.begin:r.source.end]) {
				t.Fatal("value reference source identity")
			}
		}
	}
	if len(fields[0].candidates[0].value.facets) != 1 || fields[0].candidates[0].value.facets[0].kind != "enumeration" {
		t.Fatal("facet evidence lost or silently executed")
	}
	if fields[0].candidates[0].value == fields[2].candidates[0].value {
		t.Fatal("distinct field identities merged")
	}
}

func TestVFFSKCValueGraphsNeverGuessMissingForeignOrComplexTypes(t *testing.T) {
	for label, tc := range map[string]struct{ field, extra string }{
		"foreign string":     {`<x:attribute name="F" xmlns:f="urn:foreign" type="f:string"/>`, ""},
		"missing local type": {`<x:attribute name="F" type="t:Missing"/>`, ""},
		"unprefixed type has no target namespace fallback": {`<x:attribute name="F" type="string"/>`, ""},
		"same local name is not builtin":                   {`<x:attribute name="F" type="t:string"/>`, `<x:complexType name="string"/>`},
		"unknown builtin":                                  {`<x:attribute name="F" type="x:not_a_builtin"/>`, ""},
		"implicit type":                                    {`<x:attribute name="F"/>`, ""},
		"complex inline":                                   {`<x:element name="F"><x:complexType/></x:element>`, ""},
		"unknown facet":                                    {`<x:attribute name="F"><x:simpleType><x:restriction base="x:string"><x:unknown/></x:restriction></x:simpleType></x:attribute>`, ""},
	} {
		t.Run(label, func(t *testing.T) {
			field := tc.field
			if strings.Contains(field, "<x:element") {
				field = "<x:sequence>" + field + "</x:sequence>"
			}
			schema := `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" targetNamespace="urn:t">` + tc.extra + `<x:complexType name="Holder">` + field + `</x:complexType></x:schema>`
			got := skcSemanticsRead(t, schema, "Holder { F }")
			v := got[0].rules[0].fields[0].candidates[0].value
			if v == nil || len(v.issues) == 0 {
				t.Fatal("unknown value type accepted as fully referenced")
			}
		})
	}
}

func TestVFFSKCValueGraphsRejectCyclesAndConflictingSources(t *testing.T) {
	for _, tc := range []struct{ field, extra string }{
		{`<x:attribute name="F" type="t:A"/>`, `<x:simpleType name="A"><x:restriction base="t:B"/></x:simpleType><x:simpleType name="B"><x:restriction base="t:A"/></x:simpleType>`},
		{`<x:attribute name="F" type="x:string"><x:simpleType><x:restriction base="x:int"/></x:simpleType></x:attribute>`, ""},
		{`<x:attribute name="F" type="missing:string"/>`, ""},
		{`<x:attribute name="F"><x:simpleType><x:union memberTypes=""/></x:simpleType></x:attribute>`, ""},
		{`<x:attribute name="F"><x:simpleType><x:list/></x:simpleType></x:attribute>`, ""},
	} {
		f := skcDocumentFiles(t)
		f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" targetNamespace="urn:t">` + tc.extra + `<x:complexType name="Holder">` + tc.field + `</x:complexType></x:schema>`)
		f["Schemas/root.xsd.filter"] = []byte("Holder { F }")
		if got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("cyclic/conflicting value graph accepted")
		}
	}
}

func TestVFFSKCBlacklistScopesKeepIncompleteTypesExplicit(t *testing.T) {
	schema := `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:f="urn:foreign" targetNamespace="urn:t"><x:complexType name="Local"><x:attribute name="F" type="x:string"/></x:complexType><x:complexType name="Unknown"><x:complexContent><x:extension base="f:Base"><x:attribute name="F" type="x:string"/></x:extension></x:complexContent></x:complexType></x:schema>`
	got := skcSemanticsRead(t, schema, "{ F }")
	scopes := got[0].rules[0].scopes
	if len(scopes) != 2 || scopes[0].typeDeclaration.name != "Local" || len(scopes[0].issues) != 0 || scopes[1].typeDeclaration.name != "Unknown" || len(scopes[1].issues) == 0 || len(scopes[1].fields[0].candidates) != 1 {
		t.Fatal("partial local candidates concealed unknown inherited scope")
	}
}
