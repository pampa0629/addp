package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func fixedFacetFiles(t *testing.T, primitive, base, middle, derived, value string) map[string][]byte {
	t.Helper()
	f := skcEnumFiles(t, derived, base, value, value)
	s := strings.Replace(string(f["Schemas/extra.xsd"]), `base="x:int"`, `base="x:`+primitive+`"`, 1)
	if middle != "" {
		s = strings.Replace(s, `base="c:Base"`, `base="c:Middle"`, 1)
		s = strings.Replace(s, `<x:simpleType name="Base">`, `<x:simpleType name="Middle"><x:restriction base="c:Base">`+middle+`</x:restriction></x:simpleType><x:simpleType name="Base">`, 1)
	}
	f["Schemas/extra.xsd"] = []byte(s)
	return f
}

func TestVFFSKCFixedFacetInheritance(t *testing.T) {
	rows := []struct{ kind, primitive, base, equal, changed, sample string }{
		{"minInclusive", "decimal", "1.00", "+001", "2", "3"},
		{"minExclusive", "decimal", "1.00", "01.0", "2", "3"},
		{"maxInclusive", "decimal", "10.00", "+0010", "9", "5"},
		{"maxExclusive", "decimal", "10.00", "10.0", "9", "5"},
		{"length", "token", "2", "+002", "3", "ab"},
		{"minLength", "string", "1", "01", "2", "ab"},
		{"maxLength", "string", "3", "+003", "2", "ab"},
		{"totalDigits", "decimal", "4", "+004", "3", "1.2"},
		{"fractionDigits", "decimal", "3", "003", "2", "1.2"},
	}
	for _, row := range rows {
		facet := func(value, flag string) string {
			return fmt.Sprintf(`<x:%s value="%s" fixed="%s"/>`, row.kind, value, flag)
		}
		for _, flag := range []string{"true", "1", " &#x9;true&#xA; "} {
			t.Run(row.kind+"/"+flag, func(t *testing.T) {
				base := facet(row.base, flag)
				f := fixedFacetFiles(t, row.primitive, base, "", facet(row.equal, "false"), row.sample)
				v := skcAttributeValue(t, f)
				if !v.fixedValues[0].known || len(v.fixedValues[0].facets) != 2 {
					t.Fatal("equal fixed facet not proved or declarations merged")
				}
				for _, fact := range v.constraintValues {
					if !fact.known || len(fact.facets) != 2 {
						t.Fatal("single constraint bypassed fixed facet")
					}
				}
				for _, w := range v.fixedValues[0].facets {
					if w.member != "Schemas/extra.xsd" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
						t.Fatal("fixed declaration lost exact source witness")
					}
				}
				for _, mid := range []string{"", `<x:annotation/>`, facet(row.equal, "false"), facet(row.equal, "true"), fmt.Sprintf(`<x:%s value="%s"/>`, row.kind, row.equal)} {
					f = fixedFacetFiles(t, row.primitive, base, mid, facet(row.changed, "false"), row.sample)
					if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
						t.Fatal("derived declaration changed inherited fixed value", mid)
					}
				}
			})
		}
		if row.kind != "length" {
			for _, flag := range []string{"false", "0", " false "} {
				t.Run(row.kind+"/unfixed/"+flag, func(t *testing.T) {
					v := skcAttributeValue(t, fixedFacetFiles(t, row.primitive, facet(row.base, flag), "", facet(row.changed, "true"), row.sample))
					if !v.fixedValues[0].known {
						t.Fatal("legal narrowing of unfixed facet not proved")
					}
				})
			}
		}
	}
}

func TestVFFSKCFixedFacetFlagsAndConstraintsWithoutDefaults(t *testing.T) {
	for _, flag := range []string{"", "TRUE", "yes", "2", "true false"} {
		f := fixedFacetFiles(t, "decimal", `<x:totalDigits value="4" fixed="`+flag+`"/>`, "", "", "2")
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("invalid fixed boolean accepted", flag)
		}
	}
	f := fixedFacetFiles(t, "decimal", `<x:totalDigits value="4" fixed="true"/>`, "", `<x:totalDigits value="3"/>`, "22")
	for _, name := range []string{"Schemas/extra.xsd", "Schemas/shared.xsd"} {
		f[name] = []byte(strings.ReplaceAll(string(f[name]), ` fixed="22"`, ""))
	}
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("fixed facet escaped validation without attribute constraints")
	}
	// A newly fixed derived facet must survive a further explicit false.
	f = fixedFacetFiles(t, "decimal", `<x:totalDigits value="4" fixed="false"/>`, `<x:totalDigits value="3" fixed="true"/>`, `<x:totalDigits value="2" fixed="false"/>`, "1")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("newly fixed derived facet was lost")
	}
	// Equal syntax must not release the builtin integer fractionDigits lock.
	f = fixedFacetFiles(t, "integer", `<x:fractionDigits value="0" fixed="false"/>`, "", `<x:fractionDigits value="1"/>`, "1")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("builtin fixed fractionDigits released")
	}
}

func TestVFFSKCFixedFacetPreservesOtherKindsAndSiblingCache(t *testing.T) {
	// A fixed minimum does not forbid a separate exclusive minimum; all bounds
	// still constrain values. The fixed original kind remains in the base chain.
	f := fixedFacetFiles(t, "decimal", `<x:minInclusive value="1" fixed="true"/>`, "", `<x:minExclusive value="2"/>`, "3")
	if !skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("independent bound kind confused with fixed-value redefinition")
	}
	f = fixedFacetFiles(t, "decimal", `<x:minInclusive value="1" fixed="true"/>`, `<x:minExclusive value="1"/>`, `<x:minInclusive value="2"/>`, "3")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("opposite bound kind erased inherited fixed facet")
	}
	f = skcSimpleContentFiles(t, `<x:attribute name="A" type="b:One" default="3"/><x:attribute name="B" type="b:Two" default="3"/>`, `<x:simpleType name="Base"><x:restriction base="x:decimal"><x:minInclusive value="1"/></x:restriction></x:simpleType><x:simpleType name="One"><x:restriction base="b:Base"><x:minInclusive value="2" fixed="true"/></x:restriction></x:simpleType><x:simpleType name="Two"><x:restriction base="b:Base"><x:minInclusive value="3"/></x:restriction></x:simpleType>`)
	v := skcAttributeValue(t, f)
	if len(v.constraintValues) != 4 {
		t.Fatal("sibling facts missing")
	}
	for _, fact := range v.constraintValues {
		if !fact.known {
			t.Fatal("derived fixed facet mutated cached base or sibling")
		}
	}
}

func TestVFFSKCFixedFacetExactDecimalAndUnboundedDeclaration(t *testing.T) {
	base := `<x:minInclusive value="9007199254740993.0000000000000001" fixed="true"/>`
	f := fixedFacetFiles(t, "decimal", base, "", `<x:minInclusive value="+09007199254740993.00000000000000010" fixed="false"/>`, "9007199254740993.0000000000000003")
	if !skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("typed equality lost exact high precision")
	}
	f = fixedFacetFiles(t, "decimal", base, "", `<x:minInclusive value="9007199254740993.0000000000000002"/>`, "9007199254740993.0000000000000003")
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("nearby fixed endpoints rounded into equality")
	}
	f = fixedFacetFiles(t, "string", `<x:maxLength value="184467440737095516160000" fixed="true"/>`, "", `<x:maxLength value="+0184467440737095516160000" fixed="false"/>`, "a")
	if !skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("fixed facet declaration narrowed to machine integer")
	}
}
