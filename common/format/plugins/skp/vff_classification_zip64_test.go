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

// Authored controls reuse the normal ZIP writer; only the first member's local
// extra is replaced. Central offsets and the ordinary EOCD remain consistent.
func skcReplaceFirstExtra(t *testing.T, raw, extra []byte) []byte {
	t.Helper()
	at := 30 + int(binary.LittleEndian.Uint16(raw[26:]))
	old := int(binary.LittleEndian.Uint16(raw[28:]))
	directory := directoryPosition(raw)
	delta := len(extra) - old
	out := append(append(bytes.Clone(raw[:at]), extra...), raw[at+old:]...)
	binary.LittleEndian.PutUint16(out[28:], uint16(len(extra)))
	for p := directory + delta; p < len(out)-22; {
		h := out[p:]
		offset := binary.LittleEndian.Uint32(h[42:])
		if offset != 0 {
			binary.LittleEndian.PutUint32(h[42:], uint32(int(offset)+delta))
		}
		p += 46 + int(binary.LittleEndian.Uint16(h[28:])) + int(binary.LittleEndian.Uint16(h[30:])) + int(binary.LittleEndian.Uint16(h[32:]))
	}
	binary.LittleEndian.PutUint32(out[len(out)-6:], uint32(directory+delta))
	return out
}

func skcLocalZIP64Fixture(t *testing.T, ntfs bool) []byte {
	t.Helper()
	raw := skcTestZIP(t, ntfs,
		vffTestMember{"document.xml", []byte("<classification/>"), zip.Deflate},
		vffTestMember{"Schemas/a.xsd", []byte("<schema/>"), zip.Store})
	h := raw[directoryPosition(raw):]
	extra := make([]byte, 20)
	binary.LittleEndian.PutUint16(extra, 1)
	binary.LittleEndian.PutUint16(extra[2:], 16)
	binary.LittleEndian.PutUint64(extra[4:], uint64(binary.LittleEndian.Uint32(h[24:])))
	binary.LittleEndian.PutUint64(extra[12:], uint64(binary.LittleEndian.Uint32(h[20:])))
	raw = skcReplaceFirstExtra(t, raw, extra)
	h = raw[directoryPosition(raw):]
	binary.LittleEndian.PutUint16(h[6:], 45)
	binary.LittleEndian.PutUint16(h[8:], 0)
	binary.LittleEndian.PutUint16(raw[4:], 45)
	binary.LittleEndian.PutUint16(raw[6:], 0)
	binary.LittleEndian.PutUint32(raw[18:], 0xffffffff)
	binary.LittleEndian.PutUint32(raw[22:], 0xffffffff)
	return raw
}

func TestVFFSKCLocalZIP64ExactSizesAndProfileSeparation(t *testing.T) {
	raw := skcLocalZIP64Fixture(t, false)
	original := bytes.Clone(raw)
	got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.packages) != 1 || got.packages[0].sha256 != sha256.Sum256(raw) || len(got.packages[0].files) != 2 || string(got.packages[0].files["document.xml"]) != "<classification/>" || string(got.packages[0].files["Schemas/a.xsd"]) != "<schema/>" || !bytes.Equal(raw, original) {
		t.Fatal("mixed ordinary/local-ZIP64 source identity or bytes changed")
	}
	// The same archive must not broaden the outer VFF profile.
	source := &vffSource{ctx: context.Background(), input: bytes.NewReader(raw), size: int64(len(raw)), remaining: 1 << 20}
	if records, _, err := readVFFDirectory(source, 0, vffTestLimits(), vffModelZIP); err == nil || records != nil {
		t.Fatal("SKC active ZIP64 escaped to model profile")
	}
}

