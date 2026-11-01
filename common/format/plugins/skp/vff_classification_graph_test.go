package skp

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"testing"
)

func skcGraphSchema(namespace, body string) []byte {
	return []byte(`<x:schema xmlns:x="` + vffXSDNS + `" xmlns:r="urn:r" xmlns:b="urn:b" xmlns:c="urn:c" targetNamespace="` + namespace + `">` + body + `</x:schema>`)
}
func skcGraphContainer(t *testing.T, files map[string][]byte) *vffContainer {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var members []vffTestMember
	for _, name := range names {
		members = append(members, vffTestMember{name, files[name], zip.Deflate})
	}
	return skcControl(t, skcTestZIP(t, false, members...))
}
func skcGraphFiles(t *testing.T, imports, fields string) map[string][]byte {
	f := skcDocumentFiles(t)
	f["Schemas/root.xsd"] = skcGraphSchema("urn:r", imports+`<x:complexType name="Holder"><x:sequence>`+fields+`</x:sequence></x:complexType>`)
	f["Schemas/root.xsd.filter"] = []byte("Holder { F G }")
	f["Schemas/shared.xsd"] = skcGraphSchema("urn:b", `<x:import namespace="urn:c" schemaLocation="extra.xsd"/><x:simpleType name="Label"><x:restriction base="c:Text"/></x:simpleType>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:simpleType name="Text"><x:restriction base="x:string"/></x:simpleType>`)
	return f
}
func skcGraphRead(t *testing.T, files map[string][]byte) []vffSKCFilterBindings {
	t.Helper()
	got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, files), skcBindingLimits())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestVFFSKCValueGraphFollowsOnlyEachOwnersDirectImports(t *testing.T) {
	f := skcGraphFiles(t, `<x:import namespace="urn:b" schemaLocation="shared.xsd"/>`, `<x:element name="F" type="b:Label"/><x:element name="G" type="c:Text"/>`)
	got := skcGraphRead(t, f)
	fields := got[0].rules[0].fields
	value := fields[0].candidates[0].value
	if len(value.issues) != 0 || len(value.references) != 3 || len(value.terminals) != 1 || value.terminals[0].Local != "string" {
		t.Fatalf("cross-file type chain: %#v", value)
	}
	for i, want := range []struct{ from, to string }{{"Schemas/root.xsd", "Schemas/shared.xsd"}, {"Schemas/shared.xsd", "Schemas/extra.xsd"}} {
		r := value.references[i]
		if r.source.member != want.from || r.target == nil || r.target.member != want.to || len(r.via) != 1 || r.via[0].source.member != want.from || r.via[0].target.member != want.to || !r.via[0].namespaceChecked || r.target.sha256 != sha256.Sum256(f[want.to][r.target.begin:r.target.end]) {
			t.Fatal("exact declaration or dependency route lost")
		}
	}
	unproved := fields[1].candidates[0].value
	if len(unproved.issues) == 0 || len(unproved.references) != 1 || unproved.references[0].target != nil {
		t.Fatal("source borrowed a transitive import from another document")
	}
	// Prefix binding at the foreign source, not at the filter root, controls
	// resolution. No matching basename or imported namespace can override it.
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `base="c:Text"`, `xmlns:c="urn:wrong" base="c:Text"`, 1))
	v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) == 0 || v.references[1].name.Space != "urn:wrong" || v.references[1].target != nil {
		t.Fatal("foreign QName source scope ignored")
	}
}

func TestVFFSKCValueGraphUsesVerifiedForwardIncludesAndMemberKeys(t *testing.T) {
	f := skcGraphFiles(t, `<x:include schemaLocation="shared.xsd"/>`, `<x:element name="F" type="r:Label"/><x:element name="G" type="x:float"/>`)
	f["Schemas/shared.xsd"] = skcGraphSchema("urn:r", `<x:simpleType name="Label"><x:restriction base="x:int"/></x:simpleType>`)
	// Deliberately collide a foreign declaration's byte offset with the next
	// local field. A begin-only lookup would overwrite that local field.
	local := strings.Index(string(f["Schemas/root.xsd"]), `<x:element name="G"`)
	foreign := strings.Index(string(f["Schemas/shared.xsd"]), `<x:simpleType`)
	if local <= foreign {
		t.Fatal("authored collision precondition")
	}
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `<x:simpleType`, strings.Repeat(" ", local-foreign)+`<x:simpleType`, 1))
	fields := skcGraphRead(t, f)[0].rules[0].fields
	a, b := fields[0].candidates[0].value, fields[1].candidates[0].value
	if len(a.issues) != 0 || len(b.issues) != 0 || a.references[0].target == nil || a.references[0].target.begin != fields[1].candidates[0].declaration.begin || len(a.references[0].via) != 1 || a.terminals[0].Local != "int" || b.terminals[0].Local != "float" {
		t.Fatal("include route or per-member declaration identity confused")
	}
	if a.references[0].target.member == fields[1].candidates[0].declaration.member {
		t.Fatal("different source members merged")
	}
}

