package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

func skcListFiles(t *testing.T, item, base, derived, use, declaration string) map[string][]byte {
	t.Helper()
	f := skcEnumFiles(t, derived, base, use, declaration)
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `<x:simpleType name="Base"><x:restriction base="x:int">`, `<x:simpleType name="Items"><x:list itemType="`+item+`"/></x:simpleType><x:simpleType name="Base"><x:restriction base="c:Items">`, 1))
	return f
}

func TestVFFSKCIntegerListValues(t *testing.T) {
	rows := []struct {
		name, item, base, derived, use, declaration, canonical string
		reject                                                 bool
	}{
		{"ordered exact", "x:integer", "", "", "+0001  -0 90071992547409930000000000001", "1 0 +090071992547409930000000000001", "1 0 90071992547409930000000000001", false},
		{"xml whitespace", "x:integer", "", "", " &#x9;1&#xA;02&#xD; ", "1 2", "1 2", false},
		{"empty", "x:integer", "", "", "   ", "", "", false},
		{"reordered", "x:integer", "", "", "2 1", "1 2", "", true},
		{"duplicates matter", "x:integer", "", "", "1", "1 1", "", true},
		{"decimal invalid", "x:integer", "", "", "1 2.0", "1 2", "", true},
		{"non XML whitespace", "x:integer", "", "", "1 2", "1 2", "", true},
		{"bad item", "x:integer", "", "", "1 bad 2", "1 2", "", true},
		{"builtin range", "x:byte", "", "", "-128 +0127", "-128 127", "-128 127", false},
		{"builtin overflow", "x:byte", "", "", "128", "128", "", true},
		{"length uses items", "x:integer", "", `<x:length value="2"/>`, "100000 200000", "100000 200000", "100000 200000", false},
		{"wrong item count", "x:integer", "", `<x:length value="2"/>`, "1", "1", "", true},
		{"mixed inherited lengths", "x:integer", `<x:minLength value="1"/><x:maxLength value="3"/>`, `<x:length value="2"/>`, "1 2", "1 2", "1 2", false},
		{"fixed length change", "x:integer", `<x:maxLength value="3" fixed="true"/>`, `<x:maxLength value="2"/>`, "1 2", "1 2", "", true},
		{"list enumeration", "x:integer", `<x:enumeration value="1 02"/><x:enumeration value="3"/>`, `<x:enumeration value="01 2"/>`, "1 2", "1 2", "1 2", false},
		{"item not whole enum", "x:integer", `<x:enumeration value="1 2"/>`, "", "1", "1", "", true},
		{"numeric facet invalid", "x:integer", "", `<x:minInclusive value="1"/>`, "1 2", "1 2", "", true},
		{"digits invalid", "x:integer", "", `<x:totalDigits value="3"/>`, "1 2", "1 2", "", true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := skcListFiles(t, row.item, row.base, row.derived, row.use, row.declaration)
			got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
			if row.reject {
				if err == nil || got != nil {
					t.Fatal("invalid integer list admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			v := skcAttributeValue(t, f)
			if len(v.issues) != 0 || !v.fixedValues[0].known || v.fixedValues[0].useValue.text != row.canonical {
				t.Fatal("integer list equality or canonical values not proved")
			}
			if !v.fixedValues[0].useValue.list || v.fixedValues[0].primitive != (xml.Name{}) || v.fixedValues[0].itemPrimitive.Local == "" {
				t.Fatal("integer list mislabeled as an atomic integer")
			}
			for _, fact := range v.constraintValues {
				if !fact.known || fact.value.text != row.canonical {
					t.Fatal("single list constraint not validated")
				}
			}
		})
	}
}

func TestVFFSKCIntegerListDerivedItemsAndWitnesses(t *testing.T) {
	f := skcListFiles(t, "c:Entry", "", `<x:length value="2"/>`, "02 -0", "2 0")
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `<x:simpleType name="Items">`, `<x:simpleType name="Entry"><x:restriction base="x:integer"><x:minInclusive value="0"/><x:maxInclusive value="2"/><x:enumeration value="0"/><x:enumeration value="2"/></x:restriction></x:simpleType><x:simpleType name="Items">`, 1))
	v := skcAttributeValue(t, f)
	fact := v.fixedValues[0]
	if !fact.known || len(fact.facets) != 1 || len(fact.itemFacets) != 4 || fact.useValue.items != 2 || fact.itemPrimitive.Local != "integer" {
		t.Fatal("item and list restriction facts mixed or lost")
	}
	for _, w := range append(fact.facets, fact.itemFacets...) {
		if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatal("list/item constraint lost exact witness")
		}
	}
	for _, bad := range []string{"1 2", "3 2", "-1 0"} {
		g := map[string][]byte{}
		for name, raw := range f {
			g[name] = []byte(strings.ReplaceAll(string(raw), `fixed="02 -0"`, `fixed="`+bad+`"`))
		}
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, g), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("list item bypassed its derived profile", bad)
		}
	}
	f = skcListFiles(t, "x:integer", "", "", "0 2", "00 +2")
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `<x:list itemType="x:integer"/>`, `<x:list><x:simpleType><x:restriction base="x:byte"><x:maxInclusive value="2"/></x:restriction></x:simpleType></x:list>`, 1))
	if !skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("inline integer item profile not proved")
	}
}

