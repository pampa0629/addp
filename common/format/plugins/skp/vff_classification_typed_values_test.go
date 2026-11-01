package skp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcTypedFixedFiles(t *testing.T, typ, left, right string) map[string][]byte {
	return skcSimpleContentFiles(t, `<x:attribute ref="b:A" fixed="`+left+`"/>`, `<x:attribute name="A" type="x:`+typ+`" fixed="`+right+`"/>`)
}

func TestVFFSKCFixedScalarsKeepExactPrecisionAndDatatypeWhitespace(t *testing.T) {
	for _, tc := range []struct{ typ, left, right, value string }{
		{"integer", "+0009007199254740993123456789", "9007199254740993123456789", "9007199254740993123456789"},
		{"decimal", "-.0000", "+0.00", "0"}, {"decimal", "+0001.23000", "1.23", "1.23"},
		{"decimal", ".0100", "0.01", "0.01"}, {"decimal", "1.", "+1", "1"},
		{"boolean", " 0 ", "false", "false"}, {"token", " a&#xA; b ", "a b", "a b"},
		{"normalizedString", "a&#xA;b", "a b", "a b"}, {"string", "", "", ""},
	} {
		v := skcAttributeValue(t, skcTypedFixedFiles(t, tc.typ, tc.left, tc.right))
		if len(v.issues) != 0 || len(v.fixedValues) != 1 || !v.fixedValues[0].known || v.fixedValues[0].typeName.Space != vffXSDNS || v.fixedValues[0].typeName.Local != tc.typ || v.fixedValues[0].useValue.text != tc.value || v.fixedValues[0].baseValue.text != tc.value || v.fixedValues[0].source == v.fixedValues[0].declaration {
			t.Fatalf("typed scalar/source mismatch: %#v", tc)
		}
	}
	for _, tc := range []struct{ typ, left, right string }{
		{"integer", "9007199254740993", "9007199254740992"}, {"decimal", "0.00000000000000000001", "0.00000000000000000002"},
		{"boolean", "false", "true"}, {"string", " a ", "a"}, {"token", "a b", "a b"},
	} {
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, skcTypedFixedFiles(t, tc.typ, tc.left, tc.right)), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "disagrees") {
			t.Fatalf("different exact typed values accepted: %#v %v", tc, err)
		}
	}
}

func TestVFFSKCFixedScalarsRejectLexicalAndBuiltinRangeErrors(t *testing.T) {
	for _, tc := range []struct{ typ, valid, invalid string }{
		{"integer", "0", "1.0"}, {"decimal", "0", "1e2"}, {"decimal", "0", "."}, {"decimal", "0", "1/2"}, {"boolean", "1", "True"},
		{"nonPositiveInteger", "-0", "1"}, {"negativeInteger", "-1", "-0"}, {"nonNegativeInteger", "+0", "-1"}, {"positiveInteger", "1", "0"},
		{"long", "-9223372036854775808", "-9223372036854775809"}, {"long", "9223372036854775807", "9223372036854775808"},
		{"int", "2147483647", "2147483648"}, {"short", "-32768", "-32769"}, {"byte", "127", "128"},
		{"unsignedLong", "18446744073709551615", "18446744073709551616"}, {"unsignedInt", "4294967295", "4294967296"},
		{"unsignedShort", "65535", "65536"}, {"unsignedByte", "255", "256"}, {"unsignedByte", "-0", "-1"},
	} {
		v := skcAttributeValue(t, skcTypedFixedFiles(t, tc.typ, tc.valid, tc.valid))
		if len(v.fixedValues) != 1 || !v.fixedValues[0].known {
			t.Fatalf("valid builtin edge unproved: %#v", tc)
		}
		for _, sides := range [][2]string{{tc.invalid, tc.valid}, {tc.valid, tc.invalid}, {tc.invalid, tc.invalid}} {
			if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, skcTypedFixedFiles(t, tc.typ, sides[0], sides[1])), skcBindingLimits()); err == nil || got != nil {
				t.Fatalf("invalid lexical/range accepted: %#v", tc)
			}
		}
	}
}

