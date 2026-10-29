package skp

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type vffMemberReference struct {
	member string
	offset int // 2A23 record header, relative to its source member
}

// The observed typed member value is shared by metadata and watermark records.
// The owner supplies its proven role/depth; this never searches opaque bytes.
func readVFFMemberReference(r *vffRecords, raw []byte, depth, base int) (vffMemberReference, error) {
	var result vffMemberReference
	wrappers := 0
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		wrappers++
		if tag != "2823" || wrappers != 1 {
			return fmt.Errorf("unsupported VFF member reference wrapper")
		}
		seen := map[string]bool{}
		err := r.each(data, depth+1, offset, func(tag string, data []byte, offset int) error {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF member reference value")
			}
			seen[tag] = true
			switch tag {
			case "2923":
				if len(data) != 4 || binary.LittleEndian.Uint32(data) != 4 {
					return fmt.Errorf("unsupported VFF member reference type")
				}
			case "2A23":
				if err := vffMemberName(data, 0x800); err != nil {
					return err
				}
				result.member, result.offset = string(data), offset-6
			default:
				return fmt.Errorf("unexplained VFF member reference value")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !seen["2923"] || !seen["2A23"] {
			return fmt.Errorf("incomplete VFF member reference value")
		}
		return nil
	})
	if err == nil && wrappers != 1 {
		err = fmt.Errorf("missing VFF member reference wrapper")
	}
	return result, err
}

// Counts current target bytes before hashing, once per exact member identity.
// Owner input bytes are already charged and binary input aliases rejected.
type vffReferenceBudget struct {
	container   *vffContainer
	used, limit int
	digests     map[string][32]byte
}

func (b *vffReferenceBudget) digest(member string) ([32]byte, error) {
	if v, ok := b.digests[member]; ok {
		return v, nil
	}
	raw, exists := b.container.members[member]
	if !exists || len(raw) == 0 {
		return [32]byte{}, fmt.Errorf("missing exact VFF referenced member")
	}
	if len(raw) > b.limit-b.used {
		return [32]byte{}, fmt.Errorf("VFF cumulative reference byte budget exceeded")
	}
	b.used += len(raw)
	digest := sha256.Sum256(raw)
	b.digests[member] = digest
	return digest, nil
}
