package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcComplexSchema(body, declarations string) string {
	return `<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:t" xmlns:e="urn:external" targetNamespace="urn:t">` + declarations + `<x:complexType name="Value">` + body + `</x:complexType><x:complexType name="Holder"><x:sequence><x:element name="F" type="t:Value"/></x:sequence></x:complexType></x:schema>`
}

func TestVFFSKCComplexValuesRetainOccurrenceTreeAndExternalReferences(t *testing.T) {
	schema := skcComplexSchema(`<x:sequence><x:element ref="e:long-wrapper" minOccurs="3" maxOccurs="4"/></x:sequence><x:attribute ref="e:itemType" fixed="e:long-wrapper"/><x:attribute ref="e:cType" fixed="list"/><x:attribute ref="e:arraySize" use="optional"/>`, "")
	got := skcSemanticsRead(t, schema, "Holder { F }")
	v := got[0].rules[0].fields[0].candidates[0].value
	if v == nil || len(v.structures) != 7 || len(v.references) != 5 || len(v.declarations) != 1 || len(v.terminals) != 0 || len(v.issues) != 4 {
		t.Fatalf("complex facts lost: %#v", v)
	}
	for i, want := range []struct{ kind, name, min, max string }{
		{"element", "F", "1", "1"}, {"complexType", "Value", "", ""}, {"sequence", "", "1", "1"},
		{"element", "", "3", "4"}, {"attribute", "", "", ""}, {"attribute", "", "", ""}, {"attribute", "", "", ""},
	} {
		s := v.structures[i]
		if s.source.kind != want.kind || s.source.name != want.name || s.minOccurs != want.min || s.maxOccurs != want.max || s.source.sha256 != sha256.Sum256([]byte(schema)[s.source.begin:s.source.end]) {
			t.Fatalf("ordered structure identity/bounds at %d", i)
		}
	}
	if v.structures[3].parent != v.structures[2].source.begin || v.structures[2].parent != v.structures[1].source.begin || v.structures[4].attributes["fixed"] != "e:long-wrapper" || v.structures[6].attributes["use"] != "optional" {
		t.Fatal("parent/constraint declarations lost")
	}
	for i, r := range v.references[1:] {
		role := "attributeRef"
		if i == 0 {
			role = "elementRef"
		}
		if r.role != role || r.name.Space != "urn:external" || r.source.sha256 != sha256.Sum256([]byte(schema)[r.source.begin:r.source.end]) {
			t.Fatal("external reference role or source identity lost")
		}
	}
	// fixed is a raw value, not a second QName/resource reference.
	if len(v.references) != 5 {
		t.Fatal("fixed data value guessed as dependency")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("complex declaration facts opened composition authorization")
	}
}

