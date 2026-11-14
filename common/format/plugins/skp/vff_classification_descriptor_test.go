package skp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

// Reframe the ordinary authored ZIP, preserving its compressed bytes. This is
// the observed SKC layout, including its prefilled local expanded size; it is
// deliberately not a general-purpose ZIP author or alternate decompressor.
func skcDescriptorFixture(t *testing.T) []byte {
	t.Helper()
	return skcDescriptorZIP(t, false, vffTestMember{"document.xml", []byte("<classification/>"), zip.Deflate},
		vffTestMember{"Schemas/", nil, zip.Store}, vffTestMember{"Schemas/a.xsd", []byte("<schema/>"), zip.Deflate})
}

func skcDescriptorZIP(t *testing.T, unicodePath bool, members ...vffTestMember) []byte {
	t.Helper()
	raw := skcTestZIP(t, false, members...)
	extra := make([]byte, 32)
	binary.LittleEndian.PutUint16(extra, 0x5455)
	binary.LittleEndian.PutUint16(extra[2:], 13)
	extra[4] = 7
	binary.LittleEndian.PutUint16(extra[17:], 0x7875)
	binary.LittleEndian.PutUint16(extra[19:], 11)
	extra[21], extra[22], extra[27] = 1, 4, 4
	for _, at := range []int{5, 9, 13, 23, 28} {
		binary.LittleEndian.PutUint32(extra[at:], uint32(at*123))
	}
	var locals, central bytes.Buffer
	for p := directoryPosition(raw); p < len(raw)-22; {
		h := bytes.Clone(raw[p : p+46])
		n := int(binary.LittleEndian.Uint16(h[28:]))
		name := raw[p+46 : p+46+n]
		offset := int(binary.LittleEndian.Uint32(h[42:]))
		local := bytes.Clone(raw[offset : offset+30])
		start := offset + 30 + n + int(binary.LittleEndian.Uint16(local[28:]))
		size := int(binary.LittleEndian.Uint32(h[20:]))
		flags := uint16(8)
		entryExtra := extra
		if unicodePath {
			flags = 0x808
			entryExtra = skcUnicodePathExtra(name)
			binary.LittleEndian.PutUint32(local[22:], 0)
		}
		if bytes.HasSuffix(name, []byte("/")) {
			flags = 0
		}
		binary.LittleEndian.PutUint16(local[4:], 20)
		binary.LittleEndian.PutUint16(local[6:], flags)
		binary.LittleEndian.PutUint32(local[14:], 0)
		binary.LittleEndian.PutUint32(local[18:], 0)
		binary.LittleEndian.PutUint16(local[28:], uint16(len(entryExtra)))
		binary.LittleEndian.PutUint16(h[6:], 20)
		binary.LittleEndian.PutUint16(h[8:], flags)
		binary.LittleEndian.PutUint16(h[30:], uint16(len(entryExtra)))
		binary.LittleEndian.PutUint32(h[42:], uint32(locals.Len()))
		locals.Write(local)
		locals.Write(name)
		locals.Write(entryExtra)
		locals.Write(raw[start : start+size])
		if flags&8 != 0 {
			var descriptor [16]byte
			binary.LittleEndian.PutUint32(descriptor[:], 0x08074b50)
			copy(descriptor[4:], h[16:28])
			locals.Write(descriptor[:])
		}
		central.Write(h)
		central.Write(name)
		central.Write(entryExtra)
		p += 46 + n
	}
	end := bytes.Clone(raw[len(raw)-22:])
	binary.LittleEndian.PutUint32(end[12:], uint32(central.Len()))
	binary.LittleEndian.PutUint32(end[16:], uint32(locals.Len()))
	return append(append(locals.Bytes(), central.Bytes()...), end...)
}

func skcFirstDescriptor(b []byte) int {
	return 30 + int(binary.LittleEndian.Uint16(b[26:])) + int(binary.LittleEndian.Uint16(b[28:])) + int(binary.LittleEndian.Uint32(b[directoryPosition(b)+20:]))
}

