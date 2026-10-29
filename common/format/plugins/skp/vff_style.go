package skp

// Observed binary catalog links and XML byte identity only. No complete style,
// scene association, active/selected behavior or composition admission claim.
import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

type vffStyleLimits struct{ bytes, records, depth int }
type vffStyleEntry struct {
	branch, folder, member string
	id                     uint64   // only catalog entries carry an entity ID
	token                  [16]byte // retained raw, never used to merge entries or called a GUID
	offset                 int      // 6C6B record header inside model.dat
	xmlSHA256              [32]byte
}
type vffStyleFacts struct {
	modelSHA256     [32]byte
	entries         []vffStyleEntry
	reference       uint64 // 7A69, points into 7969; no scene-specific role inference
	referenceOffset int
	state           byte // observed 7C69 0/1 only, no rendering behavior inferred
	opaque          []vffOpaqueRecord
}
type vffStyleReader struct {
	*vffRecords
	budget  *vffReferenceBudget
	result  *vffStyleFacts
	ids     map[uint64]bool
	members map[string]bool
}

func readVFFStyleCatalog(ctx context.Context, c *vffContainer, limits vffStyleLimits) (*vffStyleFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF styles require explicit positive budgets")
	}
	model := c.members["model.dat"]
	if len(model) == 0 || len(model) > limits.bytes {
		return nil, fmt.Errorf("VFF style model byte budget exceeded or empty")
	}
	r := &vffStyleReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}, budget: &vffReferenceBudget{container: c, used: len(model), limit: limits.bytes, digests: map[string][32]byte{}}, result: &vffStyleFacts{modelSHA256: sha256.Sum256(model)}, ids: map[uint64]bool{}, members: map[string]bool{}}
	root := 0
	err := r.each(model, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "F401" {
			return fmt.Errorf("VFF styles require one model root")
		}
		return r.walk(data, "F401", 1, offset)
	})
	if err != nil {
		return nil, err
	}
	if root != 1 || !r.ids[r.result.reference] {
		return nil, fmt.Errorf("missing or dangling VFF style catalog reference")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.result, nil
}

func (r *vffStyleReader) walk(raw []byte, role string, depth, base int) error {
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		where := role + "/" + tag
		if role == "F401/0602/7869/7969" || role == "F401/0602/7869/7B69" {
			if tag != "6C6B" {
				return fmt.Errorf("unsupported VFF style entry wrapper")
			}
			if role == "F401/0602/7869/7B69" && seen[tag] {
				return fmt.Errorf("duplicate VFF independent style entry")
			}
			seen[tag] = true
			return r.entry(data, strings.TrimPrefix(role, "F401/0602/7869/"), depth+1, offset)
		}
		if role != "F401" || tag == "0602" {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF style structural field")
			}
			seen[tag] = true
		}
		switch role {
		case "F401":
			if tag == "0602" {
				return r.walk(data, where, depth+1, offset)
			}
		case "F401/0602":
			if tag != "7869" {
				return fmt.Errorf("unsupported VFF style container")
			}
			return r.walk(data, where, depth+1, offset)
		case "F401/0602/7869":
			switch tag {
			case "7969", "7B69":
				return r.walk(data, where, depth+1, offset)
			case "7A69":
				id, err := vffRecordID(data)
				if err != nil {
					return err
				}
				r.result.reference = id
				r.result.referenceOffset = offset - 6
				return nil
			case "7C69":
				if len(data) != 1 || data[0] > 1 {
					return fmt.Errorf("unsupported VFF style state encoding")
				}
				r.result.state = data[0]
				return nil
			default:
				return fmt.Errorf("unexplained VFF style catalog field")
			}
		}
		r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", where, data, offset-6))
		return nil
	})
	if err != nil {
		return err
	}
	required := map[string][]string{"F401": {"0602"}, "F401/0602": {"7869"}, "F401/0602/7869": {"7969", "7A69", "7B69", "7C69"}, "F401/0602/7869/7969": {"6C6B"}, "F401/0602/7869/7B69": {"6C6B"}}
	for _, tag := range required[role] {
		if !seen[tag] {
			return fmt.Errorf("missing VFF style structural field")
		}
	}
	return nil
}

func (r *vffStyleReader) entry(raw []byte, branch string, depth, base int) error {
	e := vffStyleEntry{branch: branch, offset: base - 6}
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if tag == "DC05" || tag == "6D6B" || tag == "6F6B" {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF style identity field")
			}
			seen[tag] = true
		}
		switch tag {
		case "DC05":
			if branch != "7969" {
				return fmt.Errorf("unsupported entity ID in independent VFF style record")
			}
			id, err := r.entityID(data, depth+1, offset)
			if err != nil {
				return err
			}
			e.id = id
		case "6D6B":
			if len(data) != 16 {
				return fmt.Errorf("unsupported VFF style token size")
			}
			copy(e.token[:], data)
		case "6F6B":
			if err := vffMemberName(data, 0x800); err != nil {
				return err
			}
			if strings.Contains(string(data), "/") {
				return fmt.Errorf("VFF style folder must be a single literal component")
			}
			e.folder = string(data)
		default:
			r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "F401/0602/7869/"+branch+"/6C6B/"+tag, data, offset-6))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !seen["6D6B"] || !seen["6F6B"] || branch == "7969" && !seen["DC05"] {
		return fmt.Errorf("incomplete VFF style identity")
	}
	if e.id != 0 && r.ids[e.id] {
		return fmt.Errorf("duplicate VFF style entity ID")
	}
	e.member = "styles/" + e.folder + "/style.xml"
	if r.members[e.member] {
		return fmt.Errorf("ambiguous VFF style XML member ownership")
	}
	digest, err := r.budget.digest(e.member)
	if err != nil {
		return err
	}
	e.xmlSHA256 = digest
	if e.id != 0 {
		r.ids[e.id] = true
	}
	r.members[e.member] = true
	r.result.entries = append(r.result.entries, e)
	return nil
}