func TestVFFSKCComplexValuesResolveExactLocalMemberSymbolSpaces(t *testing.T) {
	schema := skcComplexSchema(`<x:sequence minOccurs="0" maxOccurs="unbounded"><x:element ref="t:Member" minOccurs="+0003" maxOccurs="0004"/><x:element name="Inline"><x:complexType><x:choice><x:element name="Leaf" type="x:boolean"/></x:choice></x:complexType></x:element></x:sequence><x:attribute ref="t:Member"/>`, `<x:element name="Member" type="x:long"/><x:attribute name="Member" type="x:string"/>`)
	v := skcSemanticsRead(t, schema, "Holder { F }")[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || len(v.terminals) != 3 || v.terminals[0] != (xml.Name{Space: vffXSDNS, Local: "long"}) || v.terminals[1].Local != "boolean" || v.terminals[2].Local != "string" {
		t.Fatalf("member symbols confused: %#v", v)
	}
	if s := v.structures[2]; s.minOccurs != "0" || s.maxOccurs != "unbounded" {
		t.Fatal("sequence range not preserved")
	}
	if s := v.structures[3]; s.minOccurs != "3" || s.maxOccurs != "4" || s.attributes["minOccurs"] != "+0003" {
		t.Fatal("effective bounds lost original lexical witness")
	}
	// A namespace shadow cannot resolve to the local declaration by basename.
	schema = strings.Replace(schema, `ref="t:Member" minOccurs=`, `xmlns:t="urn:other" ref="t:Member" minOccurs=`, 1)
	v = skcSemanticsRead(t, schema, "Holder { F }")[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 1 || len(v.terminals) != 2 || v.references[1].name.Space != "urn:other" {
		t.Fatal("shadowed member resolved to wrong schema")
	}
}

func TestVFFSKCComplexValuesDoNotHideUnknownStructuresOrAttributes(t *testing.T) {
	for _, body := range []string{
		`<x:complexContent><x:extension base="e:Base"/></x:complexContent>`,
		`<x:simpleContent><x:extension base="x:string"/></x:simpleContent>`,
		`<x:sequence><x:any/></x:sequence>`,
		`<x:attributeGroup ref="e:Attributes"/>`,
		`<x:sequence><x:element ref="t:Missing"/></x:sequence>`,
		`<x:sequence unknown="uninterpreted"><x:element name="F" type="x:int"/></x:sequence>`,
	} {
		v := skcSemanticsRead(t, skcComplexSchema(body, ""), "Holder { F }")[0].rules[0].fields[0].candidates[0].value
		if len(v.issues) == 0 {
			t.Fatal("unknown complex semantics erased")
		}
	}
}

func TestVFFSKCComplexValuesRejectInvalidBoundsReferencesAndCycles(t *testing.T) {
	for _, body := range []string{
		`<x:sequence><x:element name="V" type="x:int" minOccurs="5" maxOccurs="4"/></x:sequence>`,
		`<x:sequence minOccurs="-1"/>`, `<x:sequence maxOccurs=""/>`, `<x:sequence maxOccurs="1.5"/>`,
		`<x:sequence minOccurs="&#160;0"/>`,
		`<x:sequence minOccurs="unbounded"/>`, `<x:sequence maxOccurs="0x10"/>`,
		`<x:sequence><x:element ref="t:F" name="F"/></x:sequence>`,
		`<x:sequence><x:element ref="t:F" type="x:int"/></x:sequence>`,
		`<x:sequence><x:element ref="t:F"><x:simpleType><x:restriction base="x:int"/></x:simpleType></x:element></x:sequence>`,
		`<x:sequence><x:element name="Self" type="t:Value"/></x:sequence>`,
		`<x:sequence><x:element ref="missing:Item"/></x:sequence>`,
		`<x:sequence><x:element type="x:int"/></x:sequence>`,
		`<x:sequence><x:element name="" type="x:int"/></x:sequence>`,
		`<x:attribute name="A" type="x:int" fixed="1" default="2"/>`,
		`<x:attribute name="A" type="x:int" use="required" default="2"/>`,
		`<x:attribute name="A" type="x:int" use="unknown"/>`,
	} {
		f := skcDocumentFiles(t)
		f["Schemas/root.xsd"] = []byte(skcComplexSchema(body, ""))
		f["Schemas/root.xsd.filter"] = []byte("Holder { F }")
		if got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatalf("invalid complex graph accepted: %s", body)
		}
	}
}

func TestVFFSKCComplexValuesKeepHugeBoundsWithoutExpandingInstances(t *testing.T) {
	large := strings.Repeat("9", 100)
	schema := skcComplexSchema(`<x:sequence minOccurs="-0" maxOccurs="`+large+`"><x:element name="V" type="x:int"/></x:sequence>`, "")
	v := skcSemanticsRead(t, schema, "Holder { F }")[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || len(v.structures) != 4 || v.structures[2].minOccurs != "0" || v.structures[2].maxOccurs != large {
		t.Fatal("large occurrence bounds overflowed or expanded")
	}
	for _, modify := range []func(*vffSKCBindingLimits){
		func(l *vffSKCBindingLimits) { l.depth = 2 },
		func(l *vffSKCBindingLimits) { l.visits = 20 },
	} {
		f := skcDocumentFiles(t)
		f["Schemas/root.xsd"] = []byte(schema)
		f["Schemas/root.xsd.filter"] = []byte("Holder { F }")
		limits := skcBindingLimits()
		modify(&limits)
		if got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), limits); err == nil || got != nil {
			t.Fatal("complex graph bypassed cumulative expansion/depth budget")
		}
	}
}
