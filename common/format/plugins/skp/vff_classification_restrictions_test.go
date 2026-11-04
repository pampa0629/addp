package skp

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func skcEnumFiles(t *testing.T, derived, base, left, right string) map[string][]byte {
	f := skcSimpleContentFiles(t, `<x:attribute ref="c:A" fixed="`+left+`"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attribute name="A" type="c:Derived" fixed="`+right+`"/><x:simpleType name="Derived"><x:restriction base="c:Base">`+derived+`</x:restriction></x:simpleType><x:simpleType name="Base"><x:restriction base="x:int">`+base+`</x:restriction></x:simpleType>`)
	return f
}

func TestVFFSKCRestrictionEnumerationsFollowExactBaseAndPreserveFacets(t *testing.T) {
	f := skcEnumFiles(t, `<x:enumeration value="+001"/>`, `<x:enumeration value="1"/><x:enumeration value="2"/>`, `1`, `01`)
	v := skcAttributeValue(t, f)
	if len(v.issues) != 0 || len(v.fixedValues) != 1 || !v.fixedValues[0].known || v.fixedValues[0].typeName.Space != "urn:c" || v.fixedValues[0].primitive.Local != "int" || len(v.fixedValues[0].facets) != 3 || len(v.constraintValues) != 2 {
		t.Fatal("derived enum profile/type/value facts lost")
	}
	for _, w := range v.fixedValues[0].facets {
		if w.member != "Schemas/extra.xsd" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatal("enum source witness differs from exact schema")
		}
	}
	for _, fact := range v.constraintValues {
		if !fact.known || fact.value.text != "1" || len(fact.facets) != 3 {
			t.Fatal("single/fallback constraint ignored derived facets")
		}
	}
	for _, args := range [][3]string{{`<x:enumeration value="3"/>`, `1`, `1`}, {`<x:enumeration value="1"/>`, `2`, `2`}, {`<x:enumeration/>`, `1`, `1`}} {
		f = skcEnumFiles(t, args[0], `<x:enumeration value="1"/><x:enumeration value="2"/>`, args[1], args[2])
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("invalid subtype enumeration or value accepted")
		}
	}
}

func TestVFFSKCRestrictionChecksSingleDefaultsInlineAliasesAndEmptyValues(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute name="A" default=" x "><x:simpleType><x:restriction base="x:token"><x:enumeration value="x"/></x:restriction></x:simpleType></x:attribute><x:attribute name="B" type="b:Alias" default="1.000"/><x:attribute name="C" default=""><x:simpleType><x:restriction base="x:string"><x:enumeration value=""/></x:restriction></x:simpleType></x:attribute>`, `<x:simpleType name="Alias"><x:restriction base="x:decimal"/></x:simpleType>`)
	v := skcAttributeValue(t, f)
	if len(v.constraintValues) != 6 {
		t.Fatalf("declaration/use facts missing: %d", len(v.constraintValues))
	}
	for i, want := range []string{"x", "1", "", "x", "1", ""} {
		if !v.constraintValues[i].known || v.constraintValues[i].value.text != want {
			t.Fatal("inline alias/default or empty presence not validated")
		}
	}
	original := string(f["Schemas/shared.xsd"])
	for _, replacement := range []string{`default="outside"`, `fixed="outside"`} {
		f["Schemas/shared.xsd"] = []byte(strings.Replace(original, `default=" x "`, replacement, 1))
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("single default/fixed outside enumeration accepted")
		}
	}
	f = skcSimpleContentFiles(t, `<x:attribute name="A" type="x:int" default="not-an-int"/>`, "")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("single builtin default lexical error accepted")
	}
}

func TestVFFSKCRestrictionQNameEnumerationsUseEachValuesNamespace(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute xmlns:q="urn:allowed" ref="c:A" fixed="q:One"/>`, `<x:import namespace="urn:c" schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:attribute xmlns:r="urn:allowed" name="A" type="c:Enum" fixed="r:One"/><x:simpleType name="Enum"><x:restriction base="x:QName"><x:enumeration xmlns:p="urn:allowed" value="p:One"/></x:restriction></x:simpleType>`)
	v := skcAttributeValue(t, f)
	if len(v.issues) != 0 || !v.fixedValues[0].known || v.fixedValues[0].useValue.qname.Space != "urn:allowed" {
		t.Fatal("QName enum matched prefix text instead of expanded value")
	}
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `xmlns:p="urn:allowed"`, `xmlns:p="urn:other"`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("enum with another namespace admitted")
	}
}

func TestVFFSKCRestrictionNeverDropsUnimplementedFacetsOrFinalFlags(t *testing.T) {
	for _, body := range []string{`<x:pattern value="never"/>`, `<x:whiteSpace value="collapse"/>`, `<x:totalDigits value="1" fixed="false" opaque="unproved"/>`, `<x:enumeration value="1" fixed="false"/>`, `<x:enumeration value="1">opaque</x:enumeration>`, `<x:enumeration value="1"><x:annotation/><x:annotation/></x:enumeration>`} {
		v := skcAttributeValue(t, skcEnumFiles(t, body, `<x:enumeration value="1"/>`, `1`, `1`))
		if v.fixedValues[0].known || !strings.Contains(strings.Join(v.issues, " "), "typed value equality not proved") {
			t.Fatal("unsupported facet silently dropped")
		}
		for _, fact := range v.constraintValues {
			if fact.known {
				t.Fatal("single constraint bypassed unsupported facet")
			}
		}
	}
	for _, oldnew := range [][2]string{{`<x:simpleType name="Derived">`, `<x:simpleType name="Derived" final="restriction">`}, {`<x:restriction base="c:Base">`, `<x:restriction base="c:Base" unknown="opaque">`}, {`targetNamespace="urn:c"`, `targetNamespace="urn:c" finalDefault="restriction"`}} {
		f := skcEnumFiles(t, `<x:enumeration value="1"/>`, "", `1`, `1`)
		f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), oldnew[0], oldnew[1], 1))
		v := skcAttributeValue(t, f)
		if v.fixedValues[0].known {
			t.Fatal("unknown derivation flags ignored")
		}
	}
}

func TestVFFSKCRestrictionDuplicateEquivalentEnumsKeepAllSources(t *testing.T) {
	f := skcEnumFiles(t, `<x:enumeration value="1"/><x:enumeration value="01"/>`, "", `+1`, `1`)
	v := skcAttributeValue(t, f)
	if !v.fixedValues[0].known || len(v.fixedValues[0].facets) != 2 || v.fixedValues[0].facets[0] == v.fixedValues[0].facets[1] {
		t.Fatal("equivalent enum declarations merged by value")
	}
}

func TestVFFSKCRestrictionRejectsInapplicableBooleanEnumeration(t *testing.T) {
	f := skcEnumFiles(t, `<x:enumeration value="true"/>`, "", `1`, `true`)
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `base="x:int"`, `base="x:boolean"`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatal("boolean enumeration treated as a legal scalar facet")
	}
}

func TestVFFSKCRestrictionComparisonsAndWitnessCopiesUseSharedBudget(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		b := &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
		_, err := b.equalValue(vffSKCValue{text: "aa"}, vffSKCValue{text: "aa"})
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && (err == nil || !strings.Contains(err.Error(), "budget")) {
			t.Fatal("enum comparison budget/cancellation bypass")
		}
	}
	b := &vffSKCBinder{ctx: context.Background(), budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
	if got, err := b.copyFacetWitnesses(make([]vffSKCDeclarationWitness, 2)); err == nil || got != nil {
		t.Fatal("facet witness copies not charged")
	}
}
