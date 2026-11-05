package skp

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestVFFSKCEnumTextListUsesDeclaredItemType(t *testing.T) {
	for _, tc := range []struct {
		name, use, decl, facets, want string
		reject                        bool
	}{
		{"ordered with repeats", " list&#x9;set list ", "list set list", `<x:length value="3"/>`, "list set list", false},
		{"empty", " ", "", `<x:maxLength value="0"/>`, "", false},
		{"list enumeration", "set list", "set list", `<x:enumeration value="set&#xA;list"/>`, "set list", false},
		{"item not declared", "list array", "list array", "", "", true},
		{"wrong case", "List", "List", "", "", true},
		{"not XML whitespace", "list set", "list set", "", "", true},
		{"reordered", "list set", "set list", "", "", true},
		{"different repetition", "list list", "list set", "", "", true},
		{"item count", "list set", "list set", `<x:length value="1"/>`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := skcListFiles(t, "c:Entry", "", tc.facets, tc.use, tc.decl)
			f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `<x:simpleType name="Items">`, `<x:simpleType name="Entry"><x:restriction base="x:normalizedString"><x:enumeration value="list"/><x:enumeration value="set"/></x:restriction></x:simpleType><x:simpleType name="Items">`, 1))
			got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
			if tc.reject {
				if err == nil || got != nil {
					t.Fatal("invalid enumerated list admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fact := got[0].rules[0].fields[0].candidates[0].value.fixedValues[0]
			if !fact.known || !fact.useValue.list || fact.useValue.text != tc.want || fact.itemPrimitive.Local != "normalizedString" || len(fact.itemFacets) != 2 {
				t.Fatal("enumerated list value or item restrictions not proved")
			}
			for _, w := range fact.itemFacets {
				if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
					t.Fatal("item enumeration witness lost")
				}
			}
		})
	}
	f := skcListFiles(t, "x:normalizedString", "", "", "list", "list")
	if skcAttributeValue(t, f).fixedValues[0].known {
		t.Fatal("unrestricted text list claimed as proved")
	}
}
