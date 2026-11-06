package skp

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestVFFSKCSubstitutionSeparatesEdgesAffiliationsAndEffectivePairs(t *testing.T) {
	const root = `<x:import namespace="urn:b" schemaLocation="shared.xsd"/><x:complexType name="MiddleType"><x:complexContent><x:extension base="b:HeadType"/></x:complexContent></x:complexType><x:element name="Middle" abstract="true" type="r:MiddleType" substitutionGroup="b:Head"/><x:element name="Leaf" type="r:MiddleType" substitutionGroup="r:Middle"/>`
	const imported = `<x:complexType name="HeadType"/><x:element name="Head" type="b:HeadType" abstract="true"/>`
	for _, tc := range []struct {
		name, head, root string
		verified         bool
		pairs, pending   int
	}{
		{"transitive", imported, root, true, 2, 0},
		{"head blocks substitution", strings.Replace(imported, `abstract="true"`, `abstract="true" block="substitution"`, 1), root, true, 1, 0},
		{"head blocks type extension", strings.Replace(imported, `abstract="true"`, `abstract="true" block="extension"`, 1), root, true, 1, 0},
		{"head excludes derivation", strings.Replace(imported, `abstract="true"`, `abstract="true" final="extension"`, 1), root, true, 1, 1},
		{"head type prohibits extension", strings.Replace(imported, `name="HeadType"`, `name="HeadType" block="extension"`, 1), root, true, 1, 0},
		{"wrong head symbol", strings.Replace(imported, `<x:element name="Head" type="b:HeadType" abstract="true"/>`, `<x:complexType name="Head"/>`, 1), root, true, 1, 1},
		{"unverified import", imported, strings.Replace(root, `schemaLocation="shared.xsd"`, `schemaLocation="https://example.invalid/shared.xsd"`, 1), false, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := skcDocumentFiles(t)
			files["Schemas/root.xsd"] = skcGraphSchema("urn:r", tc.root)
			files["Schemas/root.xsd.filter"] = []byte("MiddleType")
			files["Schemas/shared.xsd"] = skcGraphSchema("urn:b", tc.head)
			binding := skcGraphRead(t, files)[0]
			if len(binding.schemaLinks) != 1 || binding.schemaLinks[0].namespaceChecked != tc.verified {
				t.Fatalf("unexpected dependency facts: %#v", binding.schemaLinks)
			}
			issues := binding.rules[0].issues
			if slices.Contains(issues, "schema dependency graph not bound") || slices.Contains(issues, "unresolved complexType QName: b:HeadType") == tc.verified {
				t.Fatalf("unexpected rule diagnostics: %v", issues)
			}
			facts := binding.substitutions
			if facts == nil || len(facts.affiliations) != 2 || len(facts.pairs) != tc.pairs || len(facts.pending) != tc.pending {
				t.Fatalf("unexpected substitution facts: %#v", facts)
			}
			for _, pair := range facts.pairs {
				if pair.member.name != "Leaf" || pair.head.name != "Middle" && (pair.head.name != "Head" || tc.pairs != 2) {
					t.Fatalf("unexpected effective substitution: %#v", pair)
				}
			}
		})
	}
}

func TestVFFSKCRejectsInvalidSubstitutionDeclarationScopeAndCycles(t *testing.T) {
	for _, declaration := range []string{
		`<x:element name="One" type="x:string" substitutionGroup="r:Two"/><x:element name="Two" type="x:string" substitutionGroup="r:One"/>`,
		`<x:complexType name="Holder"><x:sequence><x:element name="Child" substitutionGroup="r:Head"/></x:sequence></x:complexType>`,
	} {
		files := skcDocumentFiles(t)
		files["Schemas/root.xsd"] = skcGraphSchema("urn:r", declaration)
		got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, files), skcBindingLimits())
		if err == nil || got != nil {
			t.Fatalf("invalid substitution returned partial facts: %#v, %v", got, err)
		}
	}
}

func TestVFFSKCSubstitutionChecksInheritedParticleRestrictions(t *testing.T) {
	const base = `<x:complexType name="Ancestor"><x:sequence><x:element name="Value" type="x:int"/></x:sequence></x:complexType><x:complexType name="Base"><x:complexContent><x:extension base="r:Ancestor"><x:sequence><x:element name="Optional" type="x:int" minOccurs="0"/></x:sequence></x:extension></x:complexContent></x:complexType>`
	const groups = `<x:element name="Head" type="r:Base"/><x:element name="Member" type="r:MemberType" substitutionGroup="r:Head"/><x:element name="Unrelated" type="r:Base" substitutionGroup="r:Head"/>`
	for _, tc := range []struct {
		name, particle, diagnostic string
	}{
		{"exact", `<x:element name="Value" type="x:int"/>`, ""},
		{"same type with another prefix", `<x:element xmlns:alias="http://www.w3.org/2001/XMLSchema" name="Value" type="alias:int"/>`, ""},
		{"different type", `<x:element name="Value" type="x:string"/>`, "type compatibility unproved for Value"},
		{"different type with same local name", `<x:element xmlns:other="urn:other" name="Value" type="other:int"/>`, "type compatibility unproved for Value"},
		{"implicit type", `<x:element name="Value"/>`, "type compatibility unproved for Value"},
		{"conflicting inline type", `<x:element name="Value" type="x:int"><x:simpleType><x:restriction base="x:string"/></x:simpleType></x:element>`, "type compatibility unproved for Value"},
		{"wider maximum", `<x:element name="Value" type="x:int" maxOccurs="2"/>`, "widens Value maxOccurs from 1 to 2"},
		{"unbounded maximum", `<x:element name="Value" type="x:int" maxOccurs="unbounded"/>`, "widens Value maxOccurs from 1 to unbounded"},
		{"lower minimum", `<x:element name="Value" type="x:int" minOccurs="0"/>`, "lowers Value minOccurs from 1 to 0"},
		{"required omission", "", "omits required Value"},
		{"different namespace", `<x:element name="Value" type="x:int" form="qualified"/>`, "occurrence restriction unproved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := skcDocumentFiles(t)
			derived := `<x:complexType name="MemberType"><x:complexContent><x:restriction base="r:Base"><x:sequence>` + tc.particle + `</x:sequence></x:restriction></x:complexContent></x:complexType>`
			files["Schemas/root.xsd"] = skcGraphSchema("urn:r", base+derived+groups)
			files["Schemas/root.xsd.filter"] = []byte("MemberType")
			facts := skcGraphRead(t, files)[0].substitutions
			if len(facts.affiliations) != 2 {
				t.Fatalf("lost direct affiliations: %#v", facts.affiliations)
			}
			if tc.diagnostic == "" {
				if len(facts.pairs) != 2 || len(facts.pending) != 0 {
					t.Fatalf("valid particle restriction unresolved: %#v", facts)
				}
				return
			}
			if len(facts.pairs) != 1 || facts.pairs[0].member.name != "Unrelated" || len(facts.pending) != 1 || facts.pending[0].member.name != "Member" || !strings.Contains(facts.pending[0].reason, tc.diagnostic) {
				t.Fatalf("unexpected restriction outcome: %#v", facts)
			}
		})
	}
}
