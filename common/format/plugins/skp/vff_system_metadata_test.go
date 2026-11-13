package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func skcAppleDoubleFixture() []byte {
	raw := make([]byte, 160)
	put := func(at int, v uint32) { binary.BigEndian.PutUint32(raw[at:], v) }
	put(0, 0x00051607)
	put(4, 0x20000)
	copy(raw[8:24], "Mac OS X        ")
	binary.BigEndian.PutUint16(raw[24:], 2)
	put(26, 9)
	put(30, 50)
	put(34, 110)
	put(38, 2)
	put(42, 160)
	put(84, 0x41545452)
	put(92, 160)
	put(96, 152)
	put(100, 8)
	binary.BigEndian.PutUint16(raw[118:], 1)
	put(120, 152)
	put(124, 8)
	raw[130] = 21
	copy(raw[131:], "com.apple.quarantine\x00")
	copy(raw[152:], "opaque!!")
	return raw
}

func TestVFFSKCSystemMetadataNormalDocumentRoute(t *testing.T) {
	files := skcDocumentFiles(t)
	name := "__MACOSX/._document.xml"
	raw := skcAppleDoubleFixture()
	files[name] = raw
	files["__MACOSX/unknown.bin"] = []byte("must stay unexplained")
	got, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, files), skcDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	doc := got.documents[0]
	pkg := got.packages.packages[0]
	if len(doc.systemMetadata) != 1 || len(pkg.systemMetadata) != 1 || doc.systemMetadata[0].target != "document.xml" || doc.systemMetadata[0].member != name || doc.systemMetadata[0].sha256 != sha256.Sum256(raw) {
		t.Fatal("system metadata identity lost")
	}
	if !bytes.Equal(pkg.files[name], raw) || len(doc.unexplained) != 3 {
		t.Fatal("source removed or unknown resource excluded")
	}
	for _, fact := range doc.unexplained {
		if fact.member == name {
			t.Fatal("verified metadata still treated as model resource")
		}
	}
	// Such a member cannot simultaneously serve as declared business content.
	files["document.xml"] = bytes.ReplaceAll(files["document.xml"], []byte("Schemas/root.xsd"), []byte(name))
	if facts, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, files), skcDocumentLimits()); err == nil || facts != nil {
		t.Fatal("system member used as business resource")
	}
}

func TestVFFSKCSystemMetadataUnknownContentRemainsUnexplained(t *testing.T) {
	name := "__MACOSX/._document.xml"
	// Every byte in this strict profile's header and attribute-name area has an
	// explained value; changes must not silently acquire exclusion status.
	for at := 0; at < 152; at++ {
		files := skcDocumentFiles(t)
		raw := skcAppleDoubleFixture()
		raw[at] ^= 0x40
		files[name] = raw
		got, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, files), skcDocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		doc := got.documents[0]
		if len(doc.systemMetadata) != 0 || len(doc.unexplained) != 3 {
			t.Fatalf("changed metadata header at %d silently excluded", at)
		}
	}
	for _, name := range []string{"__MACOSX/._missing.xml", "__MACOSX/document.xml", "elsewhere/._document.xml", "__MACOSX/.__", "__MACOSX/._"} {
		files := skcDocumentFiles(t)
		files[name] = skcAppleDoubleFixture()
		got, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, files), skcDocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		if len(got.documents[0].systemMetadata) != 0 || len(got.documents[0].unexplained) != 3 {
			t.Fatal("prefix or absent pair accepted")
		}
	}
	raw := skcAppleDoubleFixture()
	for _, bad := range [][]byte{raw[:80], append(bytes.Clone(raw), 0)} {
		if vffQuarantineAppleDouble(bad) {
			t.Fatal("truncated or trailing metadata accepted")
		}
	}
}

func TestVFFSKCSystemMetadataDirectoryPairAndBudget(t *testing.T) {
	raw := skcAppleDoubleFixture()
	item := vffClassificationPackage{files: map[string][]byte{"__MACOSX/._Schemas": raw}, directories: []string{"Schemas/"}}
	facts := vffSKCSystemMetadata(item)
	if len(facts) != 1 || facts[0].target != "Schemas" {
		t.Fatal("explicit directory pairing lost")
	}
	files := skcDocumentFiles(t)
	files["__MACOSX/._document.xml"] = raw
	limits := skcDocumentLimits()
	var originalBytes int64
	for name, data := range files {
		if name != "__MACOSX/._document.xml" {
			originalBytes += int64(len(data))
		}
	}
	limits.packages.expandedBytes = originalBytes
	without := skcDocumentFiles(t)
	if _, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, without), limits); err != nil {
		t.Fatalf("control without metadata must fit the expansion budget: %v", err)
	}
	if got, err := readVFFClassificationDocuments(context.Background(), skcGraphContainer(t, files), limits); err == nil || got != nil {
		t.Fatal("metadata bypassed ZIP member budget")
	}
}
