package skp

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcBindingLimits() vffSKCBindingLimits {
	return vffSKCBindingLimits{documents: skcDocumentLimits(), xml: skcDocumentLimits().xml, visits: 10000, depth: 32}
}

func skcBindingFiles(t *testing.T) map[string][]byte {
	f := skcDocumentFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:t="urn:type" targetNamespace="urn:type"><x:complexType name="Base"><x:sequence><x:element name="Name"/></x:sequence><x:attribute name="Id"/></x:complexType><x:attributeGroup name="Audit"><x:attribute name="Created"/></x:attributeGroup><x:group name="Shape"><x:sequence><x:element name="Height"/></x:sequence></x:group><x:attribute name="Global"/><x:complexType name="Beam"><x:complexContent><x:extension base="t:Base"><x:attributeGroup ref="t:Audit"/><x:sequence><x:group ref="t:Shape"/><x:element name="Wrapper"><x:complexType><x:attribute name="Hidden"/></x:complexType></x:element></x:sequence><x:attribute ref="t:Global"/></x:extension></x:complexContent></x:complexType><x:element name="Beam" type="t:Beam"/></x:schema>`)
	f["Schemas/root.xsd.filter"] = []byte("Beam { Name Id Created Height Wrapper Global }\n{ Audit Global }")
	return f
}

func TestVFFSKCBindingsExactTypesFieldsAndInheritance(t *testing.T) {
	files := skcBindingFiles(t)
	got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, files), skcBindingLimits())
	if err != nil || len(got) != 1 || len(got[0].rules) != 2 {
		t.Fatalf("bindings: %v", err)
	}
	if got[0].schemaSHA256 != sha256.Sum256(files["Schemas/root.xsd"]) || got[0].filterSHA256 != sha256.Sum256(files["Schemas/root.xsd.filter"]) || got[0].packageMember != "classifications/literal%20schema.skc" {
		t.Fatal("exact current source identity lost")
	}
	rule := got[0].rules[0]
	if len(rule.issues) != 0 || rule.typeDeclaration.kind != "complexType" || rule.typeDeclaration.name != "Beam" || rule.typeDeclaration.schemaNamespace != "urn:type" || len(rule.fields) != 6 {
		t.Fatalf("type/fields: %#v", rule)
	}
	wantKinds := []string{"element", "attribute", "attribute", "element", "element", "attribute"}
	for i, field := range rule.fields {
		if len(field.candidates) != 1 || field.candidates[0].declaration.kind != wantKinds[i] {
			t.Fatal("field kinds or uniqueness lost")
		}
		c := field.candidates[0]
		w := c.declaration
		if w.name != field.name || w.member != "Schemas/root.xsd" || w.sha256 != sha256.Sum256(files[w.member][w.begin:w.end]) || len(c.via) < 2 || c.via[0].name != "Beam" {
			t.Fatal("exact declaration or inheritance path lost")
		}
	}
	if len(got[0].rules[1].issues) != 0 || got[0].rules[1].fields[0].candidates[0].declaration.kind != "attributeGroup" {
		t.Fatal("global blacklist scope confused with white type fields")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("binding witnesses opened composition admission")
	}
}

func TestVFFSKCBindingsNeverGuessNamesOrUnsupportedInheritance(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"same-name element cannot replace type": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `"><x:element name="Beam"/></x:schema>`)
		},
		"nested anonymous field is not outer field": func(f map[string][]byte) { f["Schemas/root.xsd.filter"] = []byte("Beam { Hidden }") },
		"namespace shadow cannot use same local type": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:extension base="t:Base">`, `<x:extension xmlns:t="urn:other" base="t:Base">`, 1))
		},
		"no default namespace fallback": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `base="t:Base"`, `base="Base"`, 1))
		},
		"restriction not treated as extension": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.ReplaceAll(string(f["Schemas/root.xsd"]), "x:extension", "x:restriction"))
		},
		"missing filtered field": func(f map[string][]byte) { f["Schemas/root.xsd.filter"] = []byte("Beam { Absent }") },
		"duplicate inherited field is ambiguous": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:attributeGroup ref="t:Audit"/>`, `<x:attribute name="Id"/><x:attributeGroup ref="t:Audit"/>`, 1))
		},
		"foreign base unresolved": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `base="t:Base"`, `xmlns:foreign="urn:foreign" base="foreign:Base"`, 1))
		},
		"prohibited field not inherited silently": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:attribute name="Id"/>`, `<x:attribute name="Id" use="prohibited"/>`, 1))
		},
		"wildcard not counted resource free": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:attribute name="Id"/>`, `<x:anyAttribute/>`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := skcBindingFiles(t)
			mutate(f)
			got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits())
			if err != nil || len(got[0].rules[0].issues) == 0 {
				t.Fatalf("unproved binding was not explicit: %v", err)
			}
		})
	}
}

