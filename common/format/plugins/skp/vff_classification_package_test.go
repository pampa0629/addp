package skp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/addp/common/format"
)

// Authored normal ZIP controls, not proof of SketchUp schema semantics. Reuse
// the existing raw ZIP author rather than a second decompressor or fixture path.
func skcTestZIP(t *testing.T, ntfs bool, members ...vffTestMember) []byte {
	t.Helper()
	outer := vffFixture(t, members...)
	raw := bytes.Clone(outer[basePosition(outer):])
	directory := directoryPosition(raw)
	var central bytes.Buffer
	for offset := directory; offset < len(raw)-22; {
		header := bytes.Clone(raw[offset : offset+46])
		n := int(binary.LittleEndian.Uint16(header[28:]))
		name := raw[offset+46 : offset+46+n]
		if bytes.HasSuffix(name, []byte("/")) {
			binary.LittleEndian.PutUint16(header[4:], 20) // DOS directory identity.
			binary.LittleEndian.PutUint32(header[38:], 0x10)
		}
		var extra []byte
		if ntfs {
			extra = make([]byte, 36)
			binary.LittleEndian.PutUint16(extra, 0x000a)
			binary.LittleEndian.PutUint16(extra[2:], 32)
			binary.LittleEndian.PutUint16(extra[8:], 1)
			binary.LittleEndian.PutUint16(extra[10:], 24)
			for i := 12; i < len(extra); i++ {
				extra[i] = byte(i) // Time values are metadata, never paths.
			}
		}
		binary.LittleEndian.PutUint16(header[30:], uint16(len(extra)))
		central.Write(header)
		central.Write(name)
		central.Write(extra)
		offset += 46 + n
	}
	end := bytes.Clone(raw[len(raw)-22:])
	binary.LittleEndian.PutUint32(end[12:], uint32(central.Len()))
	return append(append(bytes.Clone(raw[:directory]), central.Bytes()...), end...)
}

func skcTestLimits() vffClassificationPackageLimits {
	return vffClassificationPackageLimits{references: vffClassificationTestLimits(), readBytes: 2 << 20, expandedBytes: 2 << 20, memberBytes: 1 << 20, packages: 10, members: 100}
}

func skcControl(t *testing.T, raw []byte) *vffContainer {
	t.Helper()
	return vffClassificationControl(func(_ map[string][]byte, c *vffContainer) {
		c.members["classifications/literal%20schema.skc"] = raw
	})
}

func TestVFFClassificationPackagesExactBytesAndProfileSeparation(t *testing.T) {
	for _, ntfs := range []bool{false, true} {
		for _, method := range []uint16{zip.Store, zip.Deflate} {
			raw := skcTestZIP(t, ntfs, vffTestMember{"document.xml", []byte("<classification/>"), method}, vffTestMember{"Schemas/", nil, zip.Store}, vffTestMember{"Schemas/literal%20name.xsd", []byte("<schema/>"), method})
			c := skcControl(t, raw)
			before := bytes.Clone(c.members["model.dat"])
			got, err := readVFFClassificationPackages(context.Background(), c, skcTestLimits())
			if err != nil || len(got.packages) != 1 || len(got.references.entries) != 1 {
				t.Fatalf("package: %#v %v", got, err)
			}
			item := got.packages[0]
			if item.member != "classifications/literal%20schema.skc" || item.sha256 != sha256.Sum256(raw) || len(item.files) != 2 || len(item.directories) != 1 || item.directories[0] != "Schemas/" || string(item.files["Schemas/literal%20name.xsd"]) != "<schema/>" || string(item.files["document.xml"]) != "<classification/>" {
				t.Fatal("exact nested identities/bytes")
			}
			if !bytes.Equal(before, c.members["model.dat"]) || !bytes.Equal(raw, c.members[item.member]) {
				t.Fatal("source changed")
			}
		}
	}
	// The same physical parser must retain the strict outer envelope profile.
	raw := skcTestZIP(t, true, vffTestMember{"model.dat", []byte("source model"), zip.Store})
	outer := vffFixture(t, vffTestMember{"model.dat", []byte("source model"), zip.Store})
	outer = append(bytes.Clone(outer[:basePosition(outer)]), raw...)
	if got, err := readTestVFF(outer, vffTestLimits()); err == nil || got != nil {
		t.Fatal("SKC NTFS allowance escaped to model envelope")
	}
	dirMembers := []vffTestMember{{"model.dat", []byte("source model"), zip.Store}, {"Schemas/", nil, zip.Store}}
	raw = skcTestZIP(t, false, dirMembers...)
	outer = vffFixture(t, dirMembers...)
	outer = append(bytes.Clone(outer[:basePosition(outer)]), raw...)
	if got, err := readTestVFF(outer, vffTestLimits()); err == nil || got != nil {
		t.Fatal("SKC explicit directory allowance escaped to model envelope")
	}
	reserved := vffReservedLocalFixture(t, zip.Deflate)
	if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, reserved[basePosition(reserved):]), skcTestLimits()); err == nil || got != nil {
		t.Fatal("VFF local ZIP64 reservation escaped to SKC")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("nested byte validation opened composition admission")
	}
}

