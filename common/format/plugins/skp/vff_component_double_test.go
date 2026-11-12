package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func componentDoubleFiles(t *testing.T) map[string][]byte {
	f := componentFieldsFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:root" targetNamespace="urn:root"><x:simpleType name="Measure"><x:restriction base="x:double"/></x:simpleType><x:simpleType name="PositiveMeasure"><x:restriction base="r:Measure"/></x:simpleType><x:complexType name="Base"><x:sequence><x:element name="Depth" type="r:PositiveMeasure" minOccurs="0" nillable="true"/></x:sequence></x:complexType><x:complexType name="Wall"><x:complexContent><x:extension base="r:Base"/></x:complexContent></x:complexType></x:schema>`)
	return f
}
func componentDoubleEntry(bits uint64) []byte {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], bits)
	return append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("A938", raw[:]))...)
}
func componentDoubleField(value []byte) []byte {
	return componentFieldDictionary("Depth", componentMetadataEntries("", componentClassString("value", "outer")),
		componentFieldDictionary("PositiveMeasure", componentMetadataEntries("", componentClassString("value", "intermediate")),
			componentFieldDictionary("Measure", componentMetadataEntries("double", value))))
}
func componentDoublePaths() []vffComponentDoublePath {
	return []vffComponentDoublePath{{"Depth", []string{"PositiveMeasure", "Measure"}}}
}
func readComponentDoubles(ctx context.Context, c *vffContainer, paths []vffComponentDoublePath, cl vffClassificationLimits, l vffSKCBindingLimits) (*vffComponentDoubleFacts, error) {
	return readVFFComponentDoubleValues(ctx, c, componentSchemaTitle, componentSchemaPackage, "Wall", paths, cl, l)
}

func TestVFFComponentDoubleValuesPreserveBitsAndExactTypeChain(t *testing.T) {
	f := componentDoubleFiles(t)
	// Include both zero signs, subnormals, an inexact decimal, negatives, and
	// the largest finite values. A type's name does not add positive bounds.
	values := []uint64{0, 1 << 63, 1, 1<<63 | 1, 0x3f9eb851eb851eb8, 0x4074d00000000000, 0xbff0000000000000, 0x7fefffffffffffff, 0xffefffffffffffff}
	var defs [][]byte
	for i, bits := range values {
		defs = append(defs, componentFieldsDefinition(byte(i+1), componentDoubleField(componentDoubleEntry(bits))))
	}
	c := componentFieldsContainer(t, f, defs...)
	before := bytes.Clone(c.members["model.dat"])
	paths := componentDoublePaths()
	got, err := readComponentDoubles(context.Background(), c, paths, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	paths[0].types[0] = "caller changed"
	if len(got.values) != len(values) {
		t.Fatal("values missing")
	}
	for i, value := range got.values {
		if value.bits != values[i] || len(value.types) != 2 || value.types[0].name != "PositiveMeasure" || value.types[1].name != "Measure" || value.path.types[0] != "PositiveMeasure" || len(value.field.via) < 2 || value.metadata.attributeType != "double" {
			t.Fatalf("bits, inherited field, metadata or exact named chain lost: %#v", value)
		}
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], values[i])
		if value.fieldOffset >= value.dictionaryOffset || value.dictionaryOffset >= value.valueOffset || value.valueSHA256 != sha256.Sum256(vffRecord("A938", raw[:])) {
			t.Fatal("model source identity lost")
		}
		for _, w := range append(value.types, value.field.declaration) {
			if w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
				t.Fatal("XSD source witness differs")
			}
		}
	}
	for _, raw := range []string{"outer", "intermediate"} {
		sum := sha256.Sum256(vffRecord("AD38", []byte(raw)))
		found := false
		for _, opaque := range got.opaque {
			found = found || opaque.sha256 == sum
		}
		if !found {
			t.Fatal("unselected outer/intermediate value erased")
		}
	}
	if !bytes.Equal(before, c.members["model.dat"]) {
		t.Fatal("source changed")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("private double proof opened admission")
	}
	// No general floating-point lexical/facet semantics were enabled.
	scope, err := readVFFComponentFieldScope(context.Background(), c, componentSchemaTitle, componentSchemaPackage, "Wall", componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, known, err := scope.b.scalar(scope.b.root, xml.Name{Space: vffXSDNS, Local: "double"}, "0.03"); err != nil || known {
		t.Fatal("general double evaluator unexpectedly enabled", err)
	}
}

func TestVFFComponentDoubleValuesRejectMalformedAndNonFiniteAtomically(t *testing.T) {
	valid := componentDoubleEntry(0x3ff0000000000000)
	for name, entry := range map[string][]byte{
		"missing":            nil,
		"empty":              append(vffRecord("B636", []byte("value")), vffRecord("A438")...),
		"positive infinity":  componentDoubleEntry(0x7ff0000000000000),
		"negative infinity":  componentDoubleEntry(0xfff0000000000000),
		"quiet NaN":          componentDoubleEntry(0x7ff8000000000001),
		"signalling NaN":     componentDoubleEntry(0xfff0000000000001),
		"Length tag":         bytes.Replace(valid, []byte{0xA9, 0x38}, []byte{0xAF, 0x38}, 1),
		"text number":        componentClassString("value", "1"),
		"seven bytes":        append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("A938", make([]byte, 7)))...),
		"nine bytes":         append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("A938", make([]byte, 9)))...),
		"duplicate children": append(vffRecord("B636", []byte("value")), vffRecord("A438", vffRecord("A938", make([]byte, 8)), vffRecord("A938", make([]byte, 8)))...),
		"truncated":          valid[:len(valid)-1],
		"trailing byte":      append(append([]byte(nil), valid...), 0),
		"duplicate key":      append(append([]byte(nil), valid...), valid...),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentDoubleFiles(t), componentFieldsDefinition(1, componentDoubleField(valid)), componentFieldsDefinition(2, componentDoubleField(entry)))
			if got, err := readComponentDoubles(context.Background(), c, componentDoublePaths(), componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("invalid or partial double facts returned")
			}
		})
	}
}

