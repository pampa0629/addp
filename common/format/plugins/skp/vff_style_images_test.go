package skp

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func vffStyleImageTestLimits() vffXMLLimits {
	return vffXMLLimits{bytes: 1 << 20, nodes: 10000, depth: 32, attributes: 10000, text: 1 << 20}
}
func vffStyleImageDocument(reference string) string {
	return `<s:styleDocument xmlns:s="` + vffStyleNS + `" xmlns:t="` + vffTypesNS + `" xmlns:w="` + vffWatermarkNS + `"><s:style name="Display"><s:item id="2002"><t:variant type="5">123</t:variant></s:item><s:item id="5001"><t:variant type="13"><w:wmlist count="2"><w:screenimages><w:screenimage name="&lt;MODEL SPACE&gt;"/><w:screenimage name="Image &amp; One" info_filename="https://unread.invalid/original.png" scale="0.5"><w:images><w:image id="1" path="` + reference + `" file_name="decoy.png"/></w:images></w:screenimage></w:screenimages></w:wmlist></t:variant></s:item></s:style></s:styleDocument>`
}
func vffStyleImageControl(change func(map[string][]byte)) (*vffContainer, *vffStyleFacts) {
	model := []byte("current model binary fact, not decoded here")
	c := &vffContainer{members: map[string][]byte{"model.dat": model, "styles/One/style.xml": []byte(vffStyleImageDocument("./pixels%20one.png")), "styles/Two/style.xml": []byte(vffStyleImageDocument("watermarks/pixels.png")), "styles/One/pixels%20one.png": vffAuthoredPNG(8, []byte{1, 2, 3, 0}, 1), "watermarks/pixels.png": vffAuthoredPNG(8, []byte{9, 8, 7, 128}, 1)}}
	if change != nil {
		change(c.members)
	}
	catalog := &vffStyleFacts{modelSHA256: sha256.Sum256(c.members["model.dat"])}
	for _, folder := range []string{"One", "Two"} {
		member := "styles/" + folder + "/style.xml"
		catalog.entries = append(catalog.entries, vffStyleEntry{folder: folder, member: member, xmlSHA256: sha256.Sum256(c.members[member])})
	}
	return c, catalog
}
func TestVFFStyleImagesExactBasesAndUnprovedSettings(t *testing.T) {
	c, catalog := vffStyleImageControl(nil)
	got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20)
	if err != nil || len(got.images) != 2 || len(got.unproved) != 4 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	var refs []vffImageReference
	for i, r := range got.images {
		if r.member != []string{"styles/One/pixels%20one.png", "watermarks/pixels.png"}[i] || r.imageID != "1" || r.sha256 != sha256.Sum256(c.members[r.member]) {
			t.Fatal("exact member identity")
		}
		if !strings.HasPrefix(string(c.members[r.styleMember][r.offset:]), "<w:image ") {
			t.Fatal("image start offset")
		}
		refs = append(refs, r.vffImageReference)
	}
	decoded, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits())
	if err != nil || len(decoded) != 2 || decoded[0].minAlpha != 0 || decoded[1].minAlpha != 32896 {
		t.Fatalf("actual pixels: %#v %v", decoded, err)
	}
	for _, r := range got.unproved {
		if r.end <= r.begin || sha256.Sum256(c.members[r.member][r.begin:r.end]) != r.sha256 {
			t.Fatal("unproved XML bytes")
		}
	}
	// Neither exact-root syntax nor relative syntax may try the other's base.
	for _, relative := range []bool{true, false} {
		c, catalog := vffStyleImageControl(nil)
		if relative {
			c.members["pixels%20one.png"] = c.members["styles/One/pixels%20one.png"]
			delete(c.members, "styles/One/pixels%20one.png")
		} else {
			c.members["styles/Two/watermarks/pixels.png"] = c.members["watermarks/pixels.png"]
			delete(c.members, "watermarks/pixels.png")
		}
		if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err == nil || got != nil {
			t.Fatal("base fallback repaired missing member")
		}
	}
}
func TestVFFStyleImagesLiteralEscapesSharedBytesAndExplicitEmpty(t *testing.T) {
	c, catalog := vffStyleImageControl(func(m map[string][]byte) {
		m["styles/One/style.xml"] = []byte(vffStyleImageDocument("./a&amp;b.png"))
		m["styles/One/a&b.png"] = m["watermarks/pixels.png"]
	})
	got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20)
	if err != nil || got.images[0].member != "styles/One/a&b.png" {
		t.Fatalf("XML entities: %#v %v", got, err)
	}
	c, catalog = vffStyleImageControl(func(m map[string][]byte) {
		m["styles/One/style.xml"] = []byte(vffStyleImageDocument("watermarks/pixels.png"))
	})
	var total int
	for _, name := range []string{"model.dat", "styles/One/style.xml", "styles/Two/style.xml", "watermarks/pixels.png"} {
		total += len(c.members[name])
	}
	got, err = readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), total)
	if err != nil || len(got.images) != 2 || got.images[0].member != got.images[1].member || got.images[0].styleMember == got.images[1].styleMember {
		t.Fatalf("shared target distinct statements: %#v %v", got, err)
	}
	if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), total-1); err == nil || got != nil {
		t.Fatal("cumulative XML/image budget did not reject atomically")
	}
	xmlLimit := vffStyleImageTestLimits()
	xmlLimit.bytes = len(c.members["styles/One/style.xml"]) + len(c.members["styles/Two/style.xml"]) - 1
	if got, err := readVFFStyleImages(context.Background(), c, catalog, xmlLimit, 1<<20); err == nil || got != nil || !strings.Contains(err.Error(), "XML byte budget") {
		t.Fatalf("XML budget was not cumulative: %#v %v", got, err)
	}
	for _, empty := range []string{`<w:wmlist count="0"/>`, `<w:wmlist count="0"><w:screenimages/></w:wmlist>`} {
		c, catalog = vffStyleImageControl(func(m map[string][]byte) {
			for _, f := range []string{"One", "Two"} {
				doc := vffStyleImageDocument("./ignored.png")
				start := strings.Index(doc, "<w:wmlist")
				end := strings.Index(doc, "</w:wmlist>") + len("</w:wmlist>")
				m["styles/"+f+"/style.xml"] = []byte(doc[:start] + empty + doc[end:])
			}
		})
		if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err != nil || len(got.images) != 0 {
			t.Fatalf("explicit empty list: %#v %v", got, err)
		}
	}
}