func TestVFFClassificationPackagesCumulativeBudgetsAndDistinctIdentity(t *testing.T) {
	raw := skcTestZIP(t, false, vffTestMember{"Schemas/", nil, zip.Store}, vffTestMember{"Schemas/a.xsd", []byte("<schema/>"), zip.Deflate})
	c := vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		c.members["classifications/literal%20schema.skc"] = raw
		entry := vffRecord("8467", p["directory"], p["name"], p["path"])
		p["entries"] = append(bytes.Clone(entry), entry...)
	})
	limits := skcTestLimits()
	limits.packages, limits.members, limits.readBytes, limits.expandedBytes = 1, 2, int64(len(raw)), int64(len("<schema/>"))
	limits.references.bytes = len(c.members["model.dat"]) + len(raw)
	got, err := readVFFClassificationPackages(context.Background(), c, limits)
	if err != nil || len(got.packages) != 1 || len(got.references.entries) != 2 {
		t.Fatalf("shared exact member: %#v %v", got, err)
	}
	for name, mutate := range map[string]func(*vffClassificationPackageLimits){
		"source":                    func(l *vffClassificationPackageLimits) { l.references.bytes-- },
		"reads":                     func(l *vffClassificationPackageLimits) { l.readBytes-- },
		"expansion":                 func(l *vffClassificationPackageLimits) { l.expandedBytes-- },
		"member":                    func(l *vffClassificationPackageLimits) { l.memberBytes = int64(len("<schema/>") - 1) },
		"records include directory": func(l *vffClassificationPackageLimits) { l.members-- },
	} {
		t.Run(name, func(t *testing.T) {
			copy := limits
			mutate(&copy)
			if got, err := readVFFClassificationPackages(context.Background(), c, copy); err == nil || got != nil {
				t.Fatal("cumulative boundary accepted")
			}
		})
	}
	// Equal bytes are not an identity: another exact package must be charged.
	c = vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		c.members["classifications/literal%20schema.skc"] = raw
		c.members["classifications/second.skc"] = bytes.Clone(raw)
		p["entries"] = vffRecord("8467", p["directory"], p["name"], p["path"])
		p["entries"] = append(p["entries"], vffRecord("8467", p["directory"], p["name"], vffRecord("8767", []byte("classifications/second.skc")))...)
	})
	full := skcTestLimits()
	full.packages, full.members, full.readBytes, full.expandedBytes = 2, 4, int64(len(raw)*2), int64(len("<schema/>")*2)
	for name, mutate := range map[string]func(*vffClassificationPackageLimits){
		"packages":  func(l *vffClassificationPackageLimits) { l.packages = 1 },
		"reads":     func(l *vffClassificationPackageLimits) { l.readBytes-- },
		"expansion": func(l *vffClassificationPackageLimits) { l.expandedBytes-- },
		"members":   func(l *vffClassificationPackageLimits) { l.members-- },
	} {
		t.Run("second "+name, func(t *testing.T) {
			copy := full
			mutate(&copy)
			if got, err := readVFFClassificationPackages(context.Background(), c, copy); err == nil || got != nil {
				t.Fatal("distinct package budget not charged")
			}
		})
	}
	got, err = readVFFClassificationPackages(context.Background(), c, full)
	if err != nil || len(got.packages) != 2 || got.packages[0].member == got.packages[1].member {
		t.Fatalf("distinct identities: %#v %v", got, err)
	}
}

