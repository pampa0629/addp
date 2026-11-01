package skp

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestVFFSKCAttributeConstraintsPreservePresenceUseAndExactSource(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute ref="c:A" use=" required "/><x:attribute ref="c:B"/><x:attribute ref="c:C" fixed="local"/><x:attribute name="D" type="x:string" default="" use=" optional "/><x:attribute name="E" type="x:string"/><x:attribute name="F" type="x:string" use=" prohibited " fixed="ignored"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attribute name="A" type="x:string" default="global"/><x:attribute name="B" type="x:string" fixed="global"/><x:attribute name="C" type="x:string" default="overridden"/>`)
	v := skcAttributeValue(t, f)
	uses := skcAttributeSet(t, v, "complexType", "Value").uses
	if len(v.issues) != 0 || len(uses) != 5 {
		t.Fatal("constraint declarations or prohibited mode lost")
	}
	for i, want := range []struct{ mode, kind, lexical, member string }{
		{"required", "default", "global", "Schemas/extra.xsd"},
		{"optional", "fixed", "global", "Schemas/extra.xsd"},
		{"optional", "fixed", "local", "Schemas/shared.xsd"},
		{"optional", "default", "", "Schemas/shared.xsd"},
		{"optional", "", "", ""},
	} {
		g := uses[i]
		if g.use != want.mode || g.constraint.kind != want.kind || g.constraint.lexical != want.lexical || g.constraint.source.member != want.member {
			t.Fatalf("use/constraint origin %d: %#v", i, g)
		}
		if want.kind != "" {
			w := g.constraint.source
			if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) || w != g.source && w != g.declaration {
				t.Fatal("constraint has no current exact source")
			}
		}
	}
	if uses[3].constraint == uses[4].constraint {
		t.Fatal("explicit empty default confused with absent constraint")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("declaration constraints opened composition admission")
	}
}

func TestVFFSKCAttributeConstraintsKeepInheritedUseAndConstraintTogether(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:simpleContent><x:extension base="b:Base"><x:attribute name="A" use=" prohibited " type="x:string"/><x:attribute name="B" type="x:string" default="child"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:string"><x:attribute name="A" type="x:string" use=" required " fixed="base"/></x:extension></x:simpleContent></x:complexType>`)
	v := skcAttributeValue(t, f)
	base := skcAttributeSet(t, v, "complexType", "Base").uses
	derived := skcAttributeSet(t, v, "complexType", "Value").uses
	if len(v.issues) != 0 || len(base) != 1 || len(derived) != 2 || derived[0] != base[0] || derived[0].use != "required" || derived[0].constraint.kind != "fixed" || derived[0].constraint.lexical != "base" || derived[1].constraint.lexical != "child" {
		t.Fatal("inherited use or constraint overridden by prohibited/local declaration")
	}
}

func TestVFFSKCAttributeConstraintsRejectInvalidDeclarationCombinations(t *testing.T) {
	for _, attrs := range []string{
		`use=""`, `use="re quired"`, `use="Required"`, `use=" optional "`,
		`default="" fixed=""`, `default="x" use="required"`, `default="x" use="prohibited"`,
		`default="x" use=" required "`, `default="x" use=" prohibited "`,
	} {
		f := skcSimpleContentFiles(t, `<x:attribute name="A" type="x:string" `+attrs+`/>`, "")
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatalf("invalid use/default/fixed combination accepted: %s", attrs)
		}
	}
	f := skcSimpleContentFiles(t, `<x:attribute ref="c:A" default="same"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attribute name="A" type="x:string" fixed="same"/>`)
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "replace a fixed") {
		t.Fatal("global fixed silently changed into use-site default")
	}
}

