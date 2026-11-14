package skp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"
)

func skcUnicodePathExtra(name []byte) []byte {
	extra := make([]byte, 54+len(name))
	binary.LittleEndian.PutUint16(extra, 0x5455)
	binary.LittleEndian.PutUint16(extra[2:], 5)
	extra[4] = 1
	binary.LittleEndian.PutUint32(extra[5:], 123456)
	binary.LittleEndian.PutUint16(extra[9:], 0x000a)
	binary.LittleEndian.PutUint16(extra[11:], 32)
	binary.LittleEndian.PutUint16(extra[17:], 1)
	binary.LittleEndian.PutUint16(extra[19:], 24)
	binary.LittleEndian.PutUint16(extra[45:], 0x7075)
	binary.LittleEndian.PutUint16(extra[47:], uint16(5+len(name)))
	extra[49] = 1
	binary.LittleEndian.PutUint32(extra[50:], crc32.ChecksumIEEE(name))
	copy(extra[54:], name)
	return extra
}

func skcUnicodeDescriptorFixture(t *testing.T, name string) []byte {
	t.Helper()
	return skcDescriptorZIP(t, true, vffTestMember{"document.xml", []byte("<classification/>"), zip.Deflate},
		vffTestMember{name, []byte("<schema/>"), zip.Deflate})
}

func skcEditFirstCentralExtra(raw, replacement []byte) []byte {
	directory := directoryPosition(raw)
	at := directory + 46 + int(binary.LittleEndian.Uint16(raw[directory+28:]))
	old := int(binary.LittleEndian.Uint16(raw[directory+30:]))
	out := append(append(bytes.Clone(raw[:at]), replacement...), raw[at+old:]...)
	binary.LittleEndian.PutUint16(out[directory+30:], uint16(len(replacement)))
	binary.LittleEndian.PutUint32(out[len(out)-10:], binary.LittleEndian.Uint32(raw[len(raw)-10:])+uint32(len(replacement))-uint32(old))
	return out
}

// Change both copies and preserve all offsets. Equal malformed extras must
// still fail their semantic checks, independently of local/central equality.
func skcEditFirstPairedExtra(t *testing.T, raw []byte, change func([]byte) []byte) []byte {
	t.Helper()
	at := 30 + int(binary.LittleEndian.Uint16(raw[26:]))
	n := int(binary.LittleEndian.Uint16(raw[28:]))
	e := change(bytes.Clone(raw[at : at+n]))
	return skcEditFirstCentralExtra(skcReplaceFirstExtra(t, raw, e), e)
}

func TestVFFSKCUnicodePathExactIdentityAndProfileSeparation(t *testing.T) {
	for _, name := range []string{"Schemas/a.xsd", "Schemas/楼层.xsd"} {
		t.Run(name, func(t *testing.T) {
			raw := skcUnicodeDescriptorFixture(t, name)
			original := bytes.Clone(raw)
			got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits())
			if err != nil {
				t.Fatal(err)
			}
			if len(got.packages) != 1 || got.packages[0].sha256 != sha256.Sum256(raw) || len(got.packages[0].files) != 2 || len(got.packages[0].directories) != 0 || string(got.packages[0].files[name]) != "<schema/>" || !bytes.Equal(raw, original) {
				t.Fatal("source name, bytes or identity changed")
			}
			source := &vffSource{ctx: context.Background(), input: bytes.NewReader(raw), size: int64(len(raw)), remaining: 1 << 20}
			if records, _, err := readVFFDirectory(source, 0, vffTestLimits(), vffModelZIP); err == nil || records != nil {
				t.Fatal("SKC Unicode descriptor broadened the model profile")
			}
		})
	}
}

