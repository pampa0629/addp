package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
)

func TestVFFXMLASCIIEncodingPreservesSource(t *testing.T) {
	for _, encoding := range []string{"ASCII", "ascii", "US-ASCII", "uS-aScIi"} {
		t.Run(encoding, func(t *testing.T) {
			declaration := `<?xml version="1.0" encoding="` + encoding + `"?>`
			raw := []byte(declaration + `<root xmlns="urn:test" a="&#233;"><child>&#x4E2D;</child></root>`)
			original := bytes.Clone(raw)
			budget := &vffXMLBudget{limits: skcDocumentLimits().xml}
			got, err := readVFFXML(context.Background(), raw, budget, map[string]bool{"urn:test": true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.begin != len(declaration) || got.end != len(raw) || got.attrs["a"] != "é" || len(got.children) != 1 || got.children[0].text != "中" {
				t.Fatalf("source positions or XML character references changed: %#v", got)
			}
			child := got.children[0]
			if string(raw[child.begin:child.end]) != `<child>&#x4E2D;</child>` || budget.bytes != len(raw) || !bytes.Equal(raw, original) {
				t.Fatal("source bytes, child offsets or cumulative byte budget changed")
			}
		})
	}
	for _, declaration := range []string{"", `<?xml version="1.0" encoding="UTF-8"?>`} {
		raw := []byte(declaration + `<root xmlns="urn:test">中文</root>`)
		got, err := readVFFXML(context.Background(), raw, &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil)
		if err != nil || got.text != "中文" {
			t.Fatalf("UTF-8 path changed: %v", err)
		}
	}
}

func TestVFFXMLASCIIEncodingRejectsInvalidSource(t *testing.T) {
	declaration := `<?xml version="1.0" encoding="ASCII"?>`
	for name, document := range map[string]string{
		"text":                  `<root xmlns="urn:test">é</root>`,
		"attribute":             `<root xmlns="urn:test" a="é"/>`,
		"comment":               `<!--é--><root xmlns="urn:test"/>`,
		"after root":            `<root xmlns="urn:test"/><!--é-->`,
		"invalid byte":          "<root xmlns=\"urn:test\">\xff</root>",
		"invalid XML character": "<root xmlns=\"urn:test\">\x00</root>",
		"DTD":                   `<!DOCTYPE root [<!ENTITY a "x">]><root xmlns="urn:test">&a;</root>`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readVFFXML(context.Background(), []byte(declaration+document), &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil)
			if err == nil || got != nil {
				t.Fatal("invalid ASCII-declared XML accepted or partial facts returned")
			}
		})
	}
	for _, encoding := range []string{"ISO-8859-1", "UTF-16", "windows-1252", "ASCII-extra"} {
		raw := []byte(`<?xml version="1.0" encoding="` + encoding + `"?><root xmlns="urn:test"/>`)
		got, err := readVFFXML(context.Background(), raw, &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil)
		if err == nil || got != nil {
			t.Fatalf("unsupported encoding accepted: %s", encoding)
		}
	}
}

func TestVFFXMLASCIIEncodingRetainsBudgetsAndCancellation(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="ASCII"?><root xmlns="urn:test">x</root>`)
	for _, change := range []func(*vffXMLBudget){
		func(b *vffXMLBudget) { b.bytes = b.limits.bytes - len(raw) + 1 },
		func(b *vffXMLBudget) { b.nodes = b.limits.nodes },
		func(b *vffXMLBudget) { b.text = b.limits.text },
	} {
		budget := &vffXMLBudget{limits: skcDocumentLimits().xml}
		change(budget)
		if got, err := readVFFXML(context.Background(), raw, budget, map[string]bool{"urn:test": true}, nil); err == nil || got != nil {
			t.Fatal("ASCII bypassed cumulative budget")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFXML(ctx, raw, &vffXMLBudget{limits: skcDocumentLimits().xml}, map[string]bool{"urn:test": true}, nil); err != context.Canceled || got != nil {
		t.Fatalf("cancellation changed: %v", err)
	}
}

func TestVFFClassificationDocumentsASCIISchemaKeepsSourceEvidence(t *testing.T) {
	files := skcDocumentFiles(t)
	raw := append([]byte(`<?xml version="1.0" encoding="ASCII"?>`), files["Schemas/root.xsd"]...)
	files["Schemas/root.xsd"] = raw
	before := bytes.Clone(raw)
	got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	doc := got.documents[0]
	root := doc.schemas[0]
	if root.member != "Schemas/root.xsd" || root.sha256 != sha256.Sum256(before) || len(root.dependencies) != 3 || len(root.opaque) != 1 || len(doc.unexplained) != 2 {
		t.Fatal("ASCII declaration changed schema identity or unproved resources")
	}
	for _, dep := range root.dependencies {
		if !bytes.Contains(raw[dep.begin:dep.end], []byte("xs:"+dep.kind)) {
			t.Fatal("schema reference offsets changed")
		}
	}
	opaque := root.opaque[0]
	if opaque.sha256 != sha256.Sum256(raw[opaque.begin:opaque.end]) || !bytes.Equal(raw, before) {
		t.Fatal("opaque source bytes or hash changed")
	}
}