// Keep offsets consistent when varying descriptor length. Rejections must
// establish the framing boundary, not merely notice stale directory offsets.
func skcReplaceFirstDescriptor(raw, replacement []byte) []byte {
	at, directory := skcFirstDescriptor(raw), directoryPosition(raw)
	delta := len(replacement) - 16
	out := append(append(bytes.Clone(raw[:at]), replacement...), raw[at+16:]...)
	for p := directory + delta; p < len(out)-22; {
		h := out[p:]
		offset := binary.LittleEndian.Uint32(h[42:])
		if int(offset) > at {
			binary.LittleEndian.PutUint32(h[42:], uint32(int(offset)+delta))
		}
		p += 46 + int(binary.LittleEndian.Uint16(h[28:])) + int(binary.LittleEndian.Uint16(h[30:]))
	}
	binary.LittleEndian.PutUint32(out[len(out)-6:], uint32(directory+delta))
	return out
}

func TestVFFSKCSignedDescriptorExactBytesAndProfileSeparation(t *testing.T) {
	raw := skcDescriptorFixture(t)
	before := bytes.Clone(raw)
	got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.packages) != 1 || got.packages[0].sha256 != sha256.Sum256(raw) || len(got.packages[0].files) != 2 || len(got.packages[0].directories) != 1 ||
		string(got.packages[0].files["document.xml"]) != "<classification/>" || string(got.packages[0].files["Schemas/a.xsd"]) != "<schema/>" || !bytes.Equal(raw, before) {
		t.Fatal("descriptor content, directory or original identity changed")
	}
	source := &vffSource{ctx: context.Background(), input: bytes.NewReader(raw), size: int64(len(raw)), remaining: 1 << 20}
	if records, _, err := readVFFDirectory(source, 0, vffTestLimits(), vffModelZIP); err == nil || records != nil {
		t.Fatal("SKC descriptor escaped to model profile")
	}
}

