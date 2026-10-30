package skp

// Scene identity and observed structural candidate joins only. Flag behavior,
// rendering and inheritance remain unproved; this is not composition admission.
import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

type vffSceneStyleLimits struct{ bytes, records, depth int }
type vffSceneStyleCandidate struct {
	id                 uint64
	name               string
	flags              uint32 // raw 4971, no bit-to-behavior inference
	offset             int    // 4871 header within model.dat
	candidateID        uint64 // absent 4C71 stays zero; never falls back to model 7A69
	candidateOffset    int
	candidateMember    string
	candidateXMLSHA256 [32]byte
	thumbnail          *vffSceneThumbnail
	rendering          *vffSceneRendering
}
type vffSceneThumbnail struct {
	fieldOffset, pathOffset int // 5871 and 3421 headers within model.dat
	vffImageReference
}
type vffSceneRendering struct {
	offset int // 4D71 header; values retain opaque payload identities, not meanings
	values []vffOpaqueRecord
}
type vffSceneStyleFacts struct {
	modelSHA256 [32]byte
	scenes      []vffSceneStyleCandidate
	opaque      []vffOpaqueRecord
}
type vffSceneStyleReader struct {
	*vffRecords
	budget  *vffReferenceBudget
	catalog map[uint64]vffStyleEntry
	ids     map[uint64]bool
	result  *vffSceneStyleFacts
}

func readVFFSceneStyleCandidates(ctx context.Context, c *vffContainer, catalog *vffStyleFacts, limits vffSceneStyleLimits) (*vffSceneStyleFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || catalog == nil || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF scene style candidates require catalog facts and explicit positive budgets")
	}
	model := c.members["model.dat"]
	if len(model) == 0 || len(model) > limits.bytes || sha256.Sum256(model) != catalog.modelSHA256 {
		return nil, fmt.Errorf("VFF scene model facts are stale or exceed budget")
	}
	r := &vffSceneStyleReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}, budget: &vffReferenceBudget{container: c, used: len(model), limit: limits.bytes, digests: map[string][32]byte{}}, catalog: map[uint64]vffStyleEntry{}, ids: map[uint64]bool{}, result: &vffSceneStyleFacts{modelSHA256: catalog.modelSHA256}}
	for _, entry := range catalog.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.branch != "7969" {
			continue
		}
		if entry.id == 0 || entry.member != "styles/"+entry.folder+"/style.xml" {
			return nil, fmt.Errorf("invalid VFF scene catalog facts")
		}
		if err := vffMemberName([]byte(entry.folder), 0x800); err != nil {
			return nil, err
		}
		if strings.Contains(entry.folder, "/") {
			return nil, fmt.Errorf("invalid VFF scene catalog folder")
		}
		if _, exists := r.catalog[entry.id]; exists {
			return nil, fmt.Errorf("duplicate VFF scene catalog ID")
		}
		r.catalog[entry.id] = entry
	}
	if len(r.catalog) == 0 {
		return nil, fmt.Errorf("empty VFF scene style catalog facts")
	}
	root := 0
	err := r.each(model, 0, 0, func(tag string, data []byte, offset int) error {
		root++
		if root != 1 || tag != "F401" {
			return fmt.Errorf("VFF scenes require one model root")
		}
		return r.walk(data, "F401", 1, offset)
	})
	if err != nil {
		return nil, err
	}
	if root != 1 {
		return nil, fmt.Errorf("missing VFF scene model root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.result, nil
}

func (r *vffSceneStyleReader) walk(raw []byte, role string, depth, base int) error {
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		where := role + "/" + tag
		next := ""
		switch role {
		case "F401":
			if tag == "0702" {
				next = where
			}
		case "F401/0702":
			if tag != "606D" {
				return fmt.Errorf("unsupported VFF scene container")
			}
			next = where
		case "F401/0702/606D":
			if tag == "616D" {
				next = where
			}
		case "F401/0702/606D/616D":
			if tag != "4871" {
				return fmt.Errorf("unsupported VFF scene entry wrapper")
			}
			return r.scene(data, depth+1, offset)
		}
		if next != "" {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF scene structural wrapper")
			}
			seen[tag] = true
			return r.walk(data, next, depth+1, offset)
		}
		r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", where, data, offset-6))
		return nil
	})
	if err != nil {
		return err
	}
	required := map[string]string{"F401": "0702", "F401/0702": "606D", "F401/0702/606D": "616D"}
	if tag := required[role]; tag != "" && !seen[tag] {
		return fmt.Errorf("missing explicit VFF scene list")
	}
	return nil
}