func TestVFFSKCAttributeConstraintsNeverGuessTypedFixedEquality(t *testing.T) {
	for _, tc := range []struct{ typ, source, target string }{
		{"x:int", "01", "1"}, {"x:boolean", "true", "1"}, {"x:string", "same", "same"},
		{"x:QName", "p:Member", "p:Member"}, {"x:QName", "q:Member", "p:Member"},
	} {
		f := skcSimpleContentFiles(t, `<x:attribute xmlns:p="urn:source" xmlns:q="urn:target" ref="c:A" fixed="`+tc.source+`"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
		f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attribute xmlns:p="urn:target" name="A" type="`+tc.typ+`" fixed="`+tc.target+`"/>`)
		v := skcAttributeValue(t, f)
		uses := skcAttributeSet(t, v, "complexType", "Value").uses
		if !strings.Contains(strings.Join(v.issues, " "), "typed value equality not proved") || len(uses) != 1 || uses[0].constraint.kind != "fixed" || uses[0].constraint.lexical != tc.source || uses[0].constraint.source != uses[0].source {
			t.Fatalf("fixed equality guessed or use-site source discarded: %#v", tc)
		}
	}
}

func TestVFFSKCAttributeConstraintsApplyToFilteredAttributeUse(t *testing.T) {
	f := skcGraphFiles(t, "", "")
	f["Schemas/root.xsd"] = skcGraphSchema("urn:r", `<x:attribute name="A" type="x:string" fixed="same"/><x:complexType name="Holder"><x:attribute ref="r:A" default="same"/></x:complexType>`)
	f["Schemas/root.xsd.filter"] = []byte("Holder { A }")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "replace a fixed") {
		t.Fatalf("filtered attribute lost the ref use-site constraint: %v", err)
	}
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `ref="r:A" default="same"`, `ref="r:A" fixed="same" use="required"`, 1))
	candidate := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0]
	v := candidate.value
	if candidate.source == candidate.declaration || len(v.references) != 2 || v.references[0].role != "attributeRef" || !strings.Contains(strings.Join(v.issues, " "), "typed value equality not proved") || v.attributeUse == nil || v.attributeUse.source != candidate.source || v.attributeUse.declaration != candidate.declaration || v.attributeUse.use != "required" || v.attributeUse.constraint.source != candidate.source {
		t.Fatal("filtered attribute entry did not retain its exact use-site identity")
	}
	f["Schemas/root.xsd"] = skcGraphSchema("urn:r", `<x:complexType name="Holder"><x:attribute name="A" type="x:string" use=" prohibited "/></x:complexType>`)
	field := skcGraphRead(t, f)[0].rules[0].fields[0]
	if len(field.candidates) != 0 {
		t.Fatal("filter expansion bypassed the common use declaration rule")
	}
}

func TestVFFSKCAttributeConstraintsCacheByUseNotSharedDeclaration(t *testing.T) {
	f := skcGraphFiles(t, "", "")
	f["Schemas/root.xsd"] = skcGraphSchema("urn:r", `<x:attribute name="A" type="x:string" default="global"/><x:complexType name="Left"><x:attribute ref="r:A" default="first"/></x:complexType><x:complexType name="Right"><x:attribute ref="r:A" fixed="second" use="required"/></x:complexType>`)
	f["Schemas/root.xsd.filter"] = []byte("Left { A }\nRight { A }")
	rules := skcGraphRead(t, f)[0].rules
	a, b := rules[0].fields[0].candidates[0], rules[1].fields[0].candidates[0]
	if a.declaration != b.declaration || a.source == b.source || a.value == b.value || a.value.attributeUse == nil || b.value.attributeUse == nil {
		t.Fatal("different uses of one global declaration share an attribute value fact")
	}
	for i, c := range []vffSKCFieldCandidate{a, b} {
		use := c.value.attributeUse
		if len(c.value.issues) != 0 || use.source != c.source || use.constraint.source != c.source || use.constraint.lexical != []string{"first", "second"}[i] || use.use != []string{"optional", "required"}[i] {
			t.Fatal("use-site constraint or mode contaminated by another use")
		}
	}
}
