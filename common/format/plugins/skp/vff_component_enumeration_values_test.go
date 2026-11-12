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

func componentEnumerationSelected(entry []byte, options ...string) []byte {
	return componentFieldDictionary("Name", componentMetadataEntries("", componentClassString("value", "unselected outer")),
		componentFieldDictionary("Label", componentMetadataEntries("enumeration", append(componentOptionsEntry(options...), entry...))))
}
func readComponentEnumerationValues(ctx context.Context, c *vffContainer, cl vffClassificationLimits, l vffSKCBindingLimits) (*vffComponentEnumerationFacts, error) {
	return readVFFComponentEnumerationValues(ctx, c, componentSchemaTitle, componentSchemaPackage, "Wall", []vffComponentFieldPath{{"Name", "Label"}}, cl, l)
}

func TestVFFComponentEnumerationValuesPreserveTypedSelectionAndWitnesses(t *testing.T) {
	f := componentEnumerationFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `name="Name"`, `name="Name" fixed="blue   sky"`, 1))
	field := componentEnumerationSelected(componentClassString("value", " blue\t sky "), "red", "blue sky")
	c := componentFieldsContainer(t, f, componentFieldsDefinition(1, field))
	before := bytes.Clone(c.members["model.dat"])
	got, err := readComponentEnumerationValues(context.Background(), c, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	fact := got.enumerations[0]
	v := fact.selected
	if len(got.enumerations) != 1 || v == nil || v.raw != " blue\t sky " || v.value.text != "blue sky" || v.option != 1 || fact.options[v.option].raw != "blue sky" || len(fact.options[v.option].declarations) != 2 {
		t.Fatalf("selection, option order or inherited declarations lost: %#v", fact)
	}
	if v.fieldOffset >= v.dictionaryOffset || v.dictionaryOffset >= v.valueOffset || v.sha256 != sha256.Sum256(vffRecord("AD38", []byte(v.raw))) {
		t.Fatal("selected model source identity lost")
	}
	for _, w := range fact.options[v.option].declarations {
		if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatal("selected enum declaration source differs")
		}
	}
	outer := sha256.Sum256(vffRecord("AD38", []byte("unselected outer")))
	found := false
	for _, opaque := range got.opaque {
		found = found || opaque.sha256 == outer
	}
	if !found || !bytes.Equal(before, c.members["model.dat"]) {
		t.Fatal("outer value replaced selected leaf or source changed")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("selected enum proof opened admission")
	}
}

func TestVFFComponentEnumerationValuesRejectInvalidSelectionsAtomically(t *testing.T) {
	for name, entry := range map[string][]byte{
		"missing":                  nil,
		"empty wrapper":            append(vffRecord("B636", []byte("value")), vffRecord("A438")...),
		"empty string":             componentClassString("value", ""),
		"outside set":              componentClassString("value", "other"),
		"excluded by derived enum": componentClassString("value", "green"),
		"case fold":                componentClassString("value", "RED"),
		"URI decode":               componentClassString("value", "%72ed"),
		"invalid UTF8":             componentClassString("value", string([]byte{0xff})),
		"wrong type":               append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("A938", make([]byte, 8)))...),
		"duplicate value children": append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("AD38", []byte("red")), vffRecord("AD38", []byte("red")))...),
		"duplicate key":            append(componentClassString("value", "red"), componentClassString("value", "red")...),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentEnumerationFiles(t), componentFieldsDefinition(1, componentEnumerationSelected(componentClassString("value", "red"), "red", "blue sky")), componentFieldsDefinition(2, componentEnumerationSelected(entry, "red", "blue sky")))
			if got, err := readComponentEnumerationValues(context.Background(), c, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid selection returned partial facts")
			}
		})
	}
}

func TestVFFComponentEnumerationValuesRequireProvedOptionsAndFixedValue(t *testing.T) {
	for name, field := range map[string][]byte{
		"missing option":         componentEnumerationSelected(componentClassString("value", "red"), "red"),
		"extra option":           componentEnumerationSelected(componentClassString("value", "red"), "red", "blue sky", "green"),
		"duplicate option":       componentEnumerationSelected(componentClassString("value", "red"), "red", "red"),
		"typed duplicate option": componentEnumerationSelected(componentClassString("value", "red"), "red", " red "),
		"wrong marker":           bytes.Replace(componentEnumerationSelected(componentClassString("value", "red"), "red", "blue sky"), []byte("enumeration"), []byte("string"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentEnumerationFiles(t), componentFieldsDefinition(1, field))
			if got, err := readComponentEnumerationValues(context.Background(), c, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("selected member bypassed invalid metadata set")
			}
		})
	}
	for _, constraint := range []string{`fixed="blue sky"`, `default="red"`} {
		f := componentEnumerationFiles(t)
		f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `name="Name"`, `name="Name" `+constraint, 1))
		entry := componentClassString("value", "red")
		if strings.HasPrefix(constraint, "default") {
			entry = nil
		}
		c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationSelected(entry, "red", "blue sky")))
		if got, err := readComponentEnumerationValues(context.Background(), c, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("fixed mismatch or default fallback accepted")
		}
	}
}

func TestVFFComponentEnumerationValuesKeepMetadataAndEmptyMemberBoundaries(t *testing.T) {
	c := componentFieldsContainer(t, componentEnumerationFiles(t), componentFieldsDefinition(1, componentEnumerationSelected(componentClassString("value", "not an enum"), "red", "blue sky")))
	metadata, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), skcBindingLimits())
	if err != nil || metadata.enumerations[0].selected != nil {
		t.Fatal("metadata read claimed selection validation", err)
	}
	for _, raw := range []string{"", " \t "} {
		f := componentEnumerationFiles(t)
		f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:simpleType name="Label"><x:restriction base="x:token"><x:enumeration value=""/><x:enumeration value="red"/></x:restriction></x:simpleType><x:complexType name="Wall"><x:sequence><x:element name="Name" type="r:Label"/></x:sequence></x:complexType></x:schema>`)
		c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentEnumerationSelected(componentClassString("value", raw), "", "red")))
		if _, err := readComponentEnumerations(context.Background(), c, componentClassLimits(), skcBindingLimits()); err != nil {
			t.Fatal("explicit empty option metadata rejected", err)
		}
		if got, err := readComponentEnumerationValues(context.Background(), c, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("empty typed selection expanded the approved nonempty scope")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readComponentEnumerationValues(ctx, c, componentClassLimits(), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled selection returned")
	}
}