func TestVFFSKCValueGraphResolvesAnExplicitNoNamespaceImport(t *testing.T) {
	f := skcGraphFiles(t, `<x:import schemaLocation="shared.xsd"/>`, `<x:element name="F" type="Label"/><x:element name="G" type="x:int"/>`)
	f["Schemas/shared.xsd"] = []byte(`<x:schema xmlns:x="` + vffXSDNS + `"><x:simpleType name="Label"><x:restriction base="x:long"/></x:simpleType></x:schema>`)
	v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || len(v.terminals) != 1 || v.terminals[0].Local != "long" || v.references[0].target == nil || v.references[0].target.schemaNamespace != "" || len(v.references[0].via) != 1 {
		t.Fatal("explicit no-namespace import lost")
	}
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `name="F" type="Label"`, `name="F" xmlns="urn:wrong" type="Label"`, 1))
	v = skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) == 0 || v.references[0].target != nil {
		t.Fatal("actual default namespace replaced by no-namespace import")
	}
}

func TestVFFSKCValueGraphBindsForeignComplexMembersAndListUnion(t *testing.T) {
	f := skcGraphFiles(t, `<x:import namespace="urn:b" schemaLocation="shared.xsd"/>`, `<x:element name="F" type="b:Value"/><x:element name="G" type="b:Numbers"/>`)
	f["Schemas/shared.xsd"] = skcGraphSchema("urn:b", `<x:import namespace="urn:c" schemaLocation="extra.xsd"/><x:complexType name="Value"><x:sequence><x:element ref="c:Member" minOccurs="3" maxOccurs="4"/></x:sequence><x:attribute ref="c:Tag" fixed="literal value"/></x:complexType><x:simpleType name="Numbers"><x:union memberTypes="c:Words x:int"/></x:simpleType>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:c", `<x:element name="Member" type="x:long"/><x:attribute name="Tag" type="x:string"/><x:simpleType name="Words"><x:list itemType="x:token"/></x:simpleType>`)
	fields := skcGraphRead(t, f)[0].rules[0].fields
	a, b := fields[0].candidates[0].value, fields[1].candidates[0].value
	if len(a.issues) != 0 || len(a.references) != 5 || len(a.terminals) != 2 || a.terminals[0].Local != "long" || a.terminals[1].Local != "string" || len(b.issues) != 0 || len(b.references) != 4 || b.terminals[0].Local != "token" || b.terminals[1].Local != "int" {
		t.Fatal("foreign complex/list/union type chain lost")
	}
	for _, index := range []int{1, 3} {
		r := a.references[index]
		if r.source.member != "Schemas/shared.xsd" || r.target == nil || r.target.member != "Schemas/extra.xsd" || len(r.via) != 1 || r.target.sha256 != sha256.Sum256(f[r.target.member][r.target.begin:r.target.end]) {
			t.Fatal("foreign element/attribute declaration binding")
		}
	}
}

func TestVFFSKCValueGraphSharedIncludeTargetIsNotAnAmbiguousDeclaration(t *testing.T) {
	f := skcGraphFiles(t, `<x:include schemaLocation="shared.xsd"/>`, `<x:element name="F" type="r:Label"/><x:element name="G" type="x:int"/>`)
	f["Schemas/shared.xsd"] = skcGraphSchema("urn:r", `<x:include schemaLocation="extra.xsd"/>`)
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:r", `<x:simpleType name="Label"><x:restriction base="x:int"/></x:simpleType>`)
	v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || v.references[0].target.member != "Schemas/extra.xsd" || len(v.references[0].via) != 2 {
		t.Fatal("verified nested include path lost")
	}
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:include schemaLocation="shared.xsd"/>`, `<x:include schemaLocation="shared.xsd"/><x:include schemaLocation="extra.xsd"/>`, 1))
	v = skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) != 0 || v.references[0].target.member != "Schemas/extra.xsd" || len(v.references[0].via) != 1 {
		t.Fatal("one declaration reached through two paths counted twice")
	}
	// Reverse include assembly is deliberately outside this subset.
	f["Schemas/root.xsd"] = []byte(strings.Replace(string(f["Schemas/root.xsd"]), `<x:complexType name="Holder">`, `<x:simpleType name="Local"><x:restriction base="x:int"/></x:simpleType><x:complexType name="Holder">`, 1))
	f["Schemas/extra.xsd"] = skcGraphSchema("urn:r", `<x:simpleType name="Label"><x:restriction base="r:Local"/></x:simpleType>`)
	v = skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
	if len(v.issues) == 0 || v.references[1].target != nil {
		t.Fatal("reverse include context silently inferred")
	}
}

