package skp

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/addp/common/format"
)

func componentClassString(key, value string) []byte {
	return append(vffRecord("B636", []byte(key)), vffRecord("A438", vffRecord("AD38", []byte(value)))...)
}
func componentClassDictionary(name string, entries ...[]byte) []byte {
	return vffRecord("B336", vffRecord("B036", []byte("unexplained nested dictionary")),
		vffRecord("B436", []byte(name)), vffRecord("B536", entries...), vffRecord("B736", []byte{0, 0, 0, 0}))
}
func componentClassDefinition(id byte, dictionaries ...[]byte) []byte {
	guid := bytes.Repeat([]byte{id}, 16)
	return vffRecord("7C15", vffRecord("8813", vffRecord("D007", vffRecord("DC05",
		vffRecord("DE05", []byte{id}), vffRecord("DD05", vffRecord("B136", vffRecord("B236", dictionaries...)))))),
		vffRecord("7D15", guid), vffRecord("7E15", []byte("component")), vffRecord("7F15", []byte("description")))
}
func componentClassContainer(definitions ...[]byte) *vffContainer {
	return &vffContainer{members: map[string][]byte{"model.dat": vffRecord("F401",
		vffRecord("0902", []byte("opaque")),
		vffRecord("F901", vffRecord("7017", vffRecord("7117", definitions...))))}}
}
func componentClassLimits() vffClassificationLimits {
	return vffClassificationLimits{bytes: 1 << 20, records: 10000, depth: 32}
}
func TestVFFComponentClassificationBindings(t *testing.T) {
	mapping := componentClassDictionary("AppliedSchemaTypes", componentClassString("schema", "IfcWall"))
	declaration := componentClassDictionary("schema", componentClassString("SchemaType", "urn:test:IfcWall"))
	c := componentClassContainer(componentClassDefinition(1, mapping, declaration), componentClassDefinition(2))
	result, err := readVFFComponentClassifications(context.Background(), c, "schema", componentClassLimits())
	if err != nil {
		t.Fatal(err)
	}
	if result.definitions != 2 || len(result.bindings) != 1 || len(result.opaque) == 0 {
		t.Fatalf("unexpected facts: %#v", result)
	}
	got := result.bindings[0]
	if got.entityID != 1 || got.component != ([16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}) || got.schema != "schema" || got.schemaType != "IfcWall" || got.declaredType != "urn:test:IfcWall" {
		t.Fatalf("identity or type lost: %#v", got)
	}
	if got.mappingOffset <= got.definitionOffset || got.declarationOffset <= got.mappingOffset {
		t.Fatal("source locations lost")
	}
	for _, change := range []func() *vffContainer{
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, mapping, declaration), componentClassDefinition(1))
		},
		func() *vffContainer { return componentClassContainer(componentClassDefinition(1, mapping)) },
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, mapping), componentClassDefinition(2, declaration))
		},
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, mapping, declaration, declaration))
		},
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, componentClassDictionary("AppliedSchemaTypes", componentClassString("schema", "IfcWall"), componentClassString("schema", "IfcWall")), declaration))
		},
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, mapping, componentClassDictionary("schema", componentClassString("SchemaType", "urn:test:IfcDoor"))))
		},
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, mapping, componentClassDictionary("schema", componentClassString("SchemaType", "IfcWall"))))
		},
		func() *vffContainer {
			return componentClassContainer(componentClassDefinition(1, componentClassDictionary("AppliedSchemaTypes", append(vffRecord("B636", []byte("schema")), vffRecord("A438", vffRecord("A738", []byte{1, 0, 0, 0}))...)), declaration))
		},
	} {
		if got, err := readVFFComponentClassifications(context.Background(), change(), "schema", componentClassLimits()); err == nil || got != nil {
			t.Fatal("ambiguous binding accepted")
		}
	}
	for _, broken := range [][]byte{
		bytes.Replace(componentClassDefinition(2), vffRecord("DE05", []byte{2}), vffRecord("DE05", []byte{1}), 1),
		bytes.Replace(componentClassDefinition(2), vffRecord("7D15", bytes.Repeat([]byte{2}, 16)), vffRecord("7D15", bytes.Repeat([]byte{1}, 16)), 1),
		componentClassDefinition(2, componentClassDictionary("AppliedSchemaTypes", componentClassString("schema", "IfcWall")),
			componentClassDictionary("schema", componentClassString("SchemaType", "urn:test:IfcWall"), componentClassString("SchemaType", "urn:test:IfcWall"))),
		componentClassDefinition(2, componentClassDictionary("AppliedSchemaTypes", componentClassString("schema", string([]byte{0xff}))), declaration),
	} {
		container := componentClassContainer(componentClassDefinition(1, mapping, declaration), broken)
		if got, err := readVFFComponentClassifications(context.Background(), container, "schema", componentClassLimits()); err == nil || got != nil {
			t.Fatal("invalid identity or declaration accepted")
		}
	}
	for _, limits := range []vffClassificationLimits{{}, {bytes: 1, records: 10000, depth: 32}, {bytes: 1 << 20, records: 2, depth: 32}, {bytes: 1 << 20, records: 10000, depth: 5}} {
		if got, err := readVFFComponentClassifications(context.Background(), c, "schema", limits); err == nil || got != nil {
			t.Fatal("budget accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFComponentClassifications(ctx, c, "schema", componentClassLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled read succeeded")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("composition admission opened")
	}
}
