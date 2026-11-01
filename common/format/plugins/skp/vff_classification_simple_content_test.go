package skp

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcSimpleContentFiles(t *testing.T, body, extra string) map[string][]byte {
	f := skcGraphFiles(t, `<x:import namespace="urn:b" schemaLocation="shared.xsd"/>`, `<x:element name="F" type="b:Value"/><x:element name="G" type="x:int"/>`)
	f["Schemas/shared.xsd"] = skcGraphSchema("urn:b", extra+`<x:complexType name="Value">`+body+`</x:complexType>`)
	return f
}

func TestVFFSKCSimpleContentKeepsTextBaseAndAttributeGroupRolesDistinct(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Numeric"><x:attributeGroup ref="c:Attrs"/></x:extension></x:simpleContent>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/><x:simpleType name="Numeric"><x:restriction base="x:long"><x:minInclusive value="-10"/></x:restriction></x:simpleType>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attributeGroup name="Attrs"><x:attribute name="id" type="x:ID" use="optional"/><x:attribute name="path" type="x:NMTOKENS"/><x:attribute name="pos"><x:simpleType><x:list itemType="x:integer"/></x:simpleType></x:attribute></x:attributeGroup>`)
	v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || len(v.references) != 7 || len(v.terminals) != 4 || len(v.facets) != 1 {
		t.Fatalf("simple content graph: %#v", v)
	}
	for i, name := range []string{"long", "ID", "NMTOKENS", "integer"} {
		if v.terminals[i].Local != name || v.terminals[i].Space != vffXSDNS {
			t.Fatal("text/attribute terminal graph changed")
		}
	}
	base, group := v.references[1], v.references[3]
	if base.role != "simpleContentBase" || base.source.kind != "extension" || base.source.member != "Schemas/shared.xsd" || base.target == nil || base.target.kind != "simpleType" || base.target.name != "Numeric" || len(base.via) != 0 {
		t.Fatal("content base identity/role lost")
	}
	if group.role != "attributeGroupRef" || group.target == nil || group.target.name != "Attrs" || group.target.member != "Schemas/extra.xsd" || len(group.via) != 1 || group.target.sha256 != sha256.Sum256(f[group.target.member][group.target.begin:group.target.end]) {
		t.Fatal("exact attached attribute group binding lost")
	}
	for _, ref := range v.references[4:6] {
		if ref.source.kind != "attribute" || ref.role != "type" {
			t.Fatal("attribute data mislabeled as content base")
		}
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("simple content graph opened composition admission")
	}
}

func TestVFFSKCSimpleContentCanDeriveOnlyFromSimpleContentComplexBases(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Base"><x:attribute name="B" type="x:boolean"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:long"><x:attribute name="A" type="x:string"/></x:extension></x:simpleContent></x:complexType>`)
	v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || len(v.terminals) != 3 || v.terminals[0].Local != "long" || v.terminals[1].Local != "string" || v.terminals[2].Local != "boolean" || v.references[1].target.kind != "complexType" || v.references[2].role != "simpleContentBase" {
		t.Fatal("simple-content complex base or attached attributes lost")
	}
	// An element particle cannot become a scalar content base.
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `<x:simpleContent><x:extension base="x:long"><x:attribute name="A" type="x:string"/></x:extension></x:simpleContent>`, `<x:sequence><x:element name="N" type="x:int"/></x:sequence>`, 1))
	v = skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) == 0 || len(v.terminals) != 1 || v.terminals[0].Local != "boolean" {
		t.Fatal("non-simple complex base treated as text")
	}
}

func TestVFFSKCSimpleContentKeepsUnknownAndUnsupportedSemanticsExplicit(t *testing.T) {
	for _, body := range []string{
		`<x:simpleContent><x:restriction base="x:string"/></x:simpleContent>`,
		`<x:simpleContent><x:extension base="x:anyType"/></x:simpleContent>`,
		`<x:simpleContent><x:extension base="x:long"><x:anyAttribute/></x:extension></x:simpleContent>`,
		`<x:simpleContent><x:extension base="x:long"><x:element name="N" type="x:int"/></x:extension></x:simpleContent>`,
		`<x:simpleContent><x:extension base="x:long"><x:attributeGroup ref="c:Missing"/></x:extension></x:simpleContent>`,
		`<x:simpleContent unknown="opaque"><x:extension base="x:long"/></x:simpleContent>`,
	} {
		v := skcGraphRead(t, skcSimpleContentFiles(t, body, ""))[0].rules[0].fields[0].candidates[0].value
		if len(v.issues) == 0 {
			t.Fatal("unsupported simple-content semantics silently accepted")
		}
	}
}

func TestVFFSKCSimpleContentRejectsMalformedSourcesAndCycles(t *testing.T) {
	for _, tc := range []struct{ body, extra string }{
		{`<x:simpleContent/>`, ""},
		{`<x:simpleContent><x:extension/></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="missing:Type"/></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="x:long"/><x:extension base="x:string"/></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="x:long"/></x:simpleContent><x:sequence/>`, ""},
		{`<x:simpleContent><x:extension base="x:long"><x:attributeGroup name="Inline"/></x:extension></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="x:long"><x:attributeGroup name="Bad" ref="b:A"/></x:extension></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="x:long"><x:attributeGroup ref="b:A"><x:attribute name="X" type="x:int"/></x:attributeGroup></x:extension></x:simpleContent>`, ""},
		{`<x:simpleContent><x:extension base="b:Base"/></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="b:Value"/></x:simpleContent></x:complexType>`},
		{`<x:simpleContent><x:extension base="x:long"><x:attributeGroup ref="b:A"/></x:extension></x:simpleContent>`, `<x:attributeGroup name="A"><x:attributeGroup ref="b:B"/></x:attributeGroup><x:attributeGroup name="B"><x:attributeGroup ref="b:A"/></x:attributeGroup>`},
	} {
		f := skcSimpleContentFiles(t, tc.body, tc.extra)
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("malformed or cyclic simple-content graph accepted")
		}
	}
}

func TestVFFSKCSimpleContentGroupTraversalUsesSharedBudget(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="x:long"><x:attributeGroup ref="b:A"/></x:extension></x:simpleContent>`, `<x:attributeGroup name="A"><x:attributeGroup ref="b:B"/></x:attributeGroup><x:attributeGroup name="B"><x:attribute name="N" type="x:int"/></x:attributeGroup>`)
	l := skcBindingLimits()
	l.depth = 6
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), l); err == nil || got != nil {
		t.Fatal("attribute group depth bypassed shared budget")
	}
}
