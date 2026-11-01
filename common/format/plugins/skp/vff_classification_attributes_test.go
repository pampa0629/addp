package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcAttributeValue(t *testing.T, f map[string][]byte) *vffSKCValueBinding {
	t.Helper()
	return skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
}

func skcAttributeSet(t *testing.T, v *vffSKCValueBinding, kind, name string) *vffSKCValueAttributes {
	t.Helper()
	for _, set := range v.attributeSets {
		if set.source.kind == kind && set.source.name == name {
			return set
		}
	}
	t.Fatalf("missing attribute scope %s %s", kind, name)
	return nil
}

func TestVFFSKCAttributesInheritExactUsesAndKeepNestedTypesSeparate(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Base"><x:attribute name="B" type="x:boolean"/><x:attribute name="A" use="prohibited" type="x:string"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:long"><x:attribute name="A" type="x:string" use="required"/></x:extension></x:simpleContent></x:complexType>`)
	v := skcAttributeValue(t, f)
	base, value := skcAttributeSet(t, v, "complexType", "Base"), skcAttributeSet(t, v, "complexType", "Value")
	if len(v.issues) != 0 || len(base.uses) != 1 || len(value.uses) != 2 || base.uses[0] != value.uses[0] || value.uses[1].name != (xml.Name{Local: "B"}) {
		t.Fatal("base use removed, replaced or leaked into another scope")
	}
	// The prohibited local declaration does not override the inherited use.
	for _, use := range value.uses {
		w := use.declaration
		if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatal("attribute identity differs from exact source bytes")
		}
	}
	f = skcSimpleContentFiles(t, `<x:sequence><x:element name="Child"><x:complexType><x:attribute name="A" type="x:int"/></x:complexType></x:element></x:sequence><x:attribute name="A" type="x:string"/>`, "")
	v = skcAttributeValue(t, f)
	if len(v.issues) != 0 || len(v.attributeSets) != 2 || len(v.attributeSets[0].uses) != 1 || len(v.attributeSets[1].uses) != 1 || v.attributeSets[0].uses[0].declaration == v.attributeSets[1].uses[0].declaration {
		t.Fatal("nested complex type attribute scopes conflated")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("attribute declaration proof opened composition admission")
	}
}

func TestVFFSKCAttributesUseDeclarationOwnersFormAndGlobalReference(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="x:string"><x:attributeGroup ref="c:Attrs"/><x:attribute name="A" type="x:string"/><x:attribute name="A" form="qualified" type="x:string"/><x:attribute ref="c:Global"/></x:extension></x:simpleContent>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(skcGraphSchema("urn:c", `<x:attributeGroup name="Attrs"><x:attribute name="A" type="x:int"/><x:attribute name="B" form="unqualified" type="x:int"/></x:attributeGroup><x:attribute name="Global" type="x:string"/>`)), `targetNamespace="urn:c"`, `targetNamespace="urn:c" attributeFormDefault="qualified"`, 1))
	v := skcAttributeValue(t, f)
	uses := skcAttributeSet(t, v, "complexType", "Value").uses
	if len(v.issues) != 0 || len(uses) != 5 {
		t.Fatalf("attribute QName proof incomplete: %#v", v)
	}
	for i, name := range []xml.Name{{Space: "urn:c", Local: "A"}, {Local: "B"}, {Local: "A"}, {Space: "urn:b", Local: "A"}, {Space: "urn:c", Local: "Global"}} {
		if uses[i].name != name {
			t.Fatalf("attribute %d uses caller form/default namespace: %v", i, uses[i].name)
		}
	}
	if uses[4].source.member != "Schemas/shared.xsd" || uses[4].declaration.member != "Schemas/extra.xsd" || uses[4].source == uses[4].declaration {
		t.Fatal("global reference use and declaration identity conflated")
	}
}

func TestVFFSKCAttributesKeepRepeatedGroupUsesUnprovedAndPreserveReferences(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Base"><x:attributeGroup ref="b:Shared"/><x:attributeGroup ref="b:Shared"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:long"><x:attributeGroup ref="b:Shared"/></x:extension></x:simpleContent></x:complexType><x:attributeGroup name="Shared"><x:attribute name="A" type="x:int"/></x:attributeGroup>`)
	v := skcAttributeValue(t, f)
	if !strings.Contains(strings.Join(v.issues, " "), "repeated attribute use") || len(skcAttributeSet(t, v, "complexType", "Value").uses) != 3 {
		t.Fatal("repeated group/base paths guessed component-equivalent or discarded")
	}
	count := 0
	for _, ref := range v.references {
		if ref.role == "attributeGroupRef" {
			count++
		}
	}
	if count != 3 {
		t.Fatal("repeated group reference witnesses dropped")
	}
}

