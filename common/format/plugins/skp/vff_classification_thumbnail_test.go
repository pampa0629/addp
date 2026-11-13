package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
)

func TestVFFSKCExplicitEmptyThumbnailPreservesUnclaimedResources(t *testing.T) {
	for _, empty := range []string{`<thumbnail/>`, `<thumbnail></thumbnail>`} {
		for _, keepImages := range []bool{false, true} {
			files := skcDocumentFiles(t)
			files["documentProperties.xml"] = bytes.ReplaceAll(files["documentProperties.xml"], []byte(`<thumbnail>images/literal%20thumb.png</thumbnail>`), []byte(empty))
			if !keepImages {
				delete(files, "images/literal%20thumb.png")
				delete(files, "images/literal thumb.png")
			} else {
				// Unbound bytes must remain opaque, even if they are not a valid image.
				files["images/literal%20thumb.png"] = []byte("unclaimed, not a PNG")
			}
			c := skcDocumentContainer(t, files)
			source := bytes.Clone(c.members["classifications/literal%20schema.skc"])
			limits := skcDocumentLimits()
			packages, err := readVFFClassificationPackages(context.Background(), c, limits.packages)
			if err != nil {
				t.Fatal(err)
			}
			images := limits.images
			dependencies := limits.dependencies
			got, err := readVFFSKCDocument(context.Background(), packages.packages[0], &vffXMLBudget{limits: limits.xml}, &vffSKCFilterBudget{limits: limits.filters}, &images, &dependencies)
			if err != nil {
				t.Fatalf("explicit empty thumbnail rejected: %s images=%v: %v", empty, keepImages, err)
			}
			if got.thumbnail != nil {
				t.Fatal("empty thumbnail acquired an image fact")
			}
			if images != limits.images {
				t.Fatal("empty thumbnail consumed image budget")
			}
			want := map[string][32]byte{"unbound.xsd": sha256.Sum256(files["unbound.xsd"])}
			if keepImages {
				for _, name := range []string{"images/literal%20thumb.png", "images/literal thumb.png"} {
					want[name] = sha256.Sum256(files[name])
				}
			}
			if len(got.unexplained) != len(want) {
				t.Fatal("unbound resources silently claimed or dropped")
			}
			for _, member := range got.unexplained {
				if sum, ok := want[member.member]; !ok || sum != member.sha256 {
					t.Fatal("unbound source identity changed")
				}
			}
			if got.sha256 != sha256.Sum256(source) || !bytes.Equal(c.members[got.member], source) {
				t.Fatal("source changed")
			}
			// The normal document entry must use the same optional-image semantics.
			all, err := readVFFClassificationDocuments(context.Background(), c, limits)
			if err != nil || len(all.documents) != 1 {
				t.Fatalf("normal document route: %v", err)
			}
			if all.documents[0].thumbnail != nil {
				t.Fatal("normal document route acquired an image fact")
			}
		}
	}
}

func TestVFFSKCEmptyThumbnailDoesNotRelaxOtherDeclarations(t *testing.T) {
	original := []byte(`<thumbnail>images/literal%20thumb.png</thumbnail>`)
	for name, replacement := range map[string]string{
		"missing":        "",
		"duplicate":      `<thumbnail/><thumbnail/>`,
		"nil":            `<thumbnail xsi:nil="true"/>`,
		"whitespace":     `<thumbnail> </thumbnail>`,
		"child":          `<thumbnail><image/></thumbnail>`,
		"attribute":      `<thumbnail other="x"/>`,
		"parent path":    `<thumbnail>../x.png</thumbnail>`,
		"absolute path":  `<thumbnail>/x.png</thumbnail>`,
		"missing member": `<thumbnail>missing.png</thumbnail>`,
	} {
		t.Run(name, func(t *testing.T) {
			files := skcDocumentFiles(t)
			files["documentProperties.xml"] = bytes.ReplaceAll(files["documentProperties.xml"], original, []byte(replacement))
			files["documentProperties.xml"] = bytes.ReplaceAll(files["documentProperties.xml"], []byte(`<documentProperties xmlns=`), []byte(`<documentProperties xmlns:xsi="`+vffXSINS+`" xmlns=`))
			if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits()); err == nil || got != nil {
				t.Fatal("invalid thumbnail declaration accepted or partial facts returned")
			}
		})
	}
	for name, mutate := range map[string]func(map[string][]byte){
		"UTC timestamp": func(files map[string][]byte) {
			files["documentProperties.xml"] = bytes.ReplaceAll(files["documentProperties.xml"], []byte("2026-10-31T12:00:00"), []byte("2026-10-31T12:00:00Z"))
		},
		"malformed schema": func(files map[string][]byte) {
			files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `"><xs:element name="1 kv <> 100 v"/></xs:schema>`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			files := skcDocumentFiles(t)
			files["documentProperties.xml"] = bytes.ReplaceAll(files["documentProperties.xml"], original, []byte(`<thumbnail/>`))
			mutate(files)
			if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits()); err == nil || got != nil {
				t.Fatal("empty thumbnail bypassed timestamp or schema validation")
			}
		})
	}
}
