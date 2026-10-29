package skp

// Known-role binding facts only. Opaque records are returned explicitly, so a
// successful join cannot be mistaken for complete model/resource semantics.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

type vffBindingLimits struct{ bytes, records, references, depth int }
type vffBinding struct {
	id                  uint64
	token, member, role string
	layerID             uint64
	offset              int
}
type vffBindingReference struct {
	id     uint64
	kind   string
	offset int
}
type vffOpaqueRecord struct {
	location string
	bytes    int
	offset   int
	sha256   [32]byte
}
type vffBindingFacts struct {
	materials  map[uint64]vffBinding
	layers     map[uint64]uint64
	references []vffBindingReference
	opaque     []vffOpaqueRecord
}
type vffBindingReader struct {
	ctx            context.Context
	limits         vffBindingLimits
	records        int
	members, names map[string]string
	claimed        map[string]bool
	result         *vffBindingFacts
}

func readVFFMaterialBindings(ctx context.Context, container *vffContainer, materials []vffMaterialFact, limits vffBindingLimits) (*vffBindingFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if container == nil || limits.bytes <= 0 || limits.records <= 0 || limits.references <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("VFF bindings require explicit positive model budgets")
	}
	raw := container.members["model.dat"]
	if len(raw) == 0 || len(raw) > limits.bytes {
		return nil, fmt.Errorf("VFF model byte budget exceeded or empty")
	}
	reader := &vffBindingReader{ctx: ctx, limits: limits, members: map[string]string{}, names: map[string]string{}, claimed: map[string]bool{}, result: &vffBindingFacts{materials: map[uint64]vffBinding{}, layers: map[uint64]uint64{}}}
	for _, m := range materials {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parts := strings.Split(m.member, "/")
		if len(parts) != 3 || parts[0] != "materials" || parts[2] != "material.xml" || m.name == "" || reader.names[m.name] != "" || reader.members[parts[1]] != "" {
			return nil, fmt.Errorf("ambiguous VFF XML binding facts")
		}
		rawXML, ok := container.members[m.member]
		if !ok || sha256.Sum256(rawXML) != m.xmlSHA256 {
			return nil, fmt.Errorf("VFF XML facts do not match the current container")
		}
		reader.members[parts[1]] = m.member
		reader.names[m.name] = m.member
	}
	count := 0
	err := reader.each(raw, 0, 0, func(tag string, payload []byte, offset int) error {
		count++
		if tag != "F401" || count != 1 {
			return fmt.Errorf("VFF bindings require one exact model root")
		}
		return reader.walk(payload, "model", "F401", 1, offset)
	})
	if err != nil {
		return nil, err
	}
	for _, ref := range reader.result.references {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ref.kind == "layer" {
			if _, ok := reader.result.layers[ref.id]; !ok {
				return nil, fmt.Errorf("dangling VFF layer reference")
			}
		} else if material, ok := reader.result.materials[ref.id]; !ok {
			return nil, fmt.Errorf("dangling VFF material reference")
		} else if material.role != "ordinary" {
			return nil, fmt.Errorf("VFF entity references a layer-owned material")
		}
	}
	for _, m := range materials {
		if !reader.claimed[m.member] {
			return nil, fmt.Errorf("unclaimed VFF material XML declaration")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return reader.result, nil
}

// each checks the entire byte range without copying payloads or searching for
// tags inside arbitrary strings/attributes. Recursion is role-directed below.
func (r *vffBindingReader) each(raw []byte, depth, base int, consume func(string, []byte, int) error) error {
	if depth > r.limits.depth {
		return fmt.Errorf("VFF binding depth budget exceeded")
	}
	for len(raw) > 0 {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.records++
		if r.records > r.limits.records {
			return fmt.Errorf("VFF binding record budget exceeded")
		}
		if len(raw) < 6 {
			return fmt.Errorf("truncated VFF binding record")
		}
		size := uint64(binary.LittleEndian.Uint32(raw[2:6]))
		if size > uint64(len(raw)-6) {
			return fmt.Errorf("truncated VFF binding payload")
		}
		tag := fmt.Sprintf("%02X%02X", raw[0], raw[1])
		payload := raw[6 : 6+int(size)]
		if err := consume(tag, payload, base+6); err != nil {
			return err
		}
		base += 6 + int(size)
		raw = raw[6+int(size):]
	}
	return r.ctx.Err()
}
func (r *vffBindingReader) opaque(data []byte, location string, offset int) {
	r.result.opaque = append(r.result.opaque, vffOpaqueRecord{location: location, bytes: len(data), offset: offset, sha256: sha256.Sum256(data)})
}
func (r *vffBindingReader) walk(raw []byte, role, location string, depth, base int) error {
	wrappers := map[string]bool{}
	references := map[string]bool{}
	return r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if tag == "D107" || tag == "AF0D" || tag == "D207" {
			if references[tag] {
				return fmt.Errorf("duplicate VFF binding reference field")
			}
			references[tag] = true
		}
		next := ""
		where := location + "/" + tag
		switch role {
		case "model":
			next = map[string]string{"F701": "material-manager", "F801": "layer-manager", "F901": "definitions", "F601": "geometry-wrapper"}[tag]
		case "material-manager":
			if tag == "D430" {
				next = "material-list-wrapper"
			}
		case "material-list-wrapper":
			if tag == "D530" {
				next = "material-list"
			}
		case "material-list":
			if tag == "C832" {
				return r.registration(data, "ordinary", 0, depth+1, offset)
			}
		case "layer-manager":
			if tag == "983A" {
				next = "layer-list-wrapper"
			}
		case "layer-list-wrapper":
			if tag == "993A" {
				next = "layer-list"
			}
		case "layer-list":
			if tag == "8C3C" {
				return r.layer(data, where, depth+1, offset)
			}
		case "definitions":
			if tag == "7017" {
				next = "definition-list-wrapper"
			}
		case "definition-list-wrapper":
			if tag == "7117" {
				next = "definition-list"
			}
		case "definition-list":
			if tag == "7C15" {
				next = "definition"
			}
		case "definition":
			if tag == "8813" {
				next = "geometry"
			}
		case "geometry-wrapper":
			if tag == "8813" {
				next = "geometry"
			}
		case "geometry":
			next = map[string]string{"8913": "vertices", "8A13": "edges", "8B13": "faces", "8C13": "instances", "8D13": "groups"}[tag]
		case "vertices":
			if tag == "C409" {
				next = "vertex"
			}
		case "edges":
			if tag == "B80B" {
				next = "edge"
			}
		case "faces":
			if tag == "AC0D" {
				next = "face"
			}
		case "instances":
			if tag == "6419" {
				next = "instance"
			}
		case "groups":
			if tag == "4C1D" {
				next = "instances"
			}
		case "vertex", "edge", "face", "instance":
			if tag == "D007" {
				next = "properties-" + role
			}
			if tag == "AF0D" && role == "face" {
				return r.reference(data, "back", offset-6)
			}
		case "properties-vertex", "properties-edge", "properties-face", "properties-instance":
			if tag == "D107" {
				if role == "properties-vertex" {
					return fmt.Errorf("VFF material reference at unproved vertex role")
				}
				return r.reference(data, strings.TrimPrefix(role, "properties-"), offset-6)
			}
			if tag == "D207" {
				return r.reference(data, "layer", offset-6)
			}
		}
		if next != "" {
			switch role {
			case "model", "material-manager", "material-list-wrapper", "layer-manager", "layer-list-wrapper", "definitions", "definition-list-wrapper", "definition", "geometry-wrapper", "vertex", "edge", "face", "instance":
				if wrappers[tag] {
					return fmt.Errorf("duplicate VFF binding structural wrapper")
				}
				wrappers[tag] = true
			}
			return r.walk(data, next, where, depth+1, offset)
		}
		// Material-shaped records at unproved roles must never be silently joined.
		if tag == "C832" || tag == "D107" || tag == "AF0D" || tag == "D207" || tag == "8F3C" {
			return fmt.Errorf("VFF binding-bearing record at unsupported role")
		}
		r.opaque(data, where, offset-6)
		return nil
	})
}
func vffBindingID(raw []byte) (uint64, error) {
	if len(raw) < 1 || len(raw) > 8 {
		return 0, fmt.Errorf("invalid VFF binding ID size")
	}
	var value uint64
	for i, b := range raw {
		value |= uint64(b) << (8 * i)
	}
	if value == 0 {
		return 0, fmt.Errorf("zero VFF binding ID")
	}
	return value, nil
}
func (r *vffBindingReader) entityID(raw []byte, depth, base int) (uint64, error) {
	var id uint64
	count := 0
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		count++
		if tag != "DE05" || count != 1 {
			return fmt.Errorf("ambiguous VFF binding entity ID")
		}
		var err error
		id, err = vffBindingID(data)
		return err
	})
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, fmt.Errorf("missing VFF binding entity ID")
	}
	return id, nil
}
func (r *vffBindingReader) reference(raw []byte, kind string, offset int) error {
	if len(r.result.references) >= r.limits.references {
		return fmt.Errorf("VFF binding reference budget exceeded")
	}
	id, err := vffBindingID(raw)
	if err != nil {
		return err
	}
	r.result.references = append(r.result.references, vffBindingReference{id: id, kind: kind, offset: offset})
	return nil
}
func (r *vffBindingReader) registration(raw []byte, role string, layerID uint64, depth, base int) error {
	seen := map[string]bool{}
	var id uint64
	var token string
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if seen[tag] {
			return fmt.Errorf("duplicate VFF material registration field")
		}
		seen[tag] = true
		switch tag {
		case "DC05":
			var err error
			id, err = r.entityID(data, depth+1, offset)
			return err
		case "CC32":
			if len(data) == 0 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				return fmt.Errorf("invalid VFF material token")
			}
			token = string(data)
		case "CA32":
			expected := byte(0)
			if role == "layer" {
				expected = 1
			}
			if len(data) != 1 || data[0] != expected {
				return fmt.Errorf("unproved VFF material registration flags")
			}
		case "CB32":
			if len(data) != 0 {
				return fmt.Errorf("unproved VFF material registration extension")
			}
		case "CD32":
			if len(data) != 4 || binary.LittleEndian.Uint32(data) != 0 {
				return fmt.Errorf("unproved VFF material registration mode")
			}
		default:
			return fmt.Errorf("unknown VFF material registration field")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !seen["DC05"] || !seen["CC32"] || !seen["CA32"] || !seen["CB32"] {
		return fmt.Errorf("incomplete VFF material registration")
	}
	if _, ok := r.result.materials[id]; ok {
		return fmt.Errorf("duplicate VFF material ID across model roles")
	}
	candidates := map[string]bool{}
	for _, index := range []map[string]string{r.names, r.members} {
		if member := index[token]; member != "" {
			candidates[member] = true
		}
	}
	if len(candidates) != 1 {
		return fmt.Errorf("missing or conflicting VFF material declaration")
	}
	var member string
	for name := range candidates {
		member = name
	}
	r.result.materials[id] = vffBinding{id: id, token: token, member: member, role: role, layerID: layerID, offset: base - 6}
	r.claimed[member] = true
	if role == "layer" {
		r.result.layers[layerID] = id
	}
	return nil
}
func (r *vffBindingReader) layer(raw []byte, location string, depth, base int) error {
	var id uint64
	var name string
	var material []byte
	var materialOffset int
	seen := map[string]bool{}
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		if seen[tag] {
			return fmt.Errorf("duplicate VFF layer field")
		}
		seen[tag] = true
		switch tag {
		case "DC05":
			var err error
			id, err = r.entityID(data, depth+1, offset)
			return err
		case "8D3C":
			if len(data) == 0 || !utf8.Valid(data) {
				return fmt.Errorf("invalid VFF layer name")
			}
			name = string(data)
		case "8F3C":
			material = data
			materialOffset = offset
		default:
			if tag == "C832" || tag == "D107" || tag == "AF0D" || tag == "D207" {
				return fmt.Errorf("VFF binding-bearing record at unsupported layer role")
			}
			r.opaque(data, location+"/"+tag, offset-6)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if id == 0 || name == "" || !seen["8F3C"] {
		return fmt.Errorf("incomplete VFF layer material binding")
	}
	if _, ok := r.result.layers[id]; ok {
		return fmt.Errorf("duplicate VFF layer ID")
	}
	count := 0
	err = r.each(material, depth+1, materialOffset, func(tag string, data []byte, offset int) error {
		count++
		if tag != "C832" || count != 1 {
			return fmt.Errorf("ambiguous VFF layer material registration")
		}
		before := len(r.result.materials)
		if err := r.registration(data, "layer", id, depth+2, offset); err != nil {
			return err
		}
		if len(r.result.materials) != before+1 {
			return fmt.Errorf("missing VFF layer material")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("missing VFF layer material registration")
	}
	return nil
}