func TestVFFSKCAttributesRejectDistinctDeclarationsWithSameQName(t *testing.T) {
	for _, tc := range []struct{ body, extra string }{
		{`<x:attribute name="A" type="x:int"/><x:attribute name="A" type="x:int"/>`, ""},
		{`<x:simpleContent><x:extension base="b:Base"><x:attribute name="A" type="x:int"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:long"><x:attribute name="A" type="x:int"/></x:extension></x:simpleContent></x:complexType>`},
		{`<x:simpleContent><x:extension base="x:string"><x:attributeGroup ref="b:One"/><x:attributeGroup ref="b:Two"/></x:extension></x:simpleContent>`, `<x:attributeGroup name="One"><x:attribute name="A" type="x:int"/></x:attributeGroup><x:attributeGroup name="Two"><x:attribute name="A" type="x:int"/></x:attributeGroup>`},
		{`<x:simpleContent><x:extension base="x:string"><x:attributeGroup ref="b:One"/></x:extension></x:simpleContent>`, `<x:attribute name="A" type="x:int"/><x:attributeGroup name="One"><x:attribute ref="b:A"/><x:attribute ref="b:A"/></x:attributeGroup>`},
	} {
		f := skcSimpleContentFiles(t, tc.body, tc.extra)
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "identical QName") {
			t.Fatalf("duplicate declarations/uses not atomically rejected: %v", err)
		}
	}
}

func TestVFFSKCAttributesKeepDistinctUsesOfOneDeclarationUnproved(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Base"><x:attribute ref="b:A" use="required"/></x:extension></x:simpleContent>`, `<x:attribute name="A" type="x:int"/><x:complexType name="Base"><x:simpleContent><x:extension base="x:long"><x:attribute ref="b:A" use="optional"/></x:extension></x:simpleContent></x:complexType>`)
	v := skcAttributeValue(t, f)
	uses := skcAttributeSet(t, v, "complexType", "Value").uses
	if len(uses) != 2 || uses[0].source == uses[1].source || uses[0].declaration != uses[1].declaration || !strings.Contains(strings.Join(v.issues, " "), "multiple uses") {
		t.Fatal("different global attribute uses guessed equivalent or overwritten")
	}
}

func TestVFFSKCAttributesRejectInvalidFormsAndReferenceSources(t *testing.T) {
	for _, attr := range []string{
		`<x:attribute ref="b:A" form="qualified"/>`, `<x:attribute name="A" type="x:int" form="wrong"/>`, `<x:attribute name="xmlns" type="x:string"/>`,
	} {
		f := skcSimpleContentFiles(t, attr, `<x:attribute name="A" type="x:int"/>`)
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("invalid attribute name/form/ref accepted")
		}
	}
	for _, attr := range []string{`form="unqualified"`, `use="optional"`} {
		f := skcSimpleContentFiles(t, `<x:attribute ref="b:A"/>`, `<x:attribute name="A" type="x:int" `+attr+`/>`)
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("global attribute accepted local use/form")
		}
	}
}

func TestVFFSKCAttributesBoundMergesComparisonsAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		b := &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
		name := xml.Name{Local: "A"}
		use := vffSKCValueAttribute{name: name}
		to := &vffSKCValueAttributes{names: map[xml.Name][]int{name: {0}}, uses: []vffSKCValueAttribute{use}}
		from := &vffSKCValueAttributes{uses: []vffSKCValueAttribute{use}}
		err := b.mergeValueAttributes(to, from, &vffSKCValueBinding{})
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && (err == nil || !strings.Contains(err.Error(), "budget")) || len(to.uses) != 1 {
			t.Fatalf("attribute merge/comparison bypassed shared budget or cancellation: %v", err)
		}
	}
}
