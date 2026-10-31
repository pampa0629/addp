package skp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func skcDocumentLimits() vffClassificationDocumentLimits {
	return vffClassificationDocumentLimits{packages: skcTestLimits(), xml: vffXMLLimits{bytes: 1 << 20, nodes: 10000, depth: 32, attributes: 10000, text: 1 << 20}, images: vffImageLimits{images: 10, bytes: 1 << 20, pixels: 10000}, dependencies: 100}
}

func skcDocumentFiles(t *testing.T) map[string][]byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 0})
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 90, B: 10, A: 128})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{
		"document.xml":               []byte(`<classificationDocument xmlns="` + vffClassificationNS + `" xmlns:xsi="` + vffXSINS + `" xsi:schemaLocation="urn:example https://example.invalid/never-fetch.xsd"><Classification xsdFile="Schemas/root.xsd"/></classificationDocument>`),
		"documentProperties.xml":     []byte(`<documentProperties xmlns="` + vffDocumentNS + `" xmlns:dp="` + vffDocumentNS + `"><title>Not a path / title</title><description>https://example.invalid/not-a-resource</description><creator>creator</creator><keywords/><lastModifiedBy/><revision>1</revision><created>2026-10-31T12:00:00</created><modified>2026-10-31T12:00:00</modified><thumbnail>images/literal%20thumb.png</thumbnail><generator dp:name="Classification" dp:version="1"/></documentProperties>`),
		"references.xml":             []byte(`<references xmlns="` + vffReferencesNS + `"/>`),
		"Schemas/root.xsd":           []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" targetNamespace="urn:root"><xs:include schemaLocation="shared.xsd"/><xs:import namespace="urn:remote" schemaLocation="https://example.invalid/shared.xsd"/><xs:import namespace="urn:absent"/><xs:element name="opaque"><xs:annotation><xs:documentation source="http://example.invalid/documentation">text</xs:documentation></xs:annotation></xs:element></xs:schema>`),
		"Schemas/shared.xsd":         []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:include schemaLocation="root.xsd"/><xs:include schemaLocation="root.xsd"/></xs:schema>`),
		"Schemas/root.xsd.filter":    []byte("// opaque filter bytes\nIfcWall { Name }"),
		"images/literal%20thumb.png": encoded.Bytes(),
		// Same bytes but a distinct path must never replace the declared member.
		"images/literal thumb.png": bytes.Clone(encoded.Bytes()),
		"unbound.xsd":              []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"/>`),
	}
}

func skcDocumentContainer(t *testing.T, files map[string][]byte) *vffContainer {
	t.Helper()
	var members []vffTestMember
	// Directory order cannot change document interpretation.
	for _, name := range []string{"images/literal thumb.png", "unbound.xsd", "documentProperties.xml", "Schemas/shared.xsd", "references.xml", "Schemas/root.xsd.filter", "images/literal%20thumb.png", "Schemas/root.xsd", "document.xml"} {
		if data, ok := files[name]; ok {
			members = append(members, vffTestMember{name, data, zip.Deflate})
		}
	}
	return skcControl(t, skcTestZIP(t, false, members...))
}

