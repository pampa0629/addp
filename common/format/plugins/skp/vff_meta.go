package skp

// Partial known-role reference facts, not metadata or resource closure admission.
import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type vffMetaLimits struct{ bytes, records, depth int }
type vffMetaImageReference struct {
	field  string
	offset int // 2A23 record header within the metadata member
	vffImageReference
}
type vffMetaFacts struct {
	member                  string
	modelSHA256, metaSHA256 [32]byte
	images                  []vffMetaImageReference
	opaque                  []vffOpaqueRecord
}

func readVFFMetaReferences(ctx context.Context, container *vffContainer, limits vffMetaLimits) (*vffMetaFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if container == nil || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF metadata requires explicit positive budgets")
	}
	model := container.members["model.dat"]
	if len(model) == 0 || len(model) > limits.bytes {
		return nil, fmt.Errorf("VFF metadata model byte budget exceeded or empty")
	}
	r := &vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}
	result := &vffMetaFacts{modelSHA256: sha256.Sum256(model)}
	root, entry := 0, 0
	err := r.each(model, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "F401" {
			return fmt.Errorf("VFF metadata requires one model root")
		}
		return r.each(data, 1, offset, func(tag string, data []byte, offset int) error {
			if tag != "0A02" {
				result.opaque = append(result.opaque, vffOpaque("model.dat", "F401/"+tag, data, offset-6))
				return nil
			}
			entry++
			if entry != 1 {
				return fmt.Errorf("duplicate VFF metadata entry")
			}
			if err := vffMemberName(data, 0x800); err != nil {
				return err
			}
			result.member = string(data)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if root != 1 || entry != 1 {
		return nil, fmt.Errorf("missing VFF metadata model entry")
	}
	meta, exists := container.members[result.member]
	if !exists || len(meta) == 0 {
		return nil, fmt.Errorf("missing exact VFF metadata member")
	}
	if len(meta) > limits.bytes-len(model) {
		return nil, fmt.Errorf("VFF cumulative model and metadata byte budget exceeded")
	}
	result.metaSHA256 = sha256.Sum256(meta)
	usedBytes := len(model) + len(meta)
	imageDigests := map[string][32]byte{}
	root = 0
	seen := map[string]bool{}
	err = r.each(meta, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "6400" {
			return fmt.Errorf("VFF metadata requires one metadata root")
		}
		return r.each(data, 1, offset, func(tag string, data []byte, offset int) error {
			if tag != "6700" && tag != "6800" {
				result.opaque = append(result.opaque, vffOpaque(result.member, "6400/"+tag, data, offset-6))
				return nil
			}
			if seen[tag] {
				return fmt.Errorf("duplicate VFF metadata image field")
			}
			seen[tag] = true
			if len(data) == 0 {
				return nil
			}
			ref, err := readVFFMetaImage(r, data, offset)
			if err != nil {
				return err
			}
			target, exists := container.members[ref.member]
			if !exists || len(target) == 0 {
				return fmt.Errorf("missing exact VFF metadata image member")
			}
			if ref.member == "model.dat" || ref.member == result.member {
				return fmt.Errorf("VFF metadata image aliases binary input")
			}
			digest, hashed := imageDigests[ref.member]
			if !hashed {
				if len(target) > limits.bytes-usedBytes {
					return fmt.Errorf("VFF cumulative reference byte budget exceeded")
				}
				usedBytes += len(target)
				digest = sha256.Sum256(target)
				imageDigests[ref.member] = digest
			}
			ref.sha256 = digest
			result.images = append(result.images, vffMetaImageReference{field: tag, offset: ref.offset, vffImageReference: ref.vffImageReference})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if root != 1 || !seen["6700"] || !seen["6800"] {
		return nil, fmt.Errorf("missing VFF metadata image fields")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func readVFFMetaImage(r *vffRecords, raw []byte, base int) (vffMetaImageReference, error) {
	var result vffMetaImageReference
	wrappers := 0
	err := r.each(raw, 2, base, func(tag string, data []byte, offset int) error {
		wrappers++
		if tag != "2823" || wrappers != 1 {
			return fmt.Errorf("unsupported VFF metadata image wrapper")
		}
		seen := map[string]bool{}
		err := r.each(data, 3, offset, func(tag string, data []byte, offset int) error {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF metadata image value")
			}
			seen[tag] = true
			switch tag {
			case "2923":
				if len(data) != 4 || binary.LittleEndian.Uint32(data) != 4 {
					return fmt.Errorf("unsupported VFF metadata image value type")
				}
			case "2A23":
				if err := vffMemberName(data, 0x800); err != nil {
					return err
				}
				result.member, result.offset = string(data), offset-6
			default:
				return fmt.Errorf("unexplained VFF metadata image value")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !seen["2923"] || !seen["2A23"] {
			return fmt.Errorf("incomplete VFF metadata image value")
		}
		return nil
	})
	if err == nil && wrappers != 1 {
		err = fmt.Errorf("missing VFF metadata image wrapper")
	}
	return result, err
}
