package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func vffMaterialLimits() vffXMLLimits {
	return vffXMLLimits{bytes: 1 << 20, nodes: 1000, depth: 24, attributes: 2000, text: 1 << 20}
}
func vffMaterialControl() (*vffContainer, string) {
	doc := `<materialDocument xmlns="` + vffMaterialNS + `"><material name="Display Front" type="1" hasTexture="1" colorRed="255" colorGreen="255" colorBlue="255"><texture textureFilename="/historical/not-an-input.png" xScale="2" yScale="2"><images><image id="1" path="./pixels.png" file_name="historical.png"/></images></texture><pbrMR><enable_metalness>0</enable_metalness><enable_roughness>0</enable_roughness><enable_normal>0</enable_normal><enable_occlusion>0</enable_occlusion></pbrMR></material></materialDocument>`
	return &vffContainer{members: map[string][]byte{"model.dat": []byte("not proved"), "materials/Front/material.xml": []byte(doc), "materials/Front/pixels.png": []byte("\x89PNG\r\n\x1a\nimage-decode-not-proved"), "styles/opaque/style.xml": []byte("not proved")}}, doc
}
func TestVFFMaterialExactDeclarationOnly(t *testing.T) {
	container, _ := vffMaterialControl()
	facts, err := readVFFMaterials(context.Background(), container, vffMaterialLimits())
	if err != nil || len(facts) != 1 {
		t.Fatalf("declaration: %#v %v", facts, err)
	}
	want := sha256.Sum256(container.members["materials/Front/pixels.png"])
	if facts[0].image != "materials/Front/pixels.png" || facts[0].name != "Display Front" || facts[0].imageSHA256 != want {
		t.Fatal("image identity differs")
	}
	// The image signature is sufficient for declaration facts only. This layer
	// must not pretend the intentionally undecodable bytes prove image validity.
	for _, reference := range []string{"materials/Front/pixels.png", "pixels.png"} {
		c, doc := vffMaterialControl()
		c.members["materials/Front/material.xml"] = []byte(strings.ReplaceAll(doc, "./pixels.png", reference))
		if _, err := readVFFMaterials(context.Background(), c, vffMaterialLimits()); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := vffMaterialControl()
	c.members["materials/Plain/material.xml"] = []byte(`<materialDocument xmlns="` + vffMaterialNS + `"><material name="Plain"/></materialDocument>`)
	if got, err := readVFFMaterials(context.Background(), c, vffMaterialLimits()); err != nil || len(got) != 2 {
		t.Fatalf("plain material: %#v %v", got, err)
	}
}

func TestVFFMaterialXMLDeclarationsCommentsAndLiteralPaths(t *testing.T) {
	c, doc := vffMaterialControl()
	doc = `<?xml version="1.0" encoding="UTF-8"?><!-- <!DOCTYPE is inert comment text -->` + doc
	doc = strings.Replace(doc, "Display Front", "A &amp; B", 1)
	doc = strings.Replace(doc, "./pixels.png", "./pixels%20name.png", 1)
	c.members["materials/Front/material.xml"] = []byte(doc)
	c.members["materials/Front/pixels%20name.png"] = c.members["materials/Front/pixels.png"]
	c.members["materials/Front/pixels name.png"] = []byte("unrelated decoy")
	facts, err := readVFFMaterials(context.Background(), c, vffMaterialLimits())
	if err != nil || len(facts) != 1 || facts[0].name != "A & B" || facts[0].image != "materials/Front/pixels%20name.png" {
		t.Fatalf("XML or literal path semantics changed: %#v %v", facts, err)
	}
	delete(c.members, "materials/Front/pixels%20name.png")
	if got, err := readVFFMaterials(context.Background(), c, vffMaterialLimits()); err == nil || got != nil {
		t.Fatal("missing exact image was repaired by URI decoding")
	}
}

type vffMaterialCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *vffMaterialCancelContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestVFFMaterialCancellationDuringXMLParsingIsAtomic(t *testing.T) {
	c, _ := vffMaterialControl()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 8}
	got, err := readVFFMaterials(controlled, c, vffMaterialLimits())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-document cancellation lost: %#v %v", got, err)
	}
}
func TestVFFMaterialRejectsUnknownAmbiguousAndUnsafeDeclarations(t *testing.T) {
	tests := map[string]func(string) string{
		"unknown-node": func(s string) string {
			return strings.Replace(s, "</material>", `<unknown path="pixels.png"/></material>`, 1)
		},
		"unknown-attribute": func(s string) string { return strings.Replace(s, "name=", `unknown="pixels.png" name=`, 1) },
		"unknown-namespace": func(s string) string { return strings.ReplaceAll(s, vffMaterialNS, "urn:unknown") },
		"namespaced-attribute": func(s string) string {
			return strings.Replace(s, "name=", `xmlns:x="urn:unknown" x:name="other" name=`, 1)
		},
		"duplicate-attribute":       func(s string) string { return strings.Replace(s, "name=", `name="other" name=`, 1) },
		"mixed-content":             func(s string) string { return strings.Replace(s, "</texture>", "unexpected</texture>", 1) },
		"duplicate-root":            func(s string) string { return s + s },
		"trailing-text":             func(s string) string { return s + "not XML" },
		"truncated":                 func(s string) string { return s[:len(s)-1] },
		"dtd":                       func(s string) string { return `<!DOCTYPE materialDocument SYSTEM "file:///never-read">` + s },
		"entity":                    func(s string) string { return strings.Replace(s, "Display Front", "&external;", 1) },
		"processing-instruction":    func(s string) string { return `<?read href="https://never-read"?>` + s },
		"misplaced-xml-declaration": func(s string) string { return `<!-- before declaration --><?xml version="1.0"?>` + s },
		"pbr-enabled":               func(s string) string { return strings.Replace(s, "<enable_normal>0", "<enable_normal>1", 1) },
		"pbr-missing":               func(s string) string { return strings.Replace(s, "<enable_normal>0</enable_normal>", "", 1) },
		"pbr-nondefault": func(s string) string {
			return strings.Replace(s, "</pbrMR>", "<metallicFactor>0</metallicFactor></pbrMR>", 1)
		},
		"duplicate-image": func(s string) string {
			return strings.Replace(s, "</images>", `<image path="./pixels.png"/></images>`, 1)
		},
		"image-missing": func(s string) string { return strings.Replace(s, "./pixels.png", "./missing.png", 1) },
		"no-history-fallback": func(s string) string {
			return strings.Replace(strings.Replace(s, "./pixels.png", "./missing.png", 1), "/historical/not-an-input.png", "pixels.png", 1)
		},
		"colorized":     func(s string) string { return strings.Replace(s, `type="1"`, `type="2"`, 1) },
		"flag-conflict": func(s string) string { return strings.Replace(s, `hasTexture="1"`, `hasTexture="0"`, 1) },
		"nan-scale":     func(s string) string { return strings.Replace(s, `xScale="2"`, `xScale="NaN"`, 1) },
		"bad-color":     func(s string) string { return strings.Replace(s, `colorRed="255"`, `colorRed="256"`, 1) },
	}
	for _, reference := range []string{"/pixels.png", "../pixels.png", "././pixels.png", "./a/../pixels.png", `a\pixels.png`, "https://never-read/pixels.png"} {
		ref := reference
		tests["path-"+ref] = func(s string) string { return strings.Replace(s, "./pixels.png", ref, 1) }
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c, doc := vffMaterialControl()
			c.members["materials/Front/material.xml"] = []byte(mutate(doc))
			before := bytes.Clone(c.members["materials/Front/material.xml"])
			got, err := readVFFMaterials(context.Background(), c, vffMaterialLimits())
			if err == nil || got != nil {
				t.Fatalf("expected atomic refusal: %#v %v", got, err)
			}
			if !bytes.Equal(before, c.members["materials/Front/material.xml"]) {
				t.Fatal("source modified")
			}
		})
	}
	t.Run("duplicate-name", func(t *testing.T) {
		c, doc := vffMaterialControl()
		c.members["materials/Other/material.xml"] = []byte(doc)
		c.members["materials/Other/pixels.png"] = c.members["materials/Front/pixels.png"]
		if got, err := readVFFMaterials(context.Background(), c, vffMaterialLimits()); err == nil || got != nil {
			t.Fatal("duplicate XML identity accepted")
		}
	})
}
func TestVFFMaterialCumulativeBudgetsAndCancellation(t *testing.T) {
	c, doc := vffMaterialControl()
	c.members["materials/Other/material.xml"] = []byte(strings.ReplaceAll(doc, "Display Front", "Other"))
	c.members["materials/Other/pixels.png"] = c.members["materials/Front/pixels.png"]
	for name, configure := range map[string]func(*vffXMLLimits){
		"bytes":      func(l *vffXMLLimits) { l.bytes = len(doc) + 10 },
		"nodes":      func(l *vffXMLLimits) { l.nodes = 12 },
		"depth":      func(l *vffXMLLimits) { l.depth = 3 },
		"attributes": func(l *vffXMLLimits) { l.attributes = 16 },
		"text":       func(l *vffXMLLimits) { l.text = 150 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := vffMaterialLimits()
			configure(&limits)
			if got, err := readVFFMaterials(context.Background(), c, limits); err == nil || got != nil {
				t.Fatalf("budget %s not enforced: %#v %v", name, got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := readVFFMaterials(ctx, c, vffMaterialLimits())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %#v %v", got, err)
	}
}
