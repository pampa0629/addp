package skp

// Exact classification member references only. Nested SKC semantics and resource
// closure remain unproved; this reader cannot advertise composition admission.
import (
	"context"
	"crypto/sha256"
	"fmt"
)

type vffClassificationLimits struct{ bytes, records, depth int }
type vffClassificationReference struct {
	entryOffset, pathOffset int // 8467 and 8767 headers in model.dat
	member                  string
	sha256                  [32]byte
}
type vffClassificationFacts struct {
	modelSHA256 [32]byte
	entries     []vffClassificationReference
	opaque      []vffOpaqueRecord
}

func readVFFClassificationReferences(ctx context.Context, c *vffContainer, limits vffClassificationLimits) (*vffClassificationFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF classifications require explicit positive budgets")
	}
	model := c.members["model.dat"]
	if len(model) == 0 || len(model) > limits.bytes {
		return nil, fmt.Errorf("VFF classification model byte budget exceeded or empty")
	}
	r := &vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}
	b := &vffReferenceBudget{container: c, used: len(model), limit: limits.bytes, digests: map[string][32]byte{}}
	result := &vffClassificationFacts{modelSHA256: sha256.Sum256(model)}
	root, groups := 0, 0
	err := r.each(model, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "F401" {
			return fmt.Errorf("VFF classifications require one model root")
		}
		return r.each(data, 1, offset, func(tag string, data []byte, offset int) error {
			if tag != "0502" {
				result.opaque = append(result.opaque, vffOpaque("model.dat", "F401/"+tag, data, offset-6))
				return nil
			}
			groups++
			if groups != 1 {
				return fmt.Errorf("duplicate VFF classification list")
			}
			return r.each(data, 2, offset, func(tag string, data []byte, offset int) error {
				if tag != "8467" {
					return fmt.Errorf("unsupported VFF classification entry")
				}
				ref := vffClassificationReference{entryOffset: offset - 6}
				seen := map[string]bool{}
				err := r.each(data, 3, offset, func(tag string, data []byte, offset int) error {
					if seen[tag] {
						return fmt.Errorf("duplicate VFF classification field")
					}
					seen[tag] = true
					switch tag {
					case "8567", "8667":
						result.opaque = append(result.opaque, vffOpaque("model.dat", "F401/0502/8467/"+tag, data, offset-6))
					case "8767":
						if err := vffMemberName(data, 0x800); err != nil {
							return err
						}
						ref.member, ref.pathOffset = string(data), offset-6
						if ref.member == "model.dat" {
							return fmt.Errorf("VFF classification references model input")
						}
						var err error
						ref.sha256, err = b.digest(ref.member)
						return err
					default:
						return fmt.Errorf("unexplained VFF classification field")
					}
					return nil
				})
				if err != nil {
					return err
				}
				if !seen["8567"] || !seen["8667"] || !seen["8767"] {
					return fmt.Errorf("incomplete VFF classification entry")
				}
				result.entries = append(result.entries, ref)
				return nil
			})
		})
	})
	if err != nil {
		return nil, err
	}
	if root != 1 || groups != 1 {
		return nil, fmt.Errorf("missing explicit VFF classification list")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
