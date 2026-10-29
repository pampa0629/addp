package skp

// Known binary image references only; no style/rendering or resource closure claim.
import (
	"context"
	"crypto/sha256"
	"fmt"
)

type vffWatermarkLimits struct{ bytes, records, depth int }
type vffWatermarkImageReference struct {
	entryOffset, offset int // E02E and 2A23 record headers within model.dat
	vffImageReference
}
type vffWatermarkFacts struct {
	modelSHA256 [32]byte
	images      []vffWatermarkImageReference
	opaque      []vffOpaqueRecord
}
type vffWatermarkReader struct {
	*vffRecords
	budget *vffReferenceBudget
	result *vffWatermarkFacts
}

func readVFFWatermarkReferences(ctx context.Context, c *vffContainer, limits vffWatermarkLimits) (*vffWatermarkFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF watermarks require explicit positive budgets")
	}
	model := c.members["model.dat"]
	if len(model) == 0 || len(model) > limits.bytes {
		return nil, fmt.Errorf("VFF watermark model byte budget exceeded or empty")
	}
	r := &vffWatermarkReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}, budget: &vffReferenceBudget{container: c, used: len(model), limit: limits.bytes, digests: map[string][32]byte{}}, result: &vffWatermarkFacts{modelSHA256: sha256.Sum256(model)}}
	root := 0
	err := r.each(model, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "F401" {
			return fmt.Errorf("VFF watermarks require one model root")
		}
		return r.walk(data, "F401", 1, offset)
	})
	if err != nil {
		return nil, err
	}
	if root != 1 {
		return nil, fmt.Errorf("missing VFF watermark model root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.result, nil
}

func (r *vffWatermarkReader) walk(raw []byte, role string, depth, base int) error {
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		where := role + "/" + tag
		next := ""
		switch role {
		case "F401":
			if tag == "0302" {
				next = "F401/0302"
			}
		case "F401/0302":
			if tag != "EC2C" {
				return fmt.Errorf("unsupported VFF watermark container")
			}
			next = where
		case "F401/0302/EC2C":
			if tag == "ED2C" {
				next = where
			} else if tag != "EE2C" {
				return fmt.Errorf("unsupported VFF watermark list field")
			}
		case "F401/0302/EC2C/ED2C":
			if tag != "E02E" {
				return fmt.Errorf("unsupported VFF watermark entry")
			}
			return r.entry(data, depth+1, offset)
		}
		if next != "" || role == "F401/0302/EC2C" && tag == "EE2C" {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF watermark wrapper")
			}
			seen[tag] = true
		}
		if next != "" {
			return r.walk(data, next, depth+1, offset)
		}
		r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", where, data, offset-6))
		return nil
	})
	if err != nil {
		return err
	}
	required := map[string][]string{"F401": {"0302"}, "F401/0302": {"EC2C"}, "F401/0302/EC2C": {"ED2C", "EE2C"}}
	for _, tag := range required[role] {
		if !seen[tag] {
			return fmt.Errorf("missing VFF watermark structural field")
		}
	}
	return nil
}

func (r *vffWatermarkReader) entry(raw []byte, depth, base int) error {
	seen := false
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if tag != "E62E" {
			r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "F401/0302/EC2C/ED2C/E02E/"+tag, data, offset-6))
			return nil
		}
		if seen {
			return fmt.Errorf("duplicate VFF watermark image field")
		}
		seen = true
		ref, err := readVFFMemberReference(r.vffRecords, data, depth+1, offset)
		if err != nil {
			return err
		}
		if ref.member == "model.dat" {
			return fmt.Errorf("VFF watermark image aliases model input")
		}
		digest, err := r.budget.digest(ref.member)
		if err != nil {
			return err
		}
		r.result.images = append(r.result.images, vffWatermarkImageReference{entryOffset: base - 6, offset: ref.offset, vffImageReference: vffImageReference{member: ref.member, sha256: digest}})
		return nil
	})
	if err != nil {
		return err
	}
	if !seen {
		return fmt.Errorf("missing VFF watermark image field")
	}
	return nil
}