func TestVFFClassificationDocumentsExactResourcesAndUnresolvedFacts(t *testing.T) {
	files := skcDocumentFiles(t)
	c := skcDocumentContainer(t, files)
	before := bytes.Clone(c.members["classifications/literal%20schema.skc"])
	got, err := readVFFClassificationDocuments(context.Background(), c, skcDocumentLimits())
	if err != nil || len(got.documents) != 1 {
		t.Fatalf("documents: %#v %v", got, err)
	}
	fact := got.documents[0]
	if fact.member != "classifications/literal%20schema.skc" || fact.sha256 != sha256.Sum256(before) || fact.rootSchema != "Schemas/root.xsd" || len(fact.documents) != 3 || len(fact.schemas) != 2 || len(fact.filters) != 1 || len(fact.unexplained) != 2 {
		t.Fatalf("exact identities/counts: %#v", fact)
	}
	if fact.documents[0].hints[0] != (vffSKCSchemaHint{"urn:example", "https://example.invalid/never-fetch.xsd"}) || fact.documents[0].sha256 != sha256.Sum256(files["document.xml"]) {
		t.Fatal("hint or document identity changed")
	}
	if fact.unexplained[0].member != "images/literal thumb.png" || fact.unexplained[1].member != "unbound.xsd" || fact.filters[0].member != "Schemas/root.xsd.filter" {
		t.Fatal("unreferenced resource inferred or filter semantics lost")
	}
	image := fact.thumbnail
	if image.member != "images/literal%20thumb.png" || image.sourceSHA256 != sha256.Sum256(files[image.member]) || image.width != 2 || image.height != 1 || image.minAlpha != 0 || image.maxAlpha != 128*257 {
		t.Fatal("exact image role/alpha lost")
	}
	wantPixels := []byte{1, 1, 2, 2, 3, 3, 0, 0, 255, 255, 90, 90, 10, 10, 128, 128}
	if image.pixelsSHA256 != sha256.Sum256(wantPixels) {
		t.Fatal("transparent RGB or straight RGBA16 pixels changed")
	}
	root := fact.schemas[0]
	if root.targetNamespace != "urn:root" || len(root.dependencies) != 3 || len(root.opaque) != 1 || len(fact.schemas[1].dependencies) != 2 {
		t.Fatal("schema declarations, cycle or opaque subtree lost")
	}
	if root.dependencies[0].member != "Schemas/shared.xsd" || root.dependencies[0].sha256 != sha256.Sum256(files["Schemas/shared.xsd"]) || root.dependencies[1].member != "" || root.dependencies[2].member != "" {
		t.Fatal("external/absent import silently resolved by matching basename")
	}
	for _, dep := range root.dependencies {
		fragment := files[root.member][dep.begin:dep.end]
		if !bytes.Contains(fragment, []byte("xs:"+dep.kind)) {
			t.Fatal("dependency source offsets lost")
		}
	}
	opaque := root.opaque[0]
	if opaque.sha256 != sha256.Sum256(files[root.member][opaque.begin:opaque.end]) {
		t.Fatal("opaque source range lost")
	}
	if !bytes.Equal(before, c.members[fact.member]) {
		t.Fatal("source changed")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("local XML facts opened composition admission")
	}
}