func TestVFFStyleImagesUnprovedItemsNeverBecomeReferenceDiscovery(t *testing.T) {
	c, catalog := vffStyleImageControl(func(m map[string][]byte) {
		doc := vffStyleImageDocument("./pixels%20one.png")
		doc = strings.Replace(doc, `<t:variant type="5">123</t:variant>`, `<t:variant type="13"><w:image path="https://unread.invalid/unproved.png"/></t:variant>`, 1)
		m["styles/One/style.xml"] = []byte(doc)
	})
	got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20)
	if err != nil || len(got.images) != 2 {
		t.Fatalf("partial role facts: %#v %v", got, err)
	}
	found := false
	for _, r := range got.unproved {
		if strings.Contains(string(c.members[r.member][r.begin:r.end]), "https://unread.invalid/unproved.png") {
			found = true
		}
	}
	if !found {
		t.Fatal("unproved item containing resource-shaped content disappeared")
	}
	// A reference alone cannot prove that the selected target is an image.
	c.members[got.images[0].member] = []byte("not an image")
	current, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readVFFImages(context.Background(), c, []vffImageReference{current.images[0].vffImageReference}, vffImageTestLimits()); err == nil {
		t.Fatal("member existence became image validity")
	}
}
func TestVFFStyleImagesRefusalsAreAtomic(t *testing.T) {
	base := vffStyleImageDocument("./pixels%20one.png")
	for name, doc := range map[string]string{
		"DTD": `<!DOCTYPE x>` + base, "entity": `<!DOCTYPE x [<!ENTITY x "a">]>` + base,
		"PI": `<?fetch?>` + base, "two-roots": base + base, "tail": base + "text",
		"duplicate-attribute":     strings.Replace(base, `path="./pixels%20one.png"`, `path="./pixels%20one.png" path="other.png"`, 1),
		"foreign-namespace":       strings.ReplaceAll(base, vffWatermarkNS, "urn:unknown"),
		"unknown-image-attribute": strings.Replace(base, `file_name="decoy.png"`, `file_name="decoy.png" uri="other.png"`, 1),
		"duplicate-item":          strings.Replace(base, `</s:style>`, `<s:item id="5001"><t:variant type="13"/></s:item></s:style>`, 1),
		"missing-list":            strings.Replace(base, `id="5001"`, `id="5002"`, 1),
		"wrong-variant":           strings.Replace(base, `type="13"`, `type="7"`, 1),
		"count":                   strings.Replace(base, `count="2"`, `count="1"`, 1),
		"invalid-count":           strings.Replace(base, `count="2"`, `count="02"`, 1),
		"duplicate-wrapper":       strings.Replace(base, `</w:screenimages>`, `</w:screenimages><w:screenimages/>`, 1),
		"separator-hides-image":   strings.Replace(base, `<w:screenimage name="&lt;MODEL SPACE&gt;"/>`, `<w:screenimage name="&lt;MODEL SPACE&gt;"><w:images><w:image id="1" path="missing.png"/></w:images></w:screenimage>`, 1),
		"multiple-alternatives":   strings.Replace(base, `</w:images>`, `<w:image id="2" path="missing.png"/></w:images>`, 1),
		"mixed-content":           strings.Replace(base, `<w:images>`, `<w:images>text`, 1),
		"image-child":             strings.Replace(base, `file_name="decoy.png"/>`, `file_name="decoy.png"><w:image/></w:image>`, 1),
		"zero-id":                 strings.Replace(base, `<w:image id="1"`, `<w:image id="0"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, catalog := vffStyleImageControl(func(m map[string][]byte) { m["styles/One/style.xml"] = []byte(doc) })
			if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err == nil || got != nil {
				t.Fatal("invalid partial facts")
			}
		})
	}
	for _, reference := range []string{"", "../outside.png", "./../outside.png", "././pixels%20one.png", "/absolute.png", "https://unread.invalid/file.png", "a\\b.png", "./", "model.dat", "./style.xml", "./pixels one.png", "./pixels%2520one.png"} {
		t.Run(reference, func(t *testing.T) {
			c, catalog := vffStyleImageControl(func(m map[string][]byte) { m["styles/One/style.xml"] = []byte(vffStyleImageDocument(reference)) })
			if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err == nil || got != nil {
				t.Fatal("illegal or missing literal reference")
			}
		})
	}
}
func TestVFFStyleImagesBudgetsStaleFactsAndCancellation(t *testing.T) {
	c, catalog := vffStyleImageControl(nil)
	for name, change := range map[string]func(*vffXMLLimits){"bytes": func(l *vffXMLLimits) { l.bytes = 1 }, "nodes": func(l *vffXMLLimits) { l.nodes = 8 }, "depth": func(l *vffXMLLimits) { l.depth = 3 }, "attributes": func(l *vffXMLLimits) { l.attributes = 3 }, "text": func(l *vffXMLLimits) { l.text = 10 }} {
		t.Run(name, func(t *testing.T) {
			l := vffStyleImageTestLimits()
			change(&l)
			if got, err := readVFFStyleImages(context.Background(), c, catalog, l, 1<<20); err == nil || got != nil {
				t.Fatal("XML budget")
			}
		})
	}
	if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), len(c.members["model.dat"])); err == nil || got != nil {
		t.Fatal("reference budget")
	}
	c.members["styles/Two/style.xml"] = append(c.members["styles/Two/style.xml"], ' ')
	if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err == nil || got != nil {
		t.Fatal("stale XML accepted")
	}
	c, catalog = vffStyleImageControl(nil)
	c.members["model.dat"] = []byte("changed")
	if got, err := readVFFStyleImages(context.Background(), c, catalog, vffStyleImageTestLimits(), 1<<20); err == nil || got != nil {
		t.Fatal("stale model accepted")
	}
	c, catalog = vffStyleImageControl(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFStyleImages(ctx, c, catalog, vffStyleImageTestLimits(), 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 10}
	if got, err := readVFFStyleImages(checks, c, catalog, vffStyleImageTestLimits(), 1<<20); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("in-flight cancel")
	}
}
