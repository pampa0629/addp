package skp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"path"
	"sort"
	"strings"
)

type vffSKCSystemMetadataFact struct {
	vffSKCMemberFact
	target string
}

// Recognition only: unknown AppleDouble layouts stay unclaimed. ZIP integrity
// and all cumulative budgets have already been checked by the package reader.
func vffSKCSystemMetadata(item vffClassificationPackage) []vffSKCSystemMetadataFact {
	var facts []vffSKCSystemMetadataFact
	for name, raw := range item.files {
		if !strings.HasPrefix(name, "__MACOSX/") || !strings.HasPrefix(path.Base(name), "._") {
			continue
		}
		relative := strings.TrimPrefix(name, "__MACOSX/")
		target := strings.TrimPrefix(path.Base(relative), "._")
		if target == "" || target == "." || target == ".." {
			continue
		}
		if dir := path.Dir(relative); dir != "." {
			target = dir + "/" + target
		}
		if strings.HasPrefix(target, "__MACOSX/") {
			continue
		}
		_, paired := item.files[target]
		for _, dir := range item.directories {
			if dir == target+"/" {
				paired = true
				break
			}
		}
		if !paired || !vffQuarantineAppleDouble(raw) {
			continue
		}
		facts = append(facts, vffSKCSystemMetadataFact{vffSKCMemberFact{name, sha256.Sum256(raw)}, target})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].member < facts[j].member })
	return facts
}

func vffQuarantineAppleDouble(raw []byte) bool {
	// Apple's packed header: two entries, 32 Finder bytes, alignment padding,
	// one ATTR header and one aligned attribute entry named quarantine.
	if len(raw) < 152 {
		return false
	}
	u32 := func(at int) uint32 { return binary.BigEndian.Uint32(raw[at:]) }
	u16 := func(at int) uint16 { return binary.BigEndian.Uint16(raw[at:]) }
	size := uint64(len(raw))
	if u32(0) != 0x00051607 || u32(4) != 0x00020000 || string(raw[8:24]) != "Mac OS X        " || u16(24) != 2 ||
		u32(26) != 9 || u32(30) != 50 || uint64(u32(34)) != size-50 || u32(38) != 2 || uint64(u32(42)) != size || u32(46) != 0 {
		return false
	}
	if !bytes.Equal(raw[50:84], make([]byte, 34)) || u32(84) != 0x41545452 || u32(88) != 0 || uint64(u32(92)) != size ||
		u32(96) != 152 || uint64(u32(100)) != size-152 || !bytes.Equal(raw[104:118], make([]byte, 14)) || u16(118) != 1 {
		return false
	}
	return u32(120) == 152 && uint64(u32(124)) == size-152 && u16(128) == 0 && raw[130] == 21 && string(raw[131:152]) == "com.apple.quarantine\x00"
}