func TestVFFClassificationPackagesRejectArchiveAmbiguity(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"central crc": func(b []byte) []byte { b[directoryPosition(b)+16] ^= 1; return b },
		"body crc":    func(b []byte) []byte { b[30+len("document.xml")] ^= 1; return b },
		"descriptor":  func(b []byte) []byte { b[directoryPosition(b)+8] |= 8; b[6] |= 8; return b },
		"encrypted":   func(b []byte) []byte { b[directoryPosition(b)+8] |= 1; b[6] |= 1; return b },
		"zip64": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+24:], 0xffffffff)
			return b
		},
		"symlink": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+38:], uint32(0120777)<<16)
			return b
		},
		"unknown DOS attributes": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+38:], 0x400)
			return b
		},
		"normal with another DOS attribute": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+38:], 0x82)
			return b
		},
		"unknown Windows high attributes": func(b []byte) []byte {
			p := directoryPosition(b)
			binary.LittleEndian.PutUint16(b[p+4:], 20)
			binary.LittleEndian.PutUint32(b[p+38:], 0x400000)
			return b
		},
		"unknown extra":          func(b []byte) []byte { b[directoryPosition(b)+46+len("document.xml")] = 11; return b },
		"NTFS reserved":          func(b []byte) []byte { b[directoryPosition(b)+46+len("document.xml")+4] = 1; return b },
		"NTFS unknown attribute": func(b []byte) []byte { b[directoryPosition(b)+46+len("document.xml")+8] = 2; return b },
		"NTFS wrong length":      func(b []byte) []byte { b[directoryPosition(b)+46+len("document.xml")+10] = 23; return b },
		"truncated":              func(b []byte) []byte { return b[:len(b)-1] },
		"trailing":               func(b []byte) []byte { return append(b, 0) },
		"local name":             func(b []byte) []byte { b[30] = 'X'; return b },
		"directory payload": func(b []byte) []byte {
			p := directoryPosition(b)
			b[30+len("document.xml")-1], b[p+46+len("document.xml")-1] = '/', '/'
			binary.LittleEndian.PutUint16(b[p+4:], 20)
			binary.LittleEndian.PutUint32(b[p+38:], 0x10)
			return b
		},
		"hidden bytes": func(b []byte) []byte {
			p := directoryPosition(b)
			b = append(append(bytes.Clone(b[:p]), 0), b[p:]...)
			binary.LittleEndian.PutUint32(b[len(b)-22+16:], uint32(p+1))
			return b
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := skcTestZIP(t, true, vffTestMember{"document.xml", []byte("<classification/>"), zip.Store})
			got, err := readVFFClassificationPackages(context.Background(), skcControl(t, mutate(raw)), skcTestLimits())
			if err == nil || got != nil {
				t.Fatal("ambiguous/corrupt nested container accepted")
			}
		})
	}
	for name, members := range map[string][]vffTestMember{
		"duplicate":          {{"same", nil, zip.Store}, {"same", nil, zip.Store}},
		"file and directory": {{"Schemas", nil, zip.Store}, {"Schemas/", nil, zip.Store}},
		"file parent":        {{"Schemas", nil, zip.Store}, {"Schemas/a.xsd", nil, zip.Store}},
		"parent traversal":   {{"../schema", nil, zip.Store}},
		"absolute":           {{"/schema", nil, zip.Store}},
		"url":                {{"https://invalid/schema", nil, zip.Store}},
		"backslash":          {{"Schemas\\a.xsd", nil, zip.Store}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readVFFClassificationPackages(context.Background(), skcControl(t, skcTestZIP(t, false, members...)), skcTestLimits())
			if err == nil || got != nil {
				t.Fatal("invalid nested paths accepted")
			}
		})
	}
}

func TestVFFClassificationPackagesRequireFreshReferencesAndCancellation(t *testing.T) {
	c := skcControl(t, skcTestZIP(t, false, vffTestMember{"document.xml", nil, zip.Store}))
	if got, err := readVFFClassificationPackages(context.Background(), c, skcTestLimits()); err != nil || len(got.packages) != 1 {
		t.Fatalf("valid source control: %#v %v", got, err)
	}
	// Only a genuinely explicit empty classification list yields zero packages.
	empty := vffClassificationControl(func(p map[string][]byte, _ *vffContainer) { p["entries"] = nil })
	got, err := readVFFClassificationPackages(context.Background(), empty, skcTestLimits())
	if err != nil || len(got.packages) != 0 || len(got.references.entries) != 0 {
		t.Fatalf("explicit empty list: %#v %v", got, err)
	}
	for _, limits := range []vffClassificationPackageLimits{{}, {references: vffClassificationTestLimits()}, {readBytes: 1, expandedBytes: 1, memberBytes: 1, packages: 1, members: 1}} {
		if got, err := readVFFClassificationPackages(context.Background(), c, limits); err == nil || got != nil {
			t.Fatal("unspecified limits accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationPackages(ctx, c, skcTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("cancelled read: %#v %v", got, err)
	}
	if got, err := readVFFClassificationPackages(context.Background(), nil, skcTestLimits()); err == nil || got != nil {
		t.Fatal("nil source accepted")
	}
	emptyZIP := make([]byte, 22)
	binary.LittleEndian.PutUint32(emptyZIP, 0x06054b50)
	if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, emptyZIP), skcTestLimits()); err == nil || got != nil {
		t.Fatal("empty nested archive accepted")
	}
	delete(c.members, "classifications/literal%20schema.skc")
	if got, err := readVFFClassificationPackages(context.Background(), c, skcTestLimits()); err == nil || got != nil {
		t.Fatal("missing current declaration rescued by an old receipt")
	}
}