func TestVFFSKCSignedDescriptorRejectsConflictsAndUnsupportedVariants(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"unsigned 12 byte":   func(b []byte) []byte { at := skcFirstDescriptor(b); return skcReplaceFirstDescriptor(b, b[at+4:at+16]) },
		"truncated 15 byte":  func(b []byte) []byte { at := skcFirstDescriptor(b); return skcReplaceFirstDescriptor(b, b[at:at+15]) },
		"missing descriptor": func(b []byte) []byte { return skcReplaceFirstDescriptor(b, nil) },
		"ZIP64 24 byte": func(b []byte) []byte {
			at := skcFirstDescriptor(b)
			d := make([]byte, 24)
			copy(d, b[at:at+8])
			binary.LittleEndian.PutUint64(d[8:], uint64(binary.LittleEndian.Uint32(b[at+8:])))
			binary.LittleEndian.PutUint64(d[16:], uint64(binary.LittleEndian.Uint32(b[at+12:])))
			return skcReplaceFirstDescriptor(b, d)
		},
		"descriptor trailing bytes": func(b []byte) []byte {
			at := skcFirstDescriptor(b)
			return skcReplaceFirstDescriptor(b, append(bytes.Clone(b[at:at+16]), 0))
		},
		"signature":                func(b []byte) []byte { b[skcFirstDescriptor(b)] ^= 1; return b },
		"descriptor crc":           func(b []byte) []byte { b[skcFirstDescriptor(b)+4] ^= 1; return b },
		"descriptor compressed":    func(b []byte) []byte { b[skcFirstDescriptor(b)+8]++; return b },
		"descriptor expanded":      func(b []byte) []byte { b[skcFirstDescriptor(b)+12]++; return b },
		"actual crc":               func(b []byte) []byte { b[skcFirstDescriptor(b)+4] ^= 1; b[directoryPosition(b)+16] ^= 1; return b },
		"local crc nonzero":        func(b []byte) []byte { b[14] = 1; return b },
		"local compressed nonzero": func(b []byte) []byte { b[18] = 1; return b },
		"local expanded zero":      func(b []byte) []byte { binary.LittleEndian.PutUint32(b[22:], 0); return b },
		"local expanded conflict":  func(b []byte) []byte { b[22]++; return b },
		"local sentinel":           func(b []byte) []byte { binary.LittleEndian.PutUint32(b[22:], 0xffffffff); return b },
		"central sentinel": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+24:], 0xffffffff)
			return b
		},
		"local needed":   func(b []byte) []byte { binary.LittleEndian.PutUint16(b[4:], 45); return b },
		"central needed": func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+6:], 45); return b },
		"UTF8 flag with IFC4 extensions": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 0x808)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 0x808)
			return b
		},
		"encrypted": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 9)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 9)
			return b
		},
		"stored": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[8:], 0)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+10:], 0)
			return b
		},
		"missing descriptor flag": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 0)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 0)
			return b
		},
		"missing extra": func(b []byte) []byte { return skcReplaceFirstExtra(t, b, nil) },
		"truncated extra": func(b []byte) []byte {
			at := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			return skcReplaceFirstExtra(t, b, bytes.Clone(b[at:at+31]))
		},
		"duplicate extra": func(b []byte) []byte {
			at := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			e := bytes.Clone(b[at : at+32])
			return skcReplaceFirstExtra(t, b, append(e, e...))
		},
		"local metadata mismatch": func(b []byte) []byte { b[30+int(binary.LittleEndian.Uint16(b[26:]))+5]++; return b },
		"central metadata mismatch": func(b []byte) []byte {
			p := directoryPosition(b)
			b[p+46+int(binary.LittleEndian.Uint16(b[p+28:]))+23]++
			return b
		},
		"damaged stream": func(b []byte) []byte {
			b[skcFirstDescriptor(b)-int(binary.LittleEndian.Uint32(b[directoryPosition(b)+20:]))] = 0xff
			return b
		},
		"overlap": func(b []byte) []byte {
			p := directoryPosition(b)
			p += 46 + int(binary.LittleEndian.Uint16(b[p+28:])) + 32
			binary.LittleEndian.PutUint32(b[p+42:], binary.LittleEndian.Uint32(b[p+42:])-1)
			return b
		},
		"gap": func(b []byte) []byte {
			p := directoryPosition(b)
			p += 46 + int(binary.LittleEndian.Uint16(b[p+28:])) + 32
			binary.LittleEndian.PutUint32(b[p+42:], binary.LittleEndian.Uint32(b[p+42:])+1)
			return b
		},
		"directory descriptor": func(b []byte) []byte {
			p := directoryPosition(b)
			p += 46 + int(binary.LittleEndian.Uint16(b[p+28:])) + 32
			offset := int(binary.LittleEndian.Uint32(b[p+42:]))
			binary.LittleEndian.PutUint16(b[p+8:], 8)
			binary.LittleEndian.PutUint16(b[offset+6:], 8)
			return b
		},
		"multiple disks":    func(b []byte) []byte { b[len(b)-18] = 1; return b },
		"truncated archive": func(b []byte) []byte { return b[:len(b)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			raw := mutate(skcDescriptorFixture(t))
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits()); err == nil || got != nil {
				t.Fatal("invalid descriptor profile accepted or partial facts returned")
			}
		})
	}
	// Mutate each structural byte on BOTH sides, so equality alone cannot
	// accidentally legitimize an unknown or malformed extension layout.
	for _, at := range []int{0, 1, 2, 3, 4, 17, 18, 19, 20, 21, 22, 27} {
		t.Run(string(rune('A'+at)), func(t *testing.T) {
			raw := skcDescriptorFixture(t)
			p := directoryPosition(raw)
			raw[30+int(binary.LittleEndian.Uint16(raw[26:]))+at] ^= 1
			raw[p+46+int(binary.LittleEndian.Uint16(raw[p+28:]))+at] ^= 1
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits()); err == nil || got != nil {
				t.Fatal("unknown metadata structure accepted")
			}
		})
	}
}

func TestVFFSKCSignedDescriptorBudgetsAndCancellation(t *testing.T) {
	raw := skcDescriptorFixture(t)
	for name, change := range map[string]func(*vffClassificationPackageLimits){
		"read":     func(l *vffClassificationPackageLimits) { l.readBytes = 1 },
		"expanded": func(l *vffClassificationPackageLimits) { l.expandedBytes = 1 },
		"member":   func(l *vffClassificationPackageLimits) { l.memberBytes = 1 },
		"count":    func(l *vffClassificationPackageLimits) { l.members = 2 },
	} {
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