func TestVFFSKCBindingQNameDefaultScopeAndFreshSource(t *testing.T) {
	f := skcBindingFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `base="t:Base"`, `xmlns="urn:type" base="Base"`, 1))
	got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits())
	if err != nil || len(got[0].rules[0].issues) != 0 {
		t.Fatalf("actual default namespace not respected: %v", err)
	}
	f["Schemas/root.xsd.filter"] = []byte("ChangedType { Name }")
	got, err = readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits())
	if err != nil || len(got[0].rules[0].issues) == 0 || got[0].filterSHA256 != sha256.Sum256(f["Schemas/root.xsd.filter"]) {
		t.Fatal("old filter witnesses substituted for current source")
	}
}

func TestVFFSKCBindingsRejectAmbiguityCyclesAndBoundWork(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"duplicate type": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `</x:schema>`, `<x:complexType name="Beam"/></x:schema>`, 1))
		},
		"cross simple/complex symbol duplicate": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `</x:schema>`, `<x:simpleType name="Beam"/></x:schema>`, 1))
		},
		"undeclared prefix": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `base="t:Base"`, `base="missing:Base"`, 1))
		},
		"inheritance cycle": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `base="t:Base"`, `base="t:Beam"`, 1))
		},
		"group cycle": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:attribute name="Created"/>`, `<x:attributeGroup ref="t:Audit"/>`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := skcBindingFiles(t)
			mutate(f)
			if got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, f), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*vffSKCBindingLimits){"missing budgets": func(l *vffSKCBindingLimits) { *l = vffSKCBindingLimits{} }, "visits": func(l *vffSKCBindingLimits) { l.visits = 3 }, "depth": func(l *vffSKCBindingLimits) { l.depth = 1 }, "XML bytes": func(l *vffSKCBindingLimits) { l.xml.bytes = 1 }, "XML nodes": func(l *vffSKCBindingLimits) { l.xml.nodes = 1 }} {
		t.Run(name, func(t *testing.T) {
			l := skcBindingLimits()
			mutate(&l)
			if got, err := readVFFClassificationBindings(context.Background(), skcDocumentContainer(t, skcBindingFiles(t)), l); err == nil || got != nil {
				t.Fatal("binding budget bypass")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationBindings(ctx, skcDocumentContainer(t, skcBindingFiles(t)), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled binding accepted")
	}
}

func TestVFFSKCBindingBudgetsRemainCumulativeAcrossExactPackages(t *testing.T) {
	f := skcBindingFiles(t)
	filter, err := readVFFSKCFilter(context.Background(), "Schemas/root.xsd.filter", f["Schemas/root.xsd.filter"], &vffSKCFilterBudget{limits: skcDocumentLimits().filters})
	if err != nil {
		t.Fatal(err)
	}
	work := &vffSKCBindingBudget{10000, 32}
	if _, err := readVFFSKCFilterBindings(context.Background(), "Schemas/root.xsd", f["Schemas/root.xsd"], *filter, &vffXMLBudget{limits: skcBindingLimits().xml}, work); err != nil {
		t.Fatal(err)
	}
	used := 10000 - work.remaining
	if used <= 0 {
		t.Fatal("expansion not charged")
	}
	raw := skcDocumentContainer(t, f).members["classifications/literal%20schema.skc"]
	c := vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		c.members["classifications/literal%20schema.skc"] = raw
		c.members["classifications/second.skc"] = append([]byte(nil), raw...)
		p["entries"] = append(vffRecord("8467", p["directory"], p["name"], p["path"]), vffRecord("8467", p["directory"], p["name"], vffRecord("8767", []byte("classifications/second.skc")))...)
	})
	l := skcBindingLimits()
	l.visits = 2 * used
	l.xml.bytes = 2 * len(f["Schemas/root.xsd"])
	if got, err := readVFFClassificationBindings(context.Background(), c, l); err != nil || len(got) != 2 {
		t.Fatalf("two-package exact budget: %v", err)
	}
	for _, budget := range []string{"visits", "XML bytes"} {
		copy := l
		if budget == "visits" {
			copy.visits--
		} else {
			copy.xml.bytes--
		}
		if got, err := readVFFClassificationBindings(context.Background(), c, copy); err == nil || got != nil {
			t.Fatal("second package budget failed to reject atomically", budget)
		}
	}
}