func TestVFFClassificationDocumentsRejectAmbiguityAndCorruption(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"missing document":        func(f map[string][]byte) { delete(f, "document.xml") },
		"missing schema":          func(f map[string][]byte) { delete(f, "Schemas/root.xsd") },
		"missing exact thumbnail": func(f map[string][]byte) { delete(f, "images/literal%20thumb.png") },
		"missing exact include":   func(f map[string][]byte) { delete(f, "Schemas/shared.xsd") },
		"unknown declaration": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("xsdFile="), []byte("other="))
		},
		"duplicate classification": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("</classificationDocument>"), []byte(`<Classification xsdFile="Schemas/root.xsd"/></classificationDocument>`))
		},
		"wrong namespace": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte(vffClassificationNS), []byte("urn:wrong"))
		},
		"unqualified schema hint": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("xsi:schemaLocation="), []byte("schemaLocation="))
		},
		"incomplete hint pair": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("urn:example https://example.invalid/never-fetch.xsd"), []byte("urn:example"))
		},
		"duplicate hint namespace": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("urn:example https://example.invalid/never-fetch.xsd"), []byte("urn:a a urn:a b"))
		},
		"DTD": func(f map[string][]byte) {
			f["document.xml"] = append([]byte(`<!DOCTYPE x SYSTEM "file:///never-read">`), f["document.xml"]...)
		},
		"processing instruction": func(f map[string][]byte) {
			f["document.xml"] = append([]byte(`<?xml-stylesheet href="http://never-fetch"?>`), f["document.xml"]...)
		},
		"duplicate attribute": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("xsdFile="), []byte(`xsdFile="Schemas/root.xsd" xsdFile=`))
		},
		"trailing root": func(f map[string][]byte) { f["document.xml"] = append(f["document.xml"], []byte(`<extra/>`)...) },
		"mixed content": func(f map[string][]byte) {
			f["document.xml"] = bytes.ReplaceAll(f["document.xml"], []byte("<Classification"), []byte("text<Classification"))
		},
		"duplicate thumbnail": func(f map[string][]byte) {
			f["documentProperties.xml"] = bytes.ReplaceAll(f["documentProperties.xml"], []byte("</thumbnail>"), []byte(`</thumbnail><thumbnail>images/literal thumb.png</thumbnail>`))
		},
		"empty revision": func(f map[string][]byte) {
			f["documentProperties.xml"] = bytes.ReplaceAll(f["documentProperties.xml"], []byte("<revision>1"), []byte("<revision>"))
		},
		"bad timestamp": func(f map[string][]byte) {
			f["documentProperties.xml"] = bytes.ReplaceAll(f["documentProperties.xml"], []byte("2026-10-31T12:00:00"), []byte("not-a-date"))
		},
		"unqualified generator attr": func(f map[string][]byte) {
			f["documentProperties.xml"] = bytes.ReplaceAll(f["documentProperties.xml"], []byte("dp:name="), []byte("name="))
		},
		"unknown generator attr": func(f map[string][]byte) {
			f["documentProperties.xml"] = bytes.ReplaceAll(f["documentProperties.xml"], []byte("dp:name="), []byte(`other="ignored" dp:name=`))
		},
		"nonempty references": func(f map[string][]byte) {
			f["references.xml"] = []byte(`<references xmlns="` + vffReferencesNS + `"><reference href="http://never-fetch"/></references>`)
		},
		"corrupt thumbnail": func(f map[string][]byte) { f["images/literal%20thumb.png"][40] ^= 1 },
		"trailing image": func(f map[string][]byte) {
			f["images/literal%20thumb.png"] = append(f["images/literal%20thumb.png"], 0)
		},
		"unknown dependency attr": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = bytes.ReplaceAll(f["Schemas/root.xsd"], []byte("schemaLocation=\"shared.xsd\""), []byte(`schemaLocation="shared.xsd" href="not-interpreted"`))
		},
		"missing include location": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = bytes.ReplaceAll(f["Schemas/root.xsd"], []byte(`schemaLocation="shared.xsd"`), nil)
		},
		"wrong schema namespace": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = bytes.ReplaceAll(f["Schemas/root.xsd"], []byte(vffXSDNS), []byte("urn:wrong"))
		},
		"schema DTD": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = append([]byte(`<!DOCTYPE schema []>`), f["Schemas/root.xsd"]...)
		},
		"schema XML base": func(f map[string][]byte) {
			f["Schemas/root.xsd"] = bytes.ReplaceAll(f["Schemas/root.xsd"], []byte("targetNamespace="), []byte(`xml:base="https://never-fetch/" targetNamespace=`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			files := skcDocumentFiles(t)
			mutate(files)
			if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits()); err == nil || got != nil {
				t.Fatalf("invalid document/resource accepted: %#v %v", got, err)
			}
		})
	}
	for _, reference := range []string{"../root.xsd", "/Schemas/root.xsd", "https://invalid/root.xsd", "Schemas\\root.xsd", "Schemas/./root.xsd", "Schemas/%72oot.xsd"} {
		files := skcDocumentFiles(t)
		files["document.xml"] = bytes.ReplaceAll(files["document.xml"], []byte("Schemas/root.xsd"), []byte(reference))
		if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits()); err == nil || got != nil {
			t.Fatalf("noncanonical root or decoded fallback accepted: %s", reference)
		}
	}
}

