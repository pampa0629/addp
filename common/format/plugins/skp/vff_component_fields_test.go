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

func componentFieldDictionary(name string, entries []byte, children ...[]byte) []byte {
	var nested []byte
	if len(children) != 0 {
		nested = vffRecord("DC05", vffRecord("DD05", vffRecord("B136", vffRecord("B236", children...))))
	}
	return vffRecord("B336", vffRecord("B036", nested), vffRecord("B436", []byte(name)), vffRecord("B536", entries), vffRecord("B736", []byte{0, 0, 0, 0}))
}
func componentFieldString(field, typ, raw string) []byte {
	return componentFieldDictionary(field, componentClassString("value", "separate outer value"),
		componentFieldDictionary(typ, componentClassString("value", raw)))
}
func componentFieldsDefinition(id byte, fields ...[]byte) []byte {
	return componentClassDefinition(id,
		componentClassDictionary("AppliedSchemaTypes", componentClassString(componentSchemaTitle, "Wall")),
		componentFieldDictionary(componentSchemaTitle, componentClassString("SchemaType", "urn:root:Wall"), fields...))
}
func componentFieldsContainer(t *testing.T, files map[string][]byte, defs ...[]byte) *vffContainer {
	c := skcGraphContainer(t, files)
	components := componentClassContainer(defs...).members["model.dat"]
	c.members["model.dat"] = vffRecord("F401", c.members["model.dat"][6:], components[6:])
	return c
}
func componentFieldsFiles(t *testing.T) map[string][]byte {
	f := componentSchemaFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:simpleType name="Label"><x:restriction base="x:normalizedString"><x:maxLength value="20"/></x:restriction></x:simpleType><x:complexType name="Base"><x:sequence><x:element name="Name" type="r:Label"/></x:sequence></x:complexType><x:complexType name="Wall"><x:complexContent><x:extension base="r:Base"/></x:complexContent></x:complexType></x:schema>`)
	return f
}
func readComponentFields(ctx context.Context, c *vffContainer, paths []vffComponentFieldPath, limits vffClassificationLimits, bindingLimits vffSKCBindingLimits) (*vffComponentFieldFacts, error) {
	return readVFFComponentFieldValues(ctx, c, componentSchemaTitle, componentSchemaPackage, "Wall", paths, limits, bindingLimits)
}

func TestVFFComponentFieldValuesPreserveExactPathsSourcesAndEmptyStrings(t *testing.T) {
	f := componentFieldsFiles(t)
	c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentFieldString("Name", "Label", "原始\t文字\n")), componentFieldsDefinition(2, componentFieldString("Name", "Label", "")))
	before := bytes.Clone(c.members["model.dat"])
	got, err := readComponentFields(context.Background(), c, []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.values) != 2 || got.values[0].raw != "原始\t文字\n" || got.values[0].value.text != "原始 文字 " || got.values[1].raw != "" || got.values[1].value.text != "" {
		t.Fatalf("path value lost or empty replaced: %#v", got)
	}
	for _, v := range got.values {
		if v.field.declaration.name != "Name" || v.typeDeclaration.name != "Label" || len(v.field.via) < 2 || v.primitive.Local != "normalizedString" || len(v.facets) != 1 || v.fieldOffset >= v.dictionaryOffset || v.dictionaryOffset >= v.valueOffset {
			t.Fatalf("typed inheritance or model source lost: %#v", v)
		}
		w := v.typeDeclaration
		if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) || v.valueSHA256 != sha256.Sum256(vffRecord("AD38", []byte(v.raw))) {
			t.Fatal("type or source value bytes differ")
		}
	}
	outer := sha256.Sum256(vffRecord("AD38", []byte("separate outer value")))
	found := false
	for _, opaque := range got.opaque {
		found = found || opaque.sha256 == outer
	}
	if !found || !bytes.Equal(before, c.members["model.dat"]) {
		t.Fatal("outer value erased, substituted or source changed")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("partial field proof opened composition admission")
	}
}

func TestVFFComponentFieldValuesRejectBrokenPathsAndValuesAtomically(t *testing.T) {
	paths := []vffComponentFieldPath{{"Name", "Label"}}
	for name, fields := range map[string][][]byte{
		"missing field":       {componentFieldString("Other", "Label", "value")},
		"wrong type leaf":     {componentFieldString("Name", "Other", "value")},
		"duplicate field":     {componentFieldString("Name", "Label", "one"), componentFieldString("Name", "Label", "two")},
		"duplicate type leaf": {componentFieldDictionary("Name", nil, componentFieldDictionary("Label", componentClassString("value", "one")), componentFieldDictionary("Label", componentClassString("value", "two")))},
		"missing value":       {componentFieldDictionary("Name", nil, componentFieldDictionary("Label", nil))},
		"non string":          {componentFieldDictionary("Name", nil, componentFieldDictionary("Label", append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("AA38", []byte{1}))...)))},
		"invalid UTF8":        {componentFieldString("Name", "Label", string([]byte{0xff}))},
		"length facet":        {componentFieldString("Name", "Label", strings.Repeat("a", 21))},
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentFieldsFiles(t), componentFieldsDefinition(1, componentFieldString("Name", "Label", "valid first")), componentFieldsDefinition(2, fields...))
			if got, err := readComponentFields(context.Background(), c, paths, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid or partial value facts returned")
			}
		})
	}
	for _, requested := range [][]vffComponentFieldPath{nil, {{"Name", "Label"}, {"Name", "Label"}}, {{"Name", "Other"}}, {{"", "Label"}}} {
		c := componentFieldsContainer(t, componentFieldsFiles(t), componentFieldsDefinition(1, componentFieldString("Name", "Label", "value")))
		if got, err := readComponentFields(context.Background(), c, requested, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("unspecified or guessed path accepted")
		}
	}
}

func TestVFFComponentFieldValuesKeepFieldConstraintsAndUnknownSemantics(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"fixed mismatch": func(s string) string {
			return strings.Replace(s, `name="Name" type="r:Label"`, `name="Name" type="r:Label" fixed="locked"`, 1)
		},
		"duplicate inherited field": func(s string) string {
			return strings.Replace(s, `base="r:Base"/>`, `base="r:Base"><x:sequence><x:element name="Name" type="r:Label"/></x:sequence></x:extension>`, 1)
		},
		"unresolved base": func(s string) string { return strings.Replace(s, `base="r:Base"`, `base="r:Missing"`, 1) },
		"unknown facet": func(s string) string {
			return strings.Replace(s, `<x:maxLength value="20"/>`, `<x:pattern value=".*"/>`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := componentFieldsFiles(t)
			f["Schemas/root.xsd"] = []byte(change(string(f["Schemas/root.xsd"])))
			if got, err := readComponentFields(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentFieldString("Name", "Label", "value"))), []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("unproved field value accepted")
			}
		})
	}
	f := componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `name="Name" type="r:Label"`, `name="Name" type="r:Label" default="not an empty replacement"`, 1))
	got, err := readComponentFields(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentFieldString("Name", "Label", ""))), []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits())
	if err != nil || got.values[0].raw != "" || got.values[0].value.text != "" {
		t.Fatalf("literal dictionary string replaced with XML instance default: %v", err)
	}
	f = componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `name="Name" type="r:Label"`, `name="Name" type="r:Label" fixed="one two"`, 1))
	got, err = readComponentFields(context.Background(), componentFieldsContainer(t, f, componentFieldsDefinition(1, componentFieldString("Name", "Label", "one\ttwo"))), []vffComponentFieldPath{{"Name", "Label"}}, componentClassLimits(), skcBindingLimits())
	if err != nil || got.values[0].raw != "one\ttwo" || got.values[0].value.text != "one two" {
		t.Fatalf("typed fixed equality ignored normalization or rewrote source: %v", err)
	}
}

func TestVFFComponentFieldValuesBoundsAndCancellation(t *testing.T) {
	c := componentFieldsContainer(t, componentFieldsFiles(t), componentFieldsDefinition(1, componentFieldString("Name", "Label", "value")))
	paths := []vffComponentFieldPath{{"Name", "Label"}}
	for _, limits := range []vffClassificationLimits{{}, {bytes: 1, records: 10000, depth: 64}, {bytes: 1 << 20, records: 10, depth: 64}, {bytes: 1 << 20, records: 10000, depth: 23}} {
		if got, err := readComponentFields(context.Background(), c, paths, limits, skcBindingLimits()); err == nil || got != nil {
			t.Fatal("component field budget bypass")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readComponentFields(ctx, c, paths, componentClassLimits(), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled component field read succeeded")
	}
}