func TestVFFSKCUnicodePathRejectsConflictsAndDescriptors(t *testing.T) {
	for name, change := range map[string]func([]byte) []byte{
		"name crc":             func(e []byte) []byte { e[50] ^= 1; return e },
		"path conflict":        func(e []byte) []byte { e[54] = 'X'; return e },
		"path traversal":       func(e []byte) []byte { copy(e[54:], "../"); return e },
		"path BOM":             func(e []byte) []byte { copy(e[54:], []byte{0xef, 0xbb, 0xbf}); return e },
		"path NUL":             func(e []byte) []byte { e[54] = 0; return e },
		"unknown version":      func(e []byte) []byte { e[49] = 2; return e },
		"UT flags":             func(e []byte) []byte { e[4] = 7; return e },
		"UT length":            func(e []byte) []byte { e[2]++; return e },
		"NTFS reserved":        func(e []byte) []byte { e[13]++; return e },
		"NTFS unknown tag":     func(e []byte) []byte { e[17]++; return e },
		"NTFS tag length":      func(e []byte) []byte { e[19]--; return e },
		"unknown extra":        func(e []byte) []byte { e[45] ^= 1; return e },
		"Unicode extra length": func(e []byte) []byte { e[47]++; return e },
		"duplicate path":       func(e []byte) []byte { return append(e, e[45:]...) },
		"additional extra":     func(e []byte) []byte { return append(e, 0xaa, 0xbb, 0, 0) },
		"extra trailing byte":  func(e []byte) []byte { return append(e, 0) },
		"extra order":          func(e []byte) []byte { return append(append(bytes.Clone(e[9:45]), e[:9]...), e[45:]...) },
	} {
		t.Run(name, func(t *testing.T) {
			raw := skcEditFirstPairedExtra(t, skcUnicodeDescriptorFixture(t, "Schemas/a.xsd"), change)
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits()); err == nil || got != nil {
				t.Fatal("conflicting or unknown paired extra accepted")
			}
		})
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"local extra disagreement": func(b []byte) []byte { b[30+int(binary.LittleEndian.Uint16(b[26:]))+5]++; return b },
		"central extra disagreement": func(b []byte) []byte {
			p := directoryPosition(b)
			b[p+46+int(binary.LittleEndian.Uint16(b[p+28:]))+5]++
			return b
		},
		"local name disagreement": func(b []byte) []byte { b[30] = 'X'; return b },
		"local crc":               func(b []byte) []byte { b[14] = 1; return b },
		"local compressed":        func(b []byte) []byte { b[18] = 1; return b },
		"IFC4 prefilled expanded": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[22:], binary.LittleEndian.Uint32(b[directoryPosition(b)+24:]))
			return b
		},
		"local ZIP64": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[4:], 45)
			binary.LittleEndian.PutUint32(b[18:], 0xffffffff)
			binary.LittleEndian.PutUint32(b[22:], 0xffffffff)
			return b
		},
		"central ZIP64": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+24:], 0xffffffff)
			return b
		},
		"missing EFS": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 8)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 8)
			return b
		},
		"missing descriptor flag": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 0x800)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 0x800)
			return b
		},
		"encrypted": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 0x809)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 0x809)
			return b
		},
		"signature":             func(b []byte) []byte { b[skcFirstDescriptor(b)] ^= 1; return b },
		"descriptor crc":        func(b []byte) []byte { b[skcFirstDescriptor(b)+4] ^= 1; return b },
		"descriptor compressed": func(b []byte) []byte { b[skcFirstDescriptor(b)+8]++; return b },
		"descriptor expanded":   func(b []byte) []byte { b[skcFirstDescriptor(b)+12]++; return b },
		"actual crc":            func(b []byte) []byte { b[skcFirstDescriptor(b)+4] ^= 1; b[directoryPosition(b)+16] ^= 1; return b },
		"unsigned descriptor":   func(b []byte) []byte { at := skcFirstDescriptor(b); return skcReplaceFirstDescriptor(b, b[at+4:at+16]) },
		"truncated descriptor":  func(b []byte) []byte { at := skcFirstDescriptor(b); return skcReplaceFirstDescriptor(b, b[at:at+15]) },
		"missing descriptor":    func(b []byte) []byte { return skcReplaceFirstDescriptor(b, nil) },
		"ZIP64 descriptor": func(b []byte) []byte {
			at := skcFirstDescriptor(b)
			d := make([]byte, 24)
			copy(d, b[at:at+8])
			binary.LittleEndian.PutUint64(d[8:], uint64(binary.LittleEndian.Uint32(b[at+8:])))
			binary.LittleEndian.PutUint64(d[16:], uint64(binary.LittleEndian.Uint32(b[at+12:])))
			return skcReplaceFirstDescriptor(b, d)
		},
		"descriptor trailing byte": func(b []byte) []byte {
			at := skcFirstDescriptor(b)
			return skcReplaceFirstDescriptor(b, append(bytes.Clone(b[at:at+16]), 0))
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := mutate(skcUnicodeDescriptorFixture(t, "Schemas/a.xsd"))
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits()); err == nil || got != nil {
				t.Fatal("invalid Unicode descriptor accepted")
			}
		})
	}
	// Every prefix truncation must fail, including cuts inside individual
	// extension headers, CRC bytes and UTF-8 name payloads.
	raw := skcUnicodeDescriptorFixture(t, "Schemas/a.xsd")
	n := int(binary.LittleEndian.Uint16(raw[28:]))
	for cut := 0; cut < n; cut++ {
		t.Run(fmt.Sprintf("extra prefix %d", cut), func(t *testing.T) {
			b := skcEditFirstPairedExtra(t, raw, func(e []byte) []byte { return e[:cut] })
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, b), skcTestLimits()); err == nil || got != nil {
				t.Fatal("truncated extension accepted")
			}
		})
	}
}

func TestVFFSKCUnicodePathBudgetsAndCancellation(t *testing.T) {
	raw := skcUnicodeDescriptorFixture(t, "Schemas/a.xsd")
	for name, change := range map[string]func(*vffClassificationPackageLimits){"read": func(l *vffClassificationPackageLimits) { l.readBytes = 1 }, "expanded": func(l *vffClassificationPackageLimits) { l.expandedBytes = 1 }, "member": func(l *vffClassificationPackageLimits) { l.memberBytes = 1 }, "count": func(l *vffClassificationPackageLimits) { l.members = 1 }} {
		t.Run(name, func(t *testing.T) {
			l := skcTestLimits()
			change(&l)
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), l); err == nil || got != nil {
				t.Fatal("budget bypassed")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationPackages(ctx, skcControl(t, raw), skcTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancellation bypassed")
	}
}