func TestVFFSKCValueGraphRejectsAmbiguousDeclarationsAndCrossFileCycles(t *testing.T) {
	for _, mode := range []string{"duplicate include", "different type kind", "duplicate import", "cycle"} {
		t.Run(mode, func(t *testing.T) {
			imports := `<x:include schemaLocation="shared.xsd"/><x:include schemaLocation="extra.xsd"/>`
			namespace, fieldType := "urn:r", "r:Label"
			if mode == "duplicate import" || mode == "cycle" {
				imports, namespace, fieldType = `<x:import namespace="urn:b" schemaLocation="shared.xsd"/><x:import namespace="urn:b" schemaLocation="extra.xsd"/>`, "urn:b", "b:Label"
			}
			f := skcGraphFiles(t, imports, `<x:element name="F" type="`+fieldType+`"/><x:element name="G" type="x:int"/>`)
			f["Schemas/shared.xsd"] = skcGraphSchema(namespace, `<x:simpleType name="Label"><x:restriction base="x:int"/></x:simpleType>`)
			f["Schemas/extra.xsd"] = append([]byte(nil), f["Schemas/shared.xsd"]...)
			if mode == "different type kind" {
				f["Schemas/extra.xsd"] = skcGraphSchema(namespace, `<x:complexType name="Label"/>`)
			}
			if mode == "cycle" {
				f["Schemas/root.xsd"] = skcGraphSchema("urn:r", `<x:import namespace="urn:b" schemaLocation="shared.xsd"/><x:simpleType name="Loop"><x:restriction base="b:Label"/></x:simpleType><x:complexType name="Holder"><x:sequence><x:element name="F" type="b:Label"/></x:sequence></x:complexType>`)
				f["Schemas/root.xsd.filter"] = []byte("Holder { F }")
				f["Schemas/shared.xsd"] = skcGraphSchema("urn:b", `<x:import namespace="urn:r" schemaLocation="root.xsd"/><x:simpleType name="Label"><x:restriction base="r:Loop"/></x:simpleType>`)
			}
			if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
				t.Fatal("ambiguous or cyclic cross-file value graph accepted")
			}
		})
	}
}

func TestVFFSKCValueGraphKeepsUnverifiedAndUnrelatedScopesClosed(t *testing.T) {
	for _, mode := range []string{"missing import", "same namespace remote", "wrong namespace", "chameleon", "redefine", "unknown include", "invalid own namespace import"} {
		t.Run(mode, func(t *testing.T) {
			imports := `<x:import namespace="urn:b" schemaLocation="shared.xsd"/>`
			fieldType, namespace := "b:Label", "urn:b"
			switch mode {
			case "missing import":
				imports = ""
			case "same namespace remote":
				imports += `<x:import namespace="urn:b" schemaLocation="https://invalid/another.xsd"/>`
			case "wrong namespace":
				namespace = "urn:wrong"
			case "chameleon":
				imports, fieldType, namespace = `<x:include schemaLocation="shared.xsd"/>`, "r:Label", ""
			case "redefine":
				imports, fieldType, namespace = `<x:redefine schemaLocation="shared.xsd"/>`, "r:Label", "urn:r"
			case "unknown include":
				imports += `<x:include schemaLocation="https://invalid/another.xsd"/>`
			case "invalid own namespace import":
				imports, fieldType, namespace = `<x:import namespace="urn:r" schemaLocation="shared.xsd"/>`, "r:Label", "urn:r"
				fields := `<x:simpleType name="Label"><x:restriction base="x:int"/></x:simpleType>`
				imports = fields + imports
			}
			f := skcGraphFiles(t, imports, `<x:element name="F" type="`+fieldType+`"/><x:element name="G" type="x:int"/>`)
			f["Schemas/shared.xsd"] = skcGraphSchema(namespace, `<x:simpleType name="Label"><x:restriction base="x:int"/></x:simpleType>`)
			if mode == "chameleon" {
				f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), ` targetNamespace=""`, "", 1))
			}
			v := skcGraphRead(t, f)[0].rules[0].fields[0].candidates[0].value
			if len(v.issues) == 0 || v.references[0].target != nil || len(v.references[0].via) != 0 {
				t.Fatal("incomplete scope accepted as a unique type binding")
			}
		})
	}
}

func TestVFFSKCValueGraphCumulativeXMLAndMidBindingCancellation(t *testing.T) {
	f := skcGraphFiles(t, `<x:import namespace="urn:b" schemaLocation="shared.xsd"/>`, `<x:element name="F" type="b:Label"/><x:element name="G" type="x:int"/>`)
	l := skcBindingLimits()
	l.xml.bytes = len(f["Schemas/root.xsd"]) + len(f["Schemas/shared.xsd"]) + len(f["Schemas/extra.xsd"])
	c := skcGraphContainer(t, f)
	if _, err := readVFFClassificationBindings(context.Background(), c, l); err != nil {
		t.Fatal(err)
	}
	l.xml.bytes--
	if got, err := readVFFClassificationBindings(context.Background(), c, l); err == nil || got != nil {
		t.Fatal("schema without filter bypassed cumulative XML budget")
	}
	docs, err := readVFFClassificationDocuments(context.Background(), c, skcDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	g, err := newVFFSKCBindingGraph(context.Background(), docs.packages.packages[0].files, docs.documents[0], &vffXMLBudget{limits: skcBindingLimits().xml}, &vffSKCBindingBudget{10000, 32})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.binders["Schemas/root.xsd"].ctx = &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 6}
	if got, err := g.binders["Schemas/root.xsd"].readFilterBindings(docs.documents[0].filters[0]); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("mid-binding cancellation returned a partial cross-file graph")
	}
}
