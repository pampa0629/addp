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