func TestVFFSKCFixedUnknownTypesAndFacetsRemainUnproved(t *testing.T) {
	for _, typ := range []string{"float", "double", "dateTime", "anyURI"} {
		v := skcAttributeValue(t, skcTypedFixedFiles(t, typ, "same", "same"))
		if len(v.fixedValues) != 1 || v.fixedValues[0].known || !strings.Contains(strings.Join(v.issues, " "), "typed value equality not proved") {
			t.Fatal("unsupported builtin guessed by matching strings")
		}
	}
	f := skcTypedFixedFiles(t, "int", "1", "1")
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `type="x:int"`, `type="b:Restricted"`, 1))
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `<x:complexType name="Value">`, `<x:simpleType name="Restricted"><x:restriction base="x:int"><x:pattern value="0"/></x:restriction></x:simpleType><x:complexType name="Value">`, 1))
	v := skcAttributeValue(t, f)
	if len(v.fixedValues) != 1 || v.fixedValues[0].known || v.fixedValues[0].typeName.Space != "urn:b" || v.fixedValues[0].typeName.Local != "Restricted" || !strings.Contains(strings.Join(v.issues, " "), "typed value equality not proved") {
		t.Fatal("named restriction/facet silently treated as builtin")
	}
}

func TestVFFSKCIDAncestryRejectsDeclarationConstraintsAndMultipleIDs(t *testing.T) {
	for _, tc := range []struct{ body, extra string }{
		{`<x:attribute name="A" type="x:ID" default="id1"/>`, ""},
		{`<x:attribute ref="b:A"/>`, `<x:attribute name="A" type="x:ID" fixed="id1"/>`},
		{`<x:attribute name="A" type="c:Derived" fixed="id1"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`},
		{`<x:attribute name="A" type="x:ID"/><x:attribute name="B" type="c:Derived"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`},
		{`<x:simpleContent><x:extension base="b:Base"><x:attribute name="B" type="x:ID"/></x:extension></x:simpleContent>`, `<x:complexType name="Base"><x:simpleContent><x:extension base="x:string"><x:attribute name="A" type="x:ID"/></x:extension></x:simpleContent></x:complexType>`},
		{`<x:simpleContent><x:extension base="x:string"><x:attributeGroup ref="b:IDs"/></x:extension></x:simpleContent>`, `<x:attributeGroup name="IDs"><x:attribute name="A" type="x:ID"/><x:attribute name="B" type="x:ID"/></x:attributeGroup>`},
	} {
		f := skcSimpleContentFiles(t, tc.body, tc.extra)
		f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:simpleType name="Derived"><x:restriction base="c:Parent"/></x:simpleType><x:simpleType name="Parent"><x:restriction base="x:ID"><x:minLength value="1"/></x:restriction></x:simpleType>`)
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "ID-derived") {
			t.Fatalf("ID constraint/ancestry not rejected: %v", err)
		}
	}
}

func TestVFFSKCIDFactsKeepScopesReferencesAndUnknownTypesSeparate(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:sequence><x:element name="Child"><x:complexType><x:attribute name="ChildID" type="x:ID"/></x:complexType></x:element></x:sequence><x:attribute name="A" type="x:ID"/><x:attribute name="B" type="x:IDREF"/><x:attribute name="C" type="x:IDREFS"/><x:attribute name="Ignored" type="x:ID" use="prohibited" fixed="id1"/>`, "")
	v := skcAttributeValue(t, f)
	uses := skcAttributeSet(t, v, "complexType", "Value").uses
	if len(v.issues) != 0 || len(uses) != 3 || uses[0].idType != "id" || uses[1].idType != "not_id" || uses[2].idType != "not_id" {
		t.Fatal("nested/prohibited ID or IDREF treated as a second ID")
	}
	f = skcSimpleContentFiles(t, `<x:attribute ref="b:A" default="id1"/>`, `<x:attribute name="A" type="x:ID"/>`)
	v = skcAttributeValue(t, f)
	if !strings.Contains(strings.Join(v.issues, " "), "ID attribute use constraint value not proved") || skcAttributeSet(t, v, "complexType", "Value").uses[0].idType != "id" {
		t.Fatal("use-only ID constraint guessed invalid or fully valid")
	}
	f = skcSimpleContentFiles(t, `<x:attribute name="A" type="c:Absent" default="id1"/>`, "")
	v = skcAttributeValue(t, f)
	if !strings.Contains(strings.Join(v.issues, " "), "ID ancestry not proved") || skcAttributeSet(t, v, "complexType", "Value").uses[0].idType != "unproved" {
		t.Fatal("missing foreign type guessed not derived from ID")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("scalar and ID facts opened composition admission")
	}
}

func TestVFFSKCTypedValuesChargeScalarBytesAndHonorCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		b := &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
		_, _, err := b.scalar(nil, vffSKCValueReference{}.name, "12")
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && (err == nil || !strings.Contains(err.Error(), "budget")) {
			t.Fatalf("scalar byte budget/cancellation ignored: %v", err)
		}
	}
}
