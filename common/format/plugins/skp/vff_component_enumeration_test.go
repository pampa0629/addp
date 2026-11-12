package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func componentEnumerationFiles(t *testing.T) map[string][]byte {
	f := componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:simpleType name="Colors"><x:restriction base="x:token"><x:enumeration value="red"/><x:enumeration value="blue sky"/><x:enumeration value="green"/></x:restriction></x:simpleType><x:simpleType name="Label"><x:restriction base="r:Colors"><x:enumeration value="blue sky"/><x:enumeration value="red"/></x:restriction></x:simpleType><x:complexType name="Wall"><x:sequence><x:element name="Name" type="r:Label"/></x:sequence></x:complexType></x:schema>`)
	return f
}
func componentEnumerationField(role string, options ...string) []byte {
	extra := append(componentOptionsEntry(options...), componentClassString("value", "not selected or evaluated")...)
	return componentFieldDictionary("Name", componentMetadataEntries("", nil), componentFieldDictionary("Label", componentMetadataEntries(role, extra)))
}
func readComponentEnumerations(ctx context.Context, c *vffContainer, limits vffClassificationLimits, binding vffSKCBindingLimits) (*vffComponentEnumerationFacts, error) {
	return readVFFComponentEnumerationMetadata(ctx, c, componentSchemaTitle, componentSchemaPackage, "Wall", []vffComponentFieldPath{{"Name", "Label"}}, limits, binding)
}

func TestVFFComponentEnumerationTypedSetSourcesAndOriginalOrder(t *testing.T) {
	f := componentEnumerationFiles(t)
	c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationField("enumeration", "red", " blue\t sky ")), componentFieldsDefinition(2, componentEnumerationField("enumeration", "blue sky", "red")))
	before := bytes.Clone(c.members["model.dat"])
	got, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.enumerations) != 2 {
		t.Fatal("enumeration facts missing")
	}
	first := got.enumerations[0]
	if first.metadata.attributeType != "enumeration" || first.options[0].raw != "red" || first.options[1].raw != " blue\t sky " || first.options[1].value.text != "blue sky" || got.enumerations[1].options[0].raw != "blue sky" {
		t.Fatal("raw array order, original text or typed normalization changed")
	}
	for _, fact := range got.enumerations {
		if fact.field.declaration.name != "Name" || fact.typeDeclaration.name != "Label" || len(fact.facets) != 5 {
			t.Fatal("field, type or inherited enum facets lost")
		}
		for _, option := range fact.options {
			if len(option.declarations) != 2 || option.sha256 != sha256.Sum256(vffRecord("AD38", []byte(option.raw))) {
				t.Fatal("enum ancestry or option source identity lost")
			}
			for _, w := range option.declarations {
				if w.kind != "enumeration" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
					t.Fatal("enum declaration witness differs")
				}
			}
		}
	}
	if !bytes.Equal(before, c.members["model.dat"]) || len(got.opaque) == 0 {
		t.Fatal("source changed or unselected value silently consumed")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("enumeration proof opened composition admission")
	}
}

func TestVFFComponentEnumerationRejectsInvalidOptionSetsAtomically(t *testing.T) {
	for name, field := range map[string][]byte{
		"wrong marker":                        componentEnumerationField("string", "red", "blue sky"),
		"missing":                             componentEnumerationField("enumeration", "red"),
		"extra":                               componentEnumerationField("enumeration", "red", "blue sky", "green"),
		"base value excluded by derived type": componentEnumerationField("enumeration", "red", "green"),
		"duplicate":                           componentEnumerationField("enumeration", "red", "red"),
		"typed alias duplicate":               componentEnumerationField("enumeration", "red", " red "),
		"case folding forbidden":              componentEnumerationField("enumeration", "RED", "blue sky"),
		"URI decoding forbidden":              componentEnumerationField("enumeration", "%72ed", "blue sky"),
		"non array":                           componentFieldDictionary("Name", componentMetadataEntries("", nil), componentFieldDictionary("Label", componentMetadataEntries("enumeration", componentClassString("options", "red")))),
		"missing options":                     componentFieldDictionary("Name", componentMetadataEntries("", nil), componentFieldDictionary("Label", componentMetadataEntries("enumeration", nil))),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentEnumerationFiles(t), componentFieldsDefinition(1, componentEnumerationField("enumeration", "red", "blue sky")), componentFieldsDefinition(2, field))
			if got, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid or partial enum metadata returned")
			}
		})
	}
}

func TestVFFComponentEnumerationEmptyMemberAndSchemaAliases(t *testing.T) {
	f := componentEnumerationFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:simpleType name="Label"><x:restriction base="x:token"><x:enumeration value=""/><x:enumeration value="a b"/><x:enumeration value="a   b"/></x:restriction></x:simpleType><x:complexType name="Wall"><x:sequence><x:element name="Name" type="r:Label"/></x:sequence></x:complexType></x:schema>`)
	c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationField("enumeration", "", "a\tb")))
	got, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	options := got.enumerations[0].options
	if options[0].raw != "" || options[0].value.text != "" || len(options[1].declarations) != 2 || options[1].value.text != "a b" {
		t.Fatal("empty member or duplicate schema declarations confused with duplicate options")
	}
}

func TestVFFComponentEnumerationRejectsUnprovedOrInconsistentTypes(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"no enum": func(s string) string {
			return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, `<x:enumeration value="red"/>`, ""), `<x:enumeration value="blue sky"/>`, ""), `<x:enumeration value="green"/>`, "")
		},
		"derived not in base": func(s string) string {
			return strings.Replace(s, `base="r:Colors"><x:enumeration value="blue sky"/>`, `base="r:Colors"><x:enumeration value="other"/>`, 1)
		},
		"unknown facet": func(s string) string {
			return strings.Replace(s, `base="r:Colors">`, `base="r:Colors"><x:pattern value=".*"/>`, 1)
		},
		"numeric enum outside scope": func(s string) string { return strings.Replace(s, `base="x:token"`, `base="x:integer"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			f := componentEnumerationFiles(t)
			f["Schemas/root.xsd"] = []byte(change(string(f["Schemas/root.xsd"])))
			if got, err := readComponentEnumerations(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationField("enumeration", "red", "blue sky"))), componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("unproved enum type returned")
			}
		})
	}
	f := componentEnumerationFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.ReplaceAll(string(f["Schemas/root.xsd"]), `blue sky`, `é`))
	if got, err := readComponentEnumerations(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationField("enumeration", "red", "e\u0301"))), componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("Unicode normalization was silently applied")
	}
}

func TestVFFComponentEnumerationLimitsAndCancellation(t *testing.T) {
	c := componentFieldsContainer(t, componentEnumerationFiles(t), componentFieldsDefinition(1, componentEnumerationField("enumeration", "red", "blue sky")))
	limits := componentClassLimits()
	limits.depth = 27
	if got, err := readComponentEnumerations(context.Background(), c, limits, skcBindingLimits()); err == nil || got != nil {
		t.Fatal("enum option depth budget bypass")
	}
	binding := skcBindingLimits()
	binding.visits = 4
	if got, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), binding); err == nil || got != nil {
		t.Fatal("enum schema budget bypass")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readComponentEnumerations(ctx, c, componentClassLimits(), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled enum facts returned")
	}
}