func TestVFFSKCLocalZIP64RejectsConflictsAndUnsupportedVariants(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"expanded conflict":   func(b []byte) []byte { p := 30 + int(binary.LittleEndian.Uint16(b[26:])); b[p+4]++; return b },
		"compressed conflict": func(b []byte) []byte { p := 30 + int(binary.LittleEndian.Uint16(b[26:])); b[p+12]++; return b },
		"uint64 overflow": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			binary.LittleEndian.PutUint64(b[p+4:], uint64(1)<<63)
			return b
		},
		"single sentinel": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[18:], binary.LittleEndian.Uint32(b[directoryPosition(b)+20:]))
			return b
		},
		"local version":   func(b []byte) []byte { binary.LittleEndian.PutUint16(b[4:], 20); return b },
		"central version": func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+6:], 20); return b },
		"unknown version": func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+6:], 46); return b },
		"extra id":        func(b []byte) []byte { p := 30 + int(binary.LittleEndian.Uint16(b[26:])); b[p] = 0x55; return b },
		"extra declared length": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			binary.LittleEndian.PutUint16(b[p+2:], 8)
			return b
		},
		"truncated extra": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			return skcReplaceFirstExtra(t, b, bytes.Clone(b[p:p+19]))
		},
		"missing size": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			return skcReplaceFirstExtra(t, b, bytes.Clone(b[p:p+12]))
		},
		"duplicate extra": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			e := bytes.Clone(b[p : p+20])
			return skcReplaceFirstExtra(t, b, append(e, e...))
		},
		"unknown additional extra": func(b []byte) []byte {
			p := 30 + int(binary.LittleEndian.Uint16(b[26:]))
			e := bytes.Clone(b[p : p+20])
			return skcReplaceFirstExtra(t, b, append(e, 0x55, 0x54, 0, 0))
		},
		"absent extra":  func(b []byte) []byte { return skcReplaceFirstExtra(t, b, nil) },
		"central extra": func(_ []byte) []byte { return skcLocalZIP64Fixture(t, true) },
		"central size sentinel": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+24:], 0xffffffff)
			return b
		},
		"central offset sentinel": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+42:], 0xffffffff)
			return b
		},
		"encryption": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 1)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 1)
			return b
		},
		"descriptor": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 8)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 8)
			return b
		},
		"utf8 flag outside subset": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[6:], 0x800)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 0x800)
			return b
		},
		"stored outside subset": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[8:], 0)
			binary.LittleEndian.PutUint16(b[directoryPosition(b)+10:], 0)
			return b
		},
		"directory": func(b []byte) []byte {
			p := directoryPosition(b) + 38
			binary.LittleEndian.PutUint32(b[p:], binary.LittleEndian.Uint32(b[p:])|0x10)
			return b
		},
		"local name":     func(b []byte) []byte { b[30] = 'X'; return b },
		"local crc":      func(b []byte) []byte { b[14] ^= 1; return b },
		"actual crc":     func(b []byte) []byte { b[14] ^= 1; b[directoryPosition(b)+16] ^= 1; return b },
		"damaged stream": func(b []byte) []byte { p := 30 + int(binary.LittleEndian.Uint16(b[26:])) + 20; b[p] = 0xff; return b },
		"overlap": func(b []byte) []byte {
			p := directoryPosition(b)
			p += 46 + int(binary.LittleEndian.Uint16(b[p+28:]))
			binary.LittleEndian.PutUint32(b[p+42:], binary.LittleEndian.Uint32(b[p+42:])-1)
			return b
		},
		"gap": func(b []byte) []byte {
			p := directoryPosition(b)
			p += 46 + int(binary.LittleEndian.Uint16(b[p+28:]))
			binary.LittleEndian.PutUint32(b[p+42:], binary.LittleEndian.Uint32(b[p+42:])+1)
			return b
		},
		"multiple disks": func(b []byte) []byte { binary.LittleEndian.PutUint16(b[len(b)-18:], 1); return b },
		"ZIP64 directory end": func(b []byte) []byte {
			e := make([]byte, 76)
			binary.LittleEndian.PutUint32(e, 0x06064b50)
			binary.LittleEndian.PutUint64(e[4:], 44)
			binary.LittleEndian.PutUint32(e[56:], 0x07064b50)
			return append(append(bytes.Clone(b[:len(b)-22]), e...), b[len(b)-22:]...)
		},
		"truncated archive": func(b []byte) []byte { return b[:len(b)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			raw := mutate(skcLocalZIP64Fixture(t, false))
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), skcTestLimits()); err == nil || got != nil {
				t.Fatal("invalid or unsupported local ZIP64 accepted or partial facts returned")
			}
		})
	}
}

func TestVFFSKCLocalZIP64RetainsBudgetsAndCancellation(t *testing.T) {
	raw := skcLocalZIP64Fixture(t, false)
	for name, change := range map[string]func(*vffClassificationPackageLimits){
		"expanded": func(l *vffClassificationPackageLimits) { l.expandedBytes = 1 },
		"member":   func(l *vffClassificationPackageLimits) { l.memberBytes = 1 },
		"reads":    func(l *vffClassificationPackageLimits) { l.readBytes = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			l := skcTestLimits()
			change(&l)
			if got, err := readVFFClassificationPackages(context.Background(), skcControl(t, raw), l); err == nil || got != nil {
				t.Fatal("ZIP64 bypassed source/expansion budget")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := readVFFClassificationPackages(ctx, skcControl(t, raw), skcTestLimits()); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("cancellation changed: %v", err)
	}
}
