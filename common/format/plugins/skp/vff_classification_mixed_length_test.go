package skp

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestVFFSKCMixedLengthInheritance(t *testing.T) {
	rows := []struct {
		name, base, middle, derived, value string
		reject                             bool
	}{
		{"inherited min", `<x:minLength value="1"/>`, "", `<x:length value="2"/>`, "中😀", false},
		{"inherited max", `<x:maxLength value="3"/>`, "", `<x:length value="2"/>`, "中😀", false},
		{"inherited interval", `<x:minLength value="1"/><x:maxLength value="3"/>`, "", `<x:length value="2"/>`, "中😀", false},
		{"repeat exact min", `<x:minLength value="1"/>`, "", `<x:length value="2"/><x:minLength value="+001"/>`, "中😀", false},
		{"repeat exact max", `<x:maxLength value="3"/>`, "", `<x:maxLength value="003"/><x:length value="2"/>`, "中😀", false},
		{"repeat exact interval", `<x:minLength value="1"/><x:maxLength value="3"/>`, "", `<x:length value="2"/><x:minLength value="1"/><x:maxLength value="3"/>`, "中😀", false},
		{"same layer min without ancestor", "", "", `<x:length value="2"/><x:minLength value="1"/>`, "中😀", true},
		{"same layer max without ancestor", "", "", `<x:maxLength value="3"/><x:length value="2"/>`, "中😀", true},
		{"changed min with new length", `<x:minLength value="1"/>`, "", `<x:length value="2"/><x:minLength value="2"/>`, "中😀", true},
		{"changed max with new length", `<x:maxLength value="3"/>`, "", `<x:maxLength value="2"/><x:length value="2"/>`, "中😀", true},
		{"length below inherited min", `<x:minLength value="3"/>`, "", `<x:length value="2"/>`, "中😀", true},
		{"length above inherited max", `<x:maxLength value="1"/>`, "", `<x:length value="2"/>`, "中😀", true},
		{"repeat min after length", `<x:minLength value="1"/>`, `<x:length value="2"/>`, `<x:minLength value="01" fixed="true"/>`, "中😀", false},
		{"repeat max after length", `<x:maxLength value="3"/>`, `<x:length value="2"/>`, `<x:maxLength value="03"/>`, "中😀", false},
		{"repeat length after interval", `<x:minLength value="1"/><x:maxLength value="3"/>`, `<x:length value="2"/>`, `<x:length value="02"/>`, "中😀", false},
		{"new min after length", `<x:length value="2"/>`, "", `<x:minLength value="1"/>`, "中😀", true},
		{"new max after length", `<x:length value="2"/>`, "", `<x:maxLength value="3"/>`, "中😀", true},
		{"changed min after length", `<x:minLength value="1"/>`, `<x:length value="2"/>`, `<x:minLength value="2"/>`, "中😀", true},
		{"changed max after length", `<x:maxLength value="3"/>`, `<x:length value="2"/>`, `<x:maxLength value="2"/>`, "中😀", true},
		{"changed min alongside intermediate length", `<x:minLength value="0"/>`, `<x:minLength value="1"/><x:length value="2"/>`, `<x:minLength value="0"/>`, "中😀", true},
		{"narrow min before length", `<x:minLength value="0"/>`, `<x:minLength value="1"/>`, `<x:length value="2"/>`, "中😀", false},
		{"missing one ancestor", `<x:minLength value="1"/>`, "", `<x:length value="2"/><x:maxLength value="3"/>`, "中😀", true},
		{"zero interval", `<x:minLength value="0"/><x:maxLength value="0"/>`, "", `<x:length value="0"/>`, "", false},
		{"huge max", `<x:maxLength value="184467440737095516160000" fixed="true"/>`, "", `<x:length value="2"/>`, "中😀", false},
		{"wrong actual length", `<x:minLength value="1"/><x:maxLength value="3"/>`, "", `<x:length value="2"/>`, "éx", true},
		{"fixed min cannot change", `<x:minLength value="1" fixed="true"/>`, `<x:length value="2"/>`, `<x:minLength value="2" fixed="false"/>`, "中😀", true},
	}
	for _, primitive := range []string{"string", "normalizedString", "token"} {
		for _, row := range rows {
			t.Run(primitive+"/"+row.name, func(t *testing.T) {
				f := fixedFacetFiles(t, primitive, row.base, row.middle, row.derived, row.value)
				got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
				if row.reject {
					if err == nil || got != nil {
						t.Fatal("invalid mixed length declaration/value admitted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				v := skcAttributeValue(t, f)
				check := func(known bool, facets []vffSKCDeclarationWitness) {
					if !known || len(facets) < 2 {
						t.Fatal("supported mixed length chain not proved")
					}
					for _, w := range facets {
						if w.member != "Schemas/extra.xsd" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
							t.Fatal("mixed length facet lost exact witness")
						}
					}
				}
				for _, fact := range v.constraintValues {
					check(fact.known, fact.facets)
				}
				for _, fact := range v.fixedValues {
					check(fact.known, fact.facets)
				}
			})
		}
	}
}

func TestVFFSKCMixedLengthWithoutDefaultsAndUnknownFacets(t *testing.T) {
	f := fixedFacetFiles(t, "string", `<x:length value="2"/>`, "", `<x:minLength value="1"/>`, "ab")
	for _, name := range []string{"Schemas/extra.xsd", "Schemas/shared.xsd"} {
		f[name] = []byte(strings.ReplaceAll(string(f[name]), ` fixed="ab"`, ""))
	}
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("mixed length declaration escaped validation without values")
	}
	f = fixedFacetFiles(t, "string", `<x:minLength value="1"/>`, "", `<x:length value="2"/><x:pattern value=".*"/>`, "ab")
	if skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("mixed length dropped unknown facet")
	}
}

func TestVFFSKCMixedLengthBudgetCancelAndSiblingCache(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		c := &vffSKCRestrictionChecker{b: &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}}
		err := c.checkMixedLength(&vffSKCRestrictionProfile{bounds: map[string]string{"minLength": "1"}}, map[string]string{"length": "2"})
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && err == nil {
			t.Fatal("mixed length comparison bypassed budget or cancellation")
		}
	}
	f := skcSimpleContentFiles(t, `<x:attribute name="A" type="b:One" default="ab"/><x:attribute name="B" type="b:Two" default="abc"/>`, `<x:simpleType name="Base"><x:restriction base="x:string"><x:minLength value="1"/></x:restriction></x:simpleType><x:simpleType name="One"><x:restriction base="b:Base"><x:length value="2"/></x:restriction></x:simpleType><x:simpleType name="Two"><x:restriction base="b:Base"><x:minLength value="2"/></x:restriction></x:simpleType>`)
	v := skcAttributeValue(t, f)
	if len(v.constraintValues) != 4 {
		t.Fatal("mixed length sibling facts missing")
	}
	for _, fact := range v.constraintValues {
		if !fact.known {
			t.Fatal("derived length mutated cached base or sibling")
		}
	}
}