func TestVFFClassificationDocumentsLocalImportAndRedefinitionAreOnlyFacts(t *testing.T) {
	files := skcDocumentFiles(t)
	files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:import namespace="urn:local" schemaLocation="shared.xsd"/><xs:redefine schemaLocation="shared.xsd"><xs:complexType name="unproved"/></xs:redefine></xs:schema>`)
	got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
	if err != nil || len(got.documents[0].schemas) != 2 {
		t.Fatalf("local dependency facts: %v", err)
	}
	schema := got.documents[0].schemas[0]
	if len(schema.dependencies) != 2 || schema.dependencies[0].kind != "import" || schema.dependencies[0].namespace != "urn:local" || schema.dependencies[1].kind != "redefine" || len(schema.opaque) != 1 {
		t.Fatal("import/redefine identity or uninterpreted redefinition lost")
	}
	for _, d := range schema.dependencies {
		if d.member != "Schemas/shared.xsd" || d.sha256 != sha256.Sum256(files[d.member]) {
			t.Fatal("exact shared target lost")
		}
	}
}

func TestVFFClassificationDocumentsCumulativeBudgetsAndFreshSources(t *testing.T) {
	files := skcDocumentFiles(t)
	c := skcDocumentContainer(t, files)
	limits := skcDocumentLimits()
	limits.xml.bytes = 0
	for _, name := range []string{"document.xml", "documentProperties.xml", "references.xml", "Schemas/root.xsd", "Schemas/shared.xsd"} {
		limits.xml.bytes += len(files[name])
	}
	limits.dependencies, limits.images.images, limits.images.bytes, limits.images.pixels = 5, 1, len(files["images/literal%20thumb.png"]), 2
	if _, err := readVFFClassificationDocuments(context.Background(), c, limits); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*vffClassificationDocumentLimits){
		"XML bytes":      func(l *vffClassificationDocumentLimits) { l.xml.bytes-- },
		"XML nodes":      func(l *vffClassificationDocumentLimits) { l.xml.nodes = 1 },
		"XML depth":      func(l *vffClassificationDocumentLimits) { l.xml.depth = 1 },
		"XML attributes": func(l *vffClassificationDocumentLimits) { l.xml.attributes = 1 },
		"XML text":       func(l *vffClassificationDocumentLimits) { l.xml.text = 1 },
		"dependencies":   func(l *vffClassificationDocumentLimits) { l.dependencies-- },
		"image bytes":    func(l *vffClassificationDocumentLimits) { l.images.bytes-- },
		"pixels":         func(l *vffClassificationDocumentLimits) { l.images.pixels-- },
	} {
		t.Run(name, func(t *testing.T) {
			copy := limits
			mutate(&copy)
			if got, err := readVFFClassificationDocuments(context.Background(), c, copy); err == nil || got != nil {
				t.Fatal("cumulative limit bypass")
			}
		})
	}
	// A new exact package with equal bytes consumes XML and image budgets again.
	// Failure after the first package must not expose a partial result.
	c = vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		raw := skcDocumentContainer(t, files).members["classifications/literal%20schema.skc"]
		c.members["classifications/literal%20schema.skc"] = raw
		c.members["classifications/second.skc"] = bytes.Clone(raw)
		p["entries"] = append(vffRecord("8467", p["directory"], p["name"], p["path"]), vffRecord("8467", p["directory"], p["name"], vffRecord("8767", []byte("classifications/second.skc")))...)
	})
	for name, mutate := range map[string]func(*vffClassificationDocumentLimits){
		"XML cumulative":          func(l *vffClassificationDocumentLimits) { l.xml.bytes = limits.xml.bytes },
		"images cumulative":       func(l *vffClassificationDocumentLimits) { l.images.images = 1 },
		"pixels cumulative":       func(l *vffClassificationDocumentLimits) { l.images.pixels = 3 },
		"dependencies cumulative": func(l *vffClassificationDocumentLimits) { l.dependencies = 9 },
	} {
		t.Run(name, func(t *testing.T) {
			l := skcDocumentLimits()
			mutate(&l)
			if got, err := readVFFClassificationDocuments(context.Background(), c, l); err == nil || got != nil {
				t.Fatal("distinct package budget or atomicity lost")
			}
		})
	}
	full := skcDocumentLimits()
	if got, err := readVFFClassificationDocuments(context.Background(), c, full); err != nil || len(got.documents) != 2 {
		t.Fatalf("two current packages: %#v %v", got, err)
	}
	delete(c.members, "classifications/second.skc")
	if got, err := readVFFClassificationDocuments(context.Background(), c, full); err == nil || got != nil {
		t.Fatal("prior result substituted for current missing member")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationDocuments(ctx, c, full); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("cancelled read: %#v %v", got, err)
	}
	if got, err := readVFFClassificationDocuments(context.Background(), nil, full); err == nil || got != nil {
		t.Fatal("nil source accepted")
	}
	empty := vffClassificationControl(func(p map[string][]byte, _ *vffContainer) { p["entries"] = nil })
	if got, err := readVFFClassificationDocuments(context.Background(), empty, full); err != nil || len(got.documents) != 0 {
		t.Fatalf("explicit empty classification list: %#v %v", got, err)
	}
}

func TestVFFClassificationXMLQualifiedAttributesRemainDistinct(t *testing.T) {
	raw := []byte(`<root xmlns="urn:test" xmlns:a="urn:attr" name="plain" a:name="qualified"/>`)
	limits := skcDocumentLimits().xml
	got, err := readVFFXML(context.Background(), raw, &vffXMLBudget{limits: limits}, map[string]bool{"urn:test": true}, map[xml.Name]bool{{Space: "urn:attr", Local: "name"}: true})
	if err != nil || got.attrs["name"] != "plain" || got.attrs[vffXMLAttributeKey("urn:attr", "name")] != "qualified" || len(got.attrs) != 2 {
		t.Fatalf("expanded attribute identity: %#v %v", got, err)
	}
	if _, err := readVFFXML(context.Background(), raw, &vffXMLBudget{limits: limits}, map[string]bool{"urn:test": true}, nil); err == nil {
		t.Fatal("qualified attributes enabled for existing material/style caller")
	}
	duplicate := []byte(`<root xmlns="urn:test" xmlns:a="urn:attr" xmlns:b="urn:attr" a:name="one" b:name="two"/>`)
	if _, err := readVFFXML(context.Background(), duplicate, &vffXMLBudget{limits: limits}, map[string]bool{"urn:test": true}, map[xml.Name]bool{{Space: "urn:attr", Local: "name"}: true}); err == nil {
		t.Fatal("duplicate expanded attribute accepted through different prefixes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 8}
	files := skcDocumentFiles(t)
	deps := 100
	if got, err := readVFFSKCSchema(controlled, "Schemas/root.xsd", files, &vffXMLBudget{limits: limits}, &deps); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-XSD cancellation returned partial facts: %#v %v", got, err)
	}
	for _, location := range []string{"../shared.xsd", "https://example.invalid/shared.xsd", "file:///shared.xsd", "shared%2Exsd", "shared.xsd?query", "shared.xsd#fragment"} {
		files := skcDocumentFiles(t)
		files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:include schemaLocation="` + location + `"/></xs:schema>`)
		got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
		if err != nil || len(got.documents[0].schemas) != 1 || got.documents[0].schemas[0].dependencies[0].member != "" || !strings.Contains(got.documents[0].schemas[0].dependencies[0].location, location) {
			t.Fatalf("unresolved location guessed/decoded: %s: %v", location, err)
		}
	}
}