func (r *vffSceneStyleReader) scene(raw []byte, depth, base int) error {
	s := vffSceneStyleCandidate{offset: base - 6}
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if tag == "546F" || tag == "4971" || tag == "4C71" || tag == "5871" || tag == "4D71" {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF scene identity or candidate field")
			}
			seen[tag] = true
		}
		switch tag {
		case "546F":
			fields := map[string]bool{}
			err := r.each(data, depth+1, offset, func(tag string, data []byte, offset int) error {
				if tag == "DC05" || tag == "556F" {
					if fields[tag] {
						return fmt.Errorf("duplicate VFF scene header identity")
					}
					fields[tag] = true
				}
				switch tag {
				case "DC05":
					id, err := r.entityID(data, depth+2, offset)
					if err != nil {
						return err
					}
					s.id = id
				case "556F":
					if !utf8.Valid(data) || len(data) == 0 || strings.ContainsRune(string(data), 0) {
						return fmt.Errorf("invalid VFF scene display name")
					}
					s.name = string(data)
				default:
					r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "F401/0702/606D/616D/4871/546F/"+tag, data, offset-6))
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !fields["DC05"] || !fields["556F"] {
				return fmt.Errorf("incomplete VFF scene header identity")
			}
		case "4971":
			if len(data) != 4 {
				return fmt.Errorf("invalid VFF raw scene flag width")
			}
			s.flags = binary.LittleEndian.Uint32(data)
		case "4C71":
			id, err := vffRecordID(data)
			if err != nil {
				return err
			}
			s.candidateID = id
			s.candidateOffset = offset - 6
		case "5871":
			thumbnail, err := r.thumbnail(data, depth+1, offset)
			if err != nil {
				return err
			}
			s.thumbnail = thumbnail
		case "4D71":
			rendering, err := r.rendering(data, depth+1, offset)
			if err != nil {
				return err
			}
			s.rendering = rendering
			// Framing does not prove rendering semantics or absence of resources.
			r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "F401/0702/606D/616D/4871/4D71", data, offset-6))
		default:
			r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "F401/0702/606D/616D/4871/"+tag, data, offset-6))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !seen["546F"] || !seen["4971"] {
		return fmt.Errorf("missing VFF scene header or raw flags")
	}
	if r.ids[s.id] {
		return fmt.Errorf("duplicate VFF scene entity ID")
	}
	r.ids[s.id] = true
	if seen["4C71"] {
		entry, ok := r.catalog[s.candidateID]
		if !ok {
			return fmt.Errorf("dangling VFF scene style candidate")
		}
		digest, err := r.budget.digest(entry.member)
		if err != nil {
			return err
		}
		if digest != entry.xmlSHA256 {
			return fmt.Errorf("stale VFF scene candidate XML facts")
		}
		s.candidateMember, s.candidateXMLSHA256 = entry.member, digest
	}
	r.result.scenes = append(r.result.scenes, s)
	return nil
}

func (r *vffSceneStyleReader) thumbnail(raw []byte, depth, base int) (*vffSceneThumbnail, error) {
	ref := &vffSceneThumbnail{fieldOffset: base - 6}
	count := 0
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		count++
		if tag != "3421" || count != 1 {
			return fmt.Errorf("unsupported or duplicate VFF scene thumbnail path")
		}
		if err := vffMemberName(data, 0x800); err != nil {
			return err
		}
		ref.member, ref.pathOffset = string(data), offset-6
		if ref.member == "model.dat" {
			return fmt.Errorf("VFF scene thumbnail aliases model input")
		}
		digest, err := r.budget.digest(ref.member)
		if err != nil {
			return err
		}
		ref.sha256 = digest
		return nil
	})
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("missing explicit VFF scene thumbnail path")
	}
	return ref, nil
}

func (r *vffSceneStyleReader) rendering(raw []byte, depth, base int) (*vffSceneRendering, error) {
	facts := &vffSceneRendering{offset: base - 6}
	count := 0
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		count++
		if tag != "3C73" || count != 1 {
			return fmt.Errorf("unsupported or duplicate VFF scene rendering wrapper")
		}
		seen := map[string]bool{}
		return r.each(data, depth+1, offset, func(tag string, data []byte, offset int) error {
			if seen[tag] {
				return fmt.Errorf("duplicate VFF scene rendering value")
			}
			seen[tag] = true
			facts.values = append(facts.values, vffOpaque("model.dat", "F401/0702/606D/616D/4871/4D71/3C73/"+tag, data, offset-6))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("missing VFF scene rendering wrapper")
	}
	return facts, nil
}