func TestVFFSKCIntegerListUnknownAndMalformedTypes(t *testing.T) {
	for _, item := range []string{"x:string", "x:decimal", "x:anyURI", "x:ID", "c:Absent"} {
		f := skcListFiles(t, item, "", "", "1", "1")
		if skcAttributeValue(t, f).fixedValues[0].known {
			t.Fatal("unsupported list item guessed integer", item)
		}
	}
	for _, facet := range []string{`<x:pattern value=".*"/>`, `<x:whiteSpace value="collapse"/>`, `<x:maxLength value="2" opaque="unknown"/>`} {
		f := skcListFiles(t, "x:integer", "", facet, "1", "1")
		if skcAttributeValue(t, f).fixedValues[0].known {
			t.Fatal("unknown list facet discarded")
		}
	}
	f := skcListFiles(t, "c:Inner", "", "", "1", "1")
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `<x:simpleType name="Items">`, `<x:simpleType name="Inner"><x:list itemType="x:integer"/></x:simpleType><x:simpleType name="Items">`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("list of lists admitted")
	}
	for _, replacement := range []struct{ from, to string }{
		{`<x:list itemType="x:integer"/>`, `<z:list xmlns:z="urn:foreign" itemType="x:integer"/>`},
		{`<x:length value="1"/>`, `<z:length xmlns:z="urn:foreign" value="1"/>`},
	} {
		f = skcListFiles(t, "x:integer", "", `<x:length value="1"/>`, "1", "1")
		f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), replacement.from, replacement.to, 1))
		got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
		if err == nil && got[0].rules[0].fields[0].candidates[0].value.fixedValues[0].known {
			t.Fatal("foreign namespace interpreted as an XSD list or facet")
		}
	}
}

func TestVFFSKCIntegerListTypeFactsAndIDSeparation(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute name="arraySize"><x:simpleType><x:list itemType="x:integer"/></x:simpleType></x:attribute><x:attribute name="pos"><x:simpleType><x:list itemType="x:integer"/></x:simpleType></x:attribute>`, "")
	v := skcAttributeValue(t, f)
	if len(v.valueTypes) != 2 {
		t.Fatal("list declaration facts missing without default/fixed")
	}
	for _, fact := range v.valueTypes {
		if !fact.known || fact.primitive != (xml.Name{}) || fact.itemPrimitive != (xml.Name{Space: vffXSDNS, Local: "integer"}) {
			t.Fatal("list variety or exact item type missing")
		}
	}
	f = skcSimpleContentFiles(t, `<x:attribute name="List"><x:simpleType><x:list itemType="x:ID"/></x:simpleType></x:attribute><x:attribute name="ID" type="x:ID"/>`, "")
	v = skcAttributeValue(t, f)
	uses := skcAttributeSet(t, v, "complexType", "Value").uses
	if uses[0].idType != "not_id" || uses[1].idType != "id" {
		t.Fatal("list borrowed its member's ID ancestry")
	}
}

func TestVFFSKCIntegerListBudgetCancelAndValueKinds(t *testing.T) {
	item := &vffSKCRestrictionProfile{primitive: xml.Name{Space: vffXSDNS, Local: "integer"}, known: true}
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		c := &vffSKCRestrictionChecker{b: &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}}
		raw := "1 2"
		if cancel {
			raw = ""
		}
		_, _, err := c.listValue(item, nil, raw)
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && err == nil {
			t.Fatal("list bypassed cumulative budget or cancellation")
		}
	}
	b := &vffSKCBinder{ctx: context.Background(), budget: &vffSKCBindingBudget{remaining: 100, depth: 8}}
	if equal, err := b.equalValue(vffSKCValue{text: "", list: true}, vffSKCValue{text: ""}); err != nil || equal {
		t.Fatal("empty list confused with empty atomic string")
	}
}

func TestVFFSKCIntegerListDefaultsAndSiblingProfiles(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute name="A" type="b:One" default="01"/><x:attribute name="B" type="b:Two" default="1 -0"/>`, `<x:simpleType name="Items"><x:list itemType="x:integer"/></x:simpleType><x:simpleType name="One"><x:restriction base="b:Items"><x:length value="1"/></x:restriction></x:simpleType><x:simpleType name="Two"><x:restriction base="b:Items"><x:length value="2"/></x:restriction></x:simpleType>`)
	v := skcAttributeValue(t, f)
	if len(v.constraintValues) != 4 {
		t.Fatal("single list default facts missing")
	}
	for i, fact := range v.constraintValues {
		if !fact.known || !fact.value.list || fact.value.items != i%2+1 || fact.value.text != []string{"1", "1 0"}[i%2] {
			t.Fatal("single list default or sibling cache invalid")
		}
	}
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `default="1 -0"`, `default="1 2.0"`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("single list default skipped item validation")
	}
	f = skcSimpleContentFiles(t, `<x:attribute name="A"><x:simpleType><x:restriction><x:simpleType><x:list itemType="x:integer"/></x:simpleType><x:totalDigits value="1"/></x:restriction></x:simpleType></x:attribute>`, "")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("invalid list facet escaped without a value constraint")
	}
}
