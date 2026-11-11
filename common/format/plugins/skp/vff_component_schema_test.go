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

const componentSchemaTitle = "Not a path / title"
const componentSchemaPackage = "classifications/literal%20schema.skc"

func componentSchemaFiles(t *testing.T) map[string][]byte {
	f := skcDocumentFiles(t)
	f["Schemas/root.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" targetNamespace="urn:root"><x:include schemaLocation="shared.xsd"/><x:complexType name="Wall"/></x:schema>`)
	f["Schemas/shared.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `" targetNamespace="urn:root"><x:complexType name="Beam"/></x:schema>`)
	delete(f, "Schemas/root.xsd.filter")
	return f
}
func componentSchemaContainer(t *testing.T, files map[string][]byte, types ...string) *vffContainer {
	c := skcGraphContainer(t, files)
	var defs [][]byte
	for i, typ := range types {
		defs = append(defs, componentClassDefinition(byte(i+1),
			componentClassDictionary("AppliedSchemaTypes", componentClassString(componentSchemaTitle, typ)),
			componentClassDictionary(componentSchemaTitle, componentClassString("SchemaType", "urn:root:"+typ))))
	}
	components := componentClassContainer(defs...).members["model.dat"]
	c.members["model.dat"] = vffRecord("F401", c.members["model.dat"][6:], components[6:])
	return c
}

func TestVFFComponentSchemaUsesCurrentExactTitleNamespaceAndDeclaration(t *testing.T) {
	f := componentSchemaFiles(t)
	c := componentSchemaContainer(t, f, "Wall", "Beam")
	got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, componentSchemaPackage, componentClassLimits(), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.bindings) != 2 || got.document.title != componentSchemaTitle || got.document.rootSchema != "Schemas/root.xsd" || got.document.sha256 != sha256.Sum256(c.members[componentSchemaPackage]) || got.components.modelSHA256 != sha256.Sum256(c.members["model.dat"]) {
		t.Fatalf("current source identities lost: %#v", got)
	}
	for i, binding := range got.bindings {
		w := binding.declaration
		if binding.component != i || w.kind != "complexType" || w.name != got.components.bindings[i].schemaType || w.schemaNamespace != "urn:root" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatalf("type witness differs: %#v", binding)
		}
	}
	if len(got.bindings[0].via) != 0 || len(got.bindings[1].via) != 1 || !got.bindings[1].via[0].namespaceChecked || got.bindings[1].via[0].target.member != "Schemas/shared.xsd" {
		t.Fatal("direct and exact included declarations confused")
	}
	for _, d := range got.document.documents {
		if d.sha256 != sha256.Sum256(f[d.member]) {
			t.Fatal("document source identity lost")
		}
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("component identity opened composition admission")
	}
}

func TestVFFComponentSchemaRejectsGuessedAndAmbiguousBindings(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"title mismatch": func(f map[string][]byte) {
			f["documentProperties.xml"] = []byte(strings.Replace(string(f["documentProperties.xml"]), componentSchemaTitle, componentSchemaTitle+" ", 1))
		},
		"namespace mismatch": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.ReplaceAll(string(f["Schemas/root.xsd"]), "urn:root", "urn:other"))
		},
		"element is not type": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:complexType name="Wall"/>`, `<x:element name="Wall"/>`, 1))
		},
		"simple type is not complex": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:complexType name="Wall"/>`, `<x:simpleType name="Wall"><x:restriction base="x:string"/></x:simpleType>`, 1))
		},
		"unreferenced file cannot supply type": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:include schemaLocation="shared.xsd"/>`, "", 1))
		},
		"missing type": func(f map[string][]byte) {
			f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `name="Beam"`, `name="Other"`, 1))
		},
		"duplicate included type": func(f map[string][]byte) {
			f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `</x:schema>`, `<x:complexType name="Wall"/></x:schema>`, 1))
		},
		"unresolved dependency is not complete": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `shared.xsd`, `https://example.invalid/shared.xsd`, 1))
		},
		"wrong namespace include": func(f map[string][]byte) {
			f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `urn:root`, `urn:foreign`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := componentSchemaFiles(t)
			mutate(f)
			if got, err := readVFFComponentSchemaBindings(context.Background(), componentSchemaContainer(t, f, "Wall", "Beam"), componentSchemaTitle, componentSchemaPackage, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("unproved or partial component schema binding returned")
			}
		})
	}
	c := componentSchemaContainer(t, componentSchemaFiles(t), "Wall")
	for _, member := range []string{"", "literal%20schema.skc", "classifications/literal schema.skc", "other.skc"} {
		if got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, member, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("package identity guessed")
		}
	}
	c.members["model.dat"] = []byte(strings.Replace(string(c.members["model.dat"]), "urn:root:Wall", "urn:fake:Wall", 1))
	if got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, componentSchemaPackage, componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("same local name hid wrong current namespace")
	}
}

