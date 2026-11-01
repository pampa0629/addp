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

func TestVFFSKCSchemaLinksCheckExactNamespaceRelationship(t *testing.T) {
	for _, tc := range []struct {
		label, source, target, dependency string
		checked                           bool
	}{
		{"foreign import", `targetNamespace="urn:r"`, `targetNamespace="urn:t"`, `<xs:import namespace="urn:t" schemaLocation="shared.xsd"/>`, true},
		{"wrong import target", `targetNamespace="urn:r"`, `targetNamespace="urn:other"`, `<xs:import namespace="urn:t" schemaLocation="shared.xsd"/>`, false},
		{"cannot import own namespace", `targetNamespace="urn:r"`, `targetNamespace="urn:r"`, `<xs:import namespace="urn:r" schemaLocation="shared.xsd"/>`, false},
		{"import no namespace", `targetNamespace="urn:r"`, "", `<xs:import schemaLocation="shared.xsd"/>`, true},
		{"absent import does not match declared empty", `targetNamespace="urn:r"`, `targetNamespace=""`, `<xs:import schemaLocation="shared.xsd"/>`, false},
		{"empty import is not absent", `targetNamespace="urn:r"`, "", `<xs:import namespace="" schemaLocation="shared.xsd"/>`, false},
		{"both absent cannot import own namespace", "", "", `<xs:import schemaLocation="shared.xsd"/>`, false},
		{"same namespace include", `targetNamespace="urn:r"`, `targetNamespace="urn:r"`, `<xs:include schemaLocation="shared.xsd"/>`, true},
		{"both absent include", "", "", `<xs:include schemaLocation="shared.xsd"/>`, true},
		{"chameleon is not silently adopted", `targetNamespace="urn:r"`, "", `<xs:include schemaLocation="shared.xsd"/>`, false},
		{"foreign include", `targetNamespace="urn:r"`, `targetNamespace="urn:t"`, `<xs:include schemaLocation="shared.xsd"/>`, false},
		{"redefine semantics remain unknown", `targetNamespace="urn:r"`, `targetNamespace="urn:r"`, `<xs:redefine schemaLocation="shared.xsd"/>`, false},
		{"URI namespace is not lowercased", `targetNamespace="urn:r"`, `targetNamespace="urn:T"`, `<xs:import namespace="urn:t" schemaLocation="shared.xsd"/>`, false},
		{"source whitespace not normalized implicitly", `targetNamespace=" urn:r"`, `targetNamespace="urn:t"`, `<xs:import namespace="urn:t" schemaLocation="shared.xsd"/>`, false},
		{"target whitespace not normalized implicitly", `targetNamespace="urn:r"`, `targetNamespace="urn:t "`, `<xs:import namespace="urn:t" schemaLocation="shared.xsd"/>`, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			files := skcDocumentFiles(t)
			files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" ` + tc.source + `>` + tc.dependency + `</xs:schema>`)
			files["Schemas/shared.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" ` + tc.target + `/>`)
			got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
			if err != nil || len(got.documents[0].links) != 1 {
				t.Fatalf("schema link: %v", err)
			}
			link := got.documents[0].links[0]
			if link.namespaceChecked != tc.checked || (len(link.issues) == 0) != tc.checked || link.source.member != "Schemas/root.xsd" || link.target.member != "Schemas/shared.xsd" || link.dependency != 0 {
				t.Fatalf("namespace proof or exact members: %#v", link)
			}
			if link.source.sha256 != sha256.Sum256(files[link.source.member]) || link.target.sha256 != sha256.Sum256(files[link.target.member]) || link.declarationSHA256 != sha256.Sum256(files[link.source.member][link.begin:link.end]) || string(files[link.source.member][link.begin:link.end]) != tc.dependency {
				t.Fatal("exact schema/declaration bytes lost")
			}
		})
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("namespace edge proof opened composition admission")
	}
}

func TestVFFSKCSchemaLinksNeverInventRemoteOrAbsentLocationMappings(t *testing.T) {
	for _, declaration := range []string{
		`<xs:import namespace="urn:t" schemaLocation="https://example.invalid/shared.xsd"/>`,
		`<xs:import namespace="urn:t" schemaLocation="shared.xsd?version=1"/>`,
		`<xs:import namespace="urn:t" schemaLocation="%73hared.xsd"/>`,
		`<xs:import namespace="urn:t"/>`,
	} {
		files := skcDocumentFiles(t)
		files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" targetNamespace="urn:r">` + declaration + `</xs:schema>`)
		files["Schemas/shared.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" targetNamespace="urn:t"/>`)
		got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		if len(got.documents[0].schemas) != 1 || len(got.documents[0].links) != 1 {
			t.Fatal("same namespace or filename silently claimed local target")
		}
		link := got.documents[0].links[0]
		if link.namespaceChecked || link.target.member != "" || link.target.sha256 != ([32]byte{}) || len(link.issues) == 0 || string(files[link.source.member][link.begin:link.end]) != declaration {
			t.Fatal("unresolved dependency promoted to trusted binding")
		}
	}
}

func TestVFFSKCSchemaLinksRetainDistinctUsesAndCycles(t *testing.T) {
	files := skcDocumentFiles(t)
	files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" targetNamespace="urn:r"><xs:import namespace="urn:t" schemaLocation="shared.xsd"/><xs:import namespace="urn:t" schemaLocation="shared.xsd"/></xs:schema>`)
	files["Schemas/shared.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `" targetNamespace="urn:t"><xs:import namespace="urn:r" schemaLocation="root.xsd"/></xs:schema>`)
	got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
	if err != nil || len(got.documents[0].schemas) != 2 || len(got.documents[0].links) != 3 {
		t.Fatalf("cycle/shared schema facts: %v", err)
	}
	links := got.documents[0].links
	if links[0].begin == links[1].begin || links[0].dependency != 0 || links[1].dependency != 1 || links[0].declarationSHA256 != links[1].declarationSHA256 || links[0].target != links[1].target || links[2].target != links[0].source {
		t.Fatal("repeated exact declaration positions or cycle identity collapsed")
	}
	for _, link := range links {
		if !link.namespaceChecked {
			t.Fatal("valid local edge namespace relationship lost")
		}
	}
}

func TestVFFSKCSchemaLinksRequireFreshBytesAndAtomicCancellation(t *testing.T) {
	files := skcDocumentFiles(t)
	got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	item := got.packages.packages[0]
	schemas := got.documents[0].schemas
	for _, mutate := range []func(map[string][]byte, []vffSKCSchemaFact) []vffSKCSchemaFact{
		func(f map[string][]byte, s []vffSKCSchemaFact) []vffSKCSchemaFact { delete(f, s[1].member); return s },
		func(f map[string][]byte, s []vffSKCSchemaFact) []vffSKCSchemaFact {
			f[s[1].member] = append(bytes.Clone(f[s[1].member]), ' ')
			return s
		},
		func(f map[string][]byte, s []vffSKCSchemaFact) []vffSKCSchemaFact { return append(s, s[0]) },
		func(f map[string][]byte, s []vffSKCSchemaFact) []vffSKCSchemaFact {
			s[0].dependencies[0].sha256 = [32]byte{}
			return s
		},
		func(f map[string][]byte, s []vffSKCSchemaFact) []vffSKCSchemaFact {
			s[0].dependencies[0].begin = -1
			return s
		},
	} {
		copyItem := item
		copyItem.files = map[string][]byte{}
		for name, raw := range item.files {
			copyItem.files[name] = bytes.Clone(raw)
		}
		copySchemas := append([]vffSKCSchemaFact(nil), schemas...)
		for i := range copySchemas {
			copySchemas[i].dependencies = append([]vffSKCDependency(nil), schemas[i].dependencies...)
		}
		copySchemas = mutate(copyItem.files, copySchemas)
		if links, err := bindVFFSKCSchemaLinks(context.Background(), copyItem, copySchemas); err == nil || links != nil {
			t.Fatal("stale schema bytes/facts accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	controlled := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: len(schemas) + 2}
	if links, err := bindVFFSKCSchemaLinks(controlled, item, schemas); !errors.Is(err, context.Canceled) || links != nil {
		t.Fatal("in-flight schema linking cancellation returned partial facts")
	}
	// Every duplicate declaration still consumes the original cumulative
	// dependency budget; the identity layer cannot reset it.
	files["Schemas/root.xsd"] = []byte(`<xs:schema xmlns:xs="` + vffXSDNS + `">` + strings.Repeat(`<xs:include schemaLocation="shared.xsd"/>`, 101) + `</xs:schema>`)
	if links, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), skcDocumentLimits()); err == nil || links != nil {
		t.Fatal("schema link dependency budget bypass")
	}
}