func TestVFFComponentDoubleValuesRejectUnprovedSchemaAndMetadata(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"base facet": func(s string) string {
			return strings.Replace(s, `<x:restriction base="x:double"/>`, `<x:restriction base="x:double"><x:minInclusive value="0"/></x:restriction>`, 1)
		},
		"derived facet": func(s string) string {
			return strings.Replace(s, `<x:restriction base="r:Measure"/>`, `<x:restriction base="r:Measure"><x:whiteSpace value="collapse"/></x:restriction>`, 1)
		},
		"unknown facet": func(s string) string {
			return strings.Replace(s, `<x:restriction base="x:double"/>`, `<x:restriction base="x:double"><x:unknown/></x:restriction>`, 1)
		},
		"float":   func(s string) string { return strings.Replace(s, `base="x:double"`, `base="x:float"`, 1) },
		"decimal": func(s string) string { return strings.Replace(s, `base="x:double"`, `base="x:decimal"`, 1) },
		"list": func(s string) string {
			return strings.Replace(s, `<x:restriction base="x:double"/>`, `<x:list itemType="x:double"/>`, 1)
		},
		"union": func(s string) string {
			return strings.Replace(s, `<x:restriction base="x:double"/>`, `<x:union memberTypes="x:double x:string"/>`, 1)
		},
		"fixed":   func(s string) string { return strings.Replace(s, `name="Depth"`, `name="Depth" fixed="1"`, 1) },
		"default": func(s string) string { return strings.Replace(s, `name="Depth"`, `name="Depth" default="1"`, 1) },
		"type final": func(s string) string {
			return strings.Replace(s, `name="Measure"`, `name="Measure" final="restriction"`, 1)
		},
		"schema finalDefault": func(s string) string {
			return strings.Replace(s, `<x:schema `, `<x:schema finalDefault="restriction" `, 1)
		},
		"wrong namespace": func(s string) string { return strings.Replace(s, `base="r:Measure"`, `base="x:Measure"`, 1) },
		"type cycle":      func(s string) string { return strings.Replace(s, `base="x:double"`, `base="r:PositiveMeasure"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			f := componentDoubleFiles(t)
			f["Schemas/root.xsd"] = []byte(change(string(f["Schemas/root.xsd"])))
			c := componentFieldsContainer(t, f, componentFieldsDefinition(1, componentDoubleField(componentDoubleEntry(0x3ff0000000000000))))
			if got, err := readComponentDoubles(context.Background(), c, componentDoublePaths(), componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("unproved numeric constraints accepted")
			}
		})
	}
	valid := componentDoubleField(componentDoubleEntry(0))
	for name, field := range map[string][]byte{
		"wrong marker":         bytes.Replace(valid, []byte("double"), []byte("string"), 1),
		"wrong hidden flag":    bytes.Replace(valid, vffRecord("AA38", []byte{0}), vffRecord("AA38", []byte{2}), 1),
		"options":              componentDoubleField(append(componentDoubleEntry(0), componentOptionsEntry("0")...)),
		"duplicate dictionary": componentFieldDictionary("Depth", componentMetadataEntries("", nil), componentFieldDictionary("PositiveMeasure", componentMetadataEntries("", nil)), componentFieldDictionary("PositiveMeasure", componentMetadataEntries("", nil))),
		"wrong dictionary":     bytes.Replace(valid, []byte("Measure"), []byte("Unknown"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			c := componentFieldsContainer(t, componentDoubleFiles(t), componentFieldsDefinition(1, field))
			if got, err := readComponentDoubles(context.Background(), c, componentDoublePaths(), componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("unproved metadata accepted")
			}
		})
	}
}

func TestVFFComponentDoubleValuesPathsBudgetsAndCancellation(t *testing.T) {
	c := componentFieldsContainer(t, componentDoubleFiles(t), componentFieldsDefinition(1, componentDoubleField(componentDoubleEntry(0))))
	for _, paths := range [][]vffComponentDoublePath{nil, {{"Depth", nil}}, {{"Other", []string{"PositiveMeasure", "Measure"}}}, {{"Depth", []string{"Measure"}}}, {{"Depth", []string{"PositiveMeasure"}}}, {{"Depth", []string{"PositiveMeasure", "Measure", "double"}}}, {{"Depth", []string{"PositiveMeasure", "Measure"}}, {"Depth", []string{"PositiveMeasure", "Measure"}}}} {
		if got, err := readComponentDoubles(context.Background(), c, paths, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("guessed or partial path accepted")
		}
	}
	for _, mutate := range []func(*vffClassificationLimits, *vffSKCBindingLimits){
		func(c *vffClassificationLimits, _ *vffSKCBindingLimits) { c.depth = 31 },
		func(c *vffClassificationLimits, _ *vffSKCBindingLimits) { c.records = 1 },
		func(c *vffClassificationLimits, _ *vffSKCBindingLimits) { c.bytes = 1 },
		func(_ *vffClassificationLimits, b *vffSKCBindingLimits) { b.visits = 1 },
	} {
		cl, l := componentClassLimits(), skcBindingLimits()
		mutate(&cl, &l)
		if got, err := readComponentDoubles(context.Background(), c, componentDoublePaths(), cl, l); err == nil || got != nil {
			t.Fatal("budget exceeded with partial facts")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readComponentDoubles(ctx, c, componentDoublePaths(), componentClassLimits(), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled facts returned")
	}
}