func TestVFFComponentSchemaBoundsAndCancellation(t *testing.T) {
	c := componentSchemaContainer(t, componentSchemaFiles(t), "Wall", "Beam")
	for name, change := range map[string]func(*vffSKCBindingLimits){
		"missing":         func(l *vffSKCBindingLimits) { *l = vffSKCBindingLimits{} },
		"document limits": func(l *vffSKCBindingLimits) { l.documents.xml.nodes = 0 },
		"document bytes":  func(l *vffSKCBindingLimits) { l.documents.xml.bytes = 1 },
		"schema bytes":    func(l *vffSKCBindingLimits) { l.xml.bytes = 1 },
		"schema visits":   func(l *vffSKCBindingLimits) { l.visits = 1 },
		"package bytes":   func(l *vffSKCBindingLimits) { l.documents.packages.readBytes = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := skcBindingLimits()
			change(&limits)
			if got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, componentSchemaPackage, componentClassLimits(), limits); err == nil || got != nil {
				t.Fatal("budget bypass or partial result")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFComponentSchemaBindings(ctx, c, componentSchemaTitle, componentSchemaPackage, componentClassLimits(), skcBindingLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled schema binding succeeded")
	}
}

func TestVFFComponentSchemaSelectsOnlyExplicitRegisteredPackage(t *testing.T) {
	c := componentSchemaContainer(t, componentSchemaFiles(t), "Wall")
	refs, err := readVFFClassificationReferences(context.Background(), c, componentClassLimits())
	if err != nil {
		t.Fatal(err)
	}
	ref := refs.entries[0]
	model := c.members["model.dat"]
	// Keep the first registration and add an exact but unsupported package.
	first := model[ref.entryOffset:]
	first = first[:6+int(uint32(first[2])|uint32(first[3])<<8|uint32(first[4])<<16|uint32(first[5])<<24)]
	other := vffRecord("8467", vffRecord("8567", []byte("opaque")), vffRecord("8667", []byte(componentSchemaTitle)), vffRecord("8767", []byte("classifications/other.skc")))
	group := vffRecord("0502", first)
	model = bytes.Replace(model, group, vffRecord("0502", first, other), 1)
	c.members["model.dat"] = vffRecord("F401", model[6:])
	c.members["classifications/other.skc"] = []byte("unsupported bytes must stay unproved")
	got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, componentSchemaPackage, componentClassLimits(), skcBindingLimits())
	if err != nil || len(got.bindings) != 1 || len(got.packageReferences.entries) != 2 {
		t.Fatalf("explicit selected-package proof: %v", err)
	}
	if _, err := readVFFClassificationPackages(context.Background(), c, skcBindingLimits().documents.packages); err == nil {
		t.Fatal("all-package reader no longer rejects unsupported bytes")
	}
	if got, err := readVFFComponentSchemaBindings(context.Background(), c, componentSchemaTitle, "classifications/other.skc", componentClassLimits(), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("same title borrowed a different package's schema")
	}
}
