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

func componentMetadataEntries(role string, extra []byte) []byte {
	entries := append(vffRecord("B636", []byte("is_hidden")), vffRecord("A438", vffRecord("AA38", []byte{0}))...)
	if role != "" {
		entries = append(entries, componentClassString("attribute_type", role)...)
	}
	return append(entries, extra...)
}
func componentOptionsEntry(options ...string) []byte {
	var values [][]byte
	for _, option := range options {
		values = append(values, vffRecord("A438", vffRecord("AD38", []byte(option))))
	}
	return append(vffRecord("B636", []byte("options")), vffRecord("A438", vffRecord("AE38", values...))...)
}
func componentChoiceField(options ...string) []byte {
	return componentFieldDictionary("Link", componentMetadataEntries("", nil), componentFieldDictionary("Link", componentMetadataEntries("choice", componentOptionsEntry(options...))))
}
func componentChoiceFiles(t *testing.T) map[string][]byte {
	f := componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:complexType name="Wall"><x:sequence><x:element name="Link"><x:complexType><x:choice><x:element ref="r:A"/><x:element ref="r:B"/></x:choice></x:complexType></x:element></x:sequence></x:complexType><x:complexType name="Target"/><x:element name="A" type="r:Target"/><x:element name="B" type="r:Target"/></x:schema>`)
	f["Schemas/root.xsd.filter"] = []byte("Wall { Link }\n{ Link }")
	return f
}
func readComponentChoices(ctx context.Context, c *vffContainer, limits vffClassificationLimits) (*vffComponentChoiceFacts, error) {
	return readVFFComponentChoiceMetadata(ctx, c, componentSchemaTitle, componentSchemaPackage, "Wall", []vffComponentFieldPath{{"Link", "Link"}}, limits, skcBindingLimits())
}

func TestVFFComponentMetadataDirectChoiceKeepsExactOptionsAndFilterFacts(t *testing.T) {
	f := componentChoiceFiles(t)
	parent := bytes.Replace(componentMetadataEntries("", nil), vffRecord("AA38", []byte{0}), vffRecord("AA38", []byte{1}), 1)
	field := componentFieldDictionary("Link", parent, componentFieldDictionary("Link", componentMetadataEntries("choice", componentOptionsEntry("urn:root:B", "urn:root:A"))))
	c := componentFieldsContainer(t, f, componentFieldsDefinition(1, field))
	got, err := readComponentChoices(context.Background(), c, componentClassLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.choices) != 1 {
		t.Fatal("choice facts missing")
	}
	fact := got.choices[0]
	if !fact.metadata.fieldHidden.value || fact.metadata.leafHidden.value || fact.metadata.attributeType != "choice" || len(fact.options) != 2 || fact.options[0].name != "urn:root:B" || fact.options[1].name != "urn:root:A" {
		t.Fatalf("independent flags or original option order lost: %#v", fact)
	}
	for _, option := range fact.options {
		if option.declaration.kind != "element" || option.reference.kind != "element" || option.declaration.sha256 != sha256.Sum256(f[option.declaration.member][option.declaration.begin:option.declaration.end]) || option.sha256 != sha256.Sum256(vffRecord("AD38", []byte(option.name))) {
			t.Fatal("exact option and XSD identity lost")
		}
	}
	if len(fact.metadata.filters) != 2 || !fact.metadata.filters[0].listed || !fact.metadata.filters[1].listed {
		t.Fatal("white/black membership was not preserved separately")
	}
	for _, filter := range fact.metadata.filters {
		if filter.rule.sha256 != sha256.Sum256(f[filter.member][filter.rule.begin:filter.rule.end]) {
			t.Fatal("filter declaration identity lost")
		}
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("metadata facts opened composition admission")
	}
}

func TestVFFComponentMetadataRejectsMissingWrongAndDuplicateOptions(t *testing.T) {
	for name, field := range map[string][]byte{
		"missing option":   componentChoiceField("urn:root:A"),
		"extra option":     componentChoiceField("urn:root:A", "urn:root:B", "urn:root:C"),
		"duplicate":        componentChoiceField("urn:root:A", "urn:root:A"),
		"wrong namespace":  componentChoiceField("urn:other:A", "urn:root:B"),
		"URI decoding":     componentChoiceField("urn:root:%41", "urn:root:B"),
		"not array":        componentFieldDictionary("Link", componentMetadataEntries("", nil), componentFieldDictionary("Link", componentMetadataEntries("choice", componentClassString("options", "urn:root:A")))),
		"missing metadata": componentFieldDictionary("Link", nil, componentFieldDictionary("Link", componentOptionsEntry("urn:root:A", "urn:root:B"))),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentChoiceFiles(t), componentFieldsDefinition(1, componentChoiceField("urn:root:A", "urn:root:B")), componentFieldsDefinition(2, field))
			if got, err := readComponentChoices(context.Background(), c, componentClassLimits()); err == nil || got != nil {
				t.Fatal("invalid or partial choice facts returned")
			}
		})
	}
	for name, change := range map[string]func(string) string{
		"missing declaration": func(s string) string { return strings.Replace(s, `<x:element name="A" type="r:Target"/>`, "", 1) },
		"wrong role":          func(s string) string { return strings.Replace(s, `ref="r:A"`, `name="A" type="r:Target"`, 1) },
		"unsupported nested compositor": func(s string) string {
			return strings.Replace(s, `<x:element ref="r:A"/>`, `<x:sequence><x:element ref="r:A"/></x:sequence>`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := componentChoiceFiles(t)
			f["Schemas/root.xsd"] = []byte(change(string(f["Schemas/root.xsd"])))
			if got, err := readComponentChoices(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentChoiceField("urn:root:A", "urn:root:B"))), componentClassLimits()); err == nil || got != nil {
				t.Fatal("unproved XSD choice returned")
			}
		})
	}
}

func TestVFFComponentMetadataStringMarkerAndBooleanMustBeExact(t *testing.T) {
	f := componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:maxLength value="20"/>`, `<x:enumeration value="value"/>`, 1))
	if got, err := readComponentFields(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentFieldString("Name", "Label", "value"))), []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("enumeration was silently treated as plain string metadata")
	}
	valid := componentFieldString("Name", "Label", "value")
	for name, field := range map[string][]byte{
		"wrong type marker":       bytes.Replace(valid, []byte("string"), []byte("choice"), 1),
		"invalid boolean":         bytes.Replace(valid, vffRecord("AA38", []byte{0}), vffRecord("AA38", []byte{2}), 1),
		"string boolean":          bytes.Replace(valid, vffRecord("AA38", []byte{0}), vffRecord("AD38", []byte{0}), 1),
		"missing hidden":          componentFieldDictionary("Name", nil, componentFieldDictionary("Label", componentMetadataEntries("string", componentClassString("value", "value")))),
		"string options unproved": componentFieldDictionary("Name", componentMetadataEntries("", nil), componentFieldDictionary("Label", append(componentMetadataEntries("string", componentClassString("value", "value")), componentOptionsEntry("value")...))),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentFieldsFiles(t), componentFieldsDefinition(1, field))
			if got, err := readComponentFields(context.Background(), c, []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid string metadata returned")
			}
		})
	}
}

func TestVFFComponentMetadataBudgetsAndCancellation(t *testing.T) {
	c := componentFieldsContainer(t, componentChoiceFiles(t), componentFieldsDefinition(1, componentChoiceField("urn:root:A", "urn:root:B")))
	limits := componentClassLimits()
	limits.depth = 27
	if got, err := readComponentChoices(context.Background(), c, limits); err == nil || got != nil {
		t.Fatal("option string depth was not bounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readComponentChoices(ctx, c, componentClassLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled metadata returned")
	}
}
