package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type vffSKCEnumerationValue struct {
	value  vffSKCScalar
	source vffSKCDeclarationWitness
}
type vffSKCRestrictionProfile struct {
	primitive xml.Name
	levels    [][]vffSKCEnumerationValue
	facets    []vffSKCDeclarationWitness
	known     bool
}
type vffSKCRestrictionChecker struct {
	b      *vffSKCBinder
	refs   map[vffSKCTypeReferenceKey]vffSKCValueReference
	cache  map[*vffXMLNode]*vffSKCRestrictionProfile
	active map[*vffXMLNode]bool
}

// Projection of already-resolved type edges. This never resolves a name or
// parses another document and never discards a facet to recover a builtin.
func (c *vffSKCRestrictionChecker) profile(node *vffXMLNode, depth int) (*vffSKCRestrictionProfile, error) {
	b := c.b
	if err := b.tick(depth); err != nil {
		return nil, err
	}
	if p, ok := c.cache[node]; ok {
		return p, nil
	}
	if c.active[node] {
		return nil, fmt.Errorf("cyclic restriction profile")
	}
	c.active[node] = true
	defer delete(c.active, node)
	unknown := &vffSKCRestrictionProfile{}
	if strings.Trim(node.text, " \t\r\n") != "" {
		return unknown, nil
	}
	allowed := map[string]bool{"id": true}
	switch node.name.Local {
	case "attribute":
		for _, key := range []string{"name", "ref", "type", "use", "form", "default", "fixed"} {
			allowed[key] = true
		}
	case "simpleType":
		allowed["name"] = true
	case "restriction":
		allowed["base"] = true
	default:
		return unknown, nil
	}
	for key := range node.attrs {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		if !allowed[key] {
			return unknown, nil
		}
	}
	if node.name.Local == "attribute" {
		for _, child := range node.children {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if child.name.Local != "annotation" && child.name.Local != "simpleType" {
				return unknown, nil
			}
		}
	}
	// Schema defaults can constrain derivation legality. They are not ignored.
	for parent := node; parent != nil; parent = parent.parent {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		if parent.name.Local == "schema" && parent.attrs["finalDefault"] != "" {
			return unknown, nil
		}
	}
	role := "type"
	if node.name.Local == "restriction" {
		role = "base"
	}
	var base *vffSKCRestrictionProfile
	if ref, ok := c.refs[vffSKCTypeReferenceKey{b.witness(node), role}]; ok {
		if ref.name.Space == vffXSDNS {
			base = &vffSKCRestrictionProfile{primitive: ref.name, known: vffSKCSupportedScalar(ref.name)}
		} else if ref.target != nil && ref.target.kind == "simpleType" {
			var err error
			base, err = c.profile(b.nodes[vffSKCNodeKey{ref.target.member, ref.target.begin}], depth+1)
			if err != nil {
				return nil, err
			}
		}
	} else {
		var child *vffXMLNode
		for _, n := range node.children {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if n.name.Local == "simpleType" || node.name.Local == "simpleType" && n.name.Local == "restriction" {
				if child != nil {
					return nil, fmt.Errorf("conflicting restriction sources")
				}
				child = n
			}
		}
		if child != nil {
			var err error
			base, err = c.profile(child, depth+1)
			if err != nil {
				return nil, err
			}
		}
	}
	if base == nil || !base.known {
		c.cache[node] = unknown
		return unknown, nil
	}
	if node.name.Local != "restriction" {
		c.cache[node] = base
		return base, nil
	}
	// Unknown children/attributes block the whole profile, not just one facet.
	var enumerations []*vffXMLNode
	for _, n := range node.children {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		if n.name.Local == "annotation" || n.name.Local == "simpleType" {
			continue
		}
		if n.name.Local != "enumeration" {
			c.cache[node] = unknown
			return unknown, nil
		}
		if strings.Trim(n.text, " \t\r\n") != "" {
			c.cache[node] = unknown
			return unknown, nil
		}
		annotations := 0
		for key := range n.attrs {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if key != "value" && key != "id" {
				c.cache[node] = unknown
				return unknown, nil
			}
		}
		for _, child := range n.children {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if child.name.Local != "annotation" {
				c.cache[node] = unknown
				return unknown, nil
			}
			annotations++
			if annotations > 1 {
				c.cache[node] = unknown
				return unknown, nil
			}
		}
		enumerations = append(enumerations, n)
	}
	if len(enumerations) > 0 && base.primitive.Local == "boolean" {
		return nil, fmt.Errorf("enumeration is not applicable to boolean in XSD 1.0")
	}
	for range len(base.levels) {
		if err := b.tick(0); err != nil {
			return nil, err
		}
	}
	facets, err := b.copyFacetWitnesses(base.facets)
	if err != nil {
		return nil, err
	}
	p := &vffSKCRestrictionProfile{primitive: base.primitive, known: true, levels: append([][]vffSKCEnumerationValue(nil), base.levels...), facets: facets}
	var layer []vffSKCEnumerationValue
	for _, n := range enumerations {
		raw, present := n.attrs["value"]
		if !present {
			return nil, fmt.Errorf("enumeration requires a value")
		}
		value, known, err := c.evaluate(base, n, raw)
		if err != nil {
			return nil, err
		}
		if !known {
			c.cache[node] = unknown
			return unknown, nil
		}
		w := b.witness(n)
		layer = append(layer, vffSKCEnumerationValue{value, w})
		p.facets = append(p.facets, w)
	}
	if len(layer) > 0 {
		p.levels = append(p.levels, layer)
	}
	c.cache[node] = p
	return p, nil
}

func (c *vffSKCRestrictionChecker) evaluate(p *vffSKCRestrictionProfile, n *vffXMLNode, raw string) (vffSKCScalar, bool, error) {
	if !p.known {
		return vffSKCScalar{}, false, nil
	}
	value, known, err := c.b.scalar(n, p.primitive, raw)
	if err != nil || !known {
		return value, known, err
	}
	for _, layer := range p.levels {
		found := false
		for _, candidate := range layer {
			if err := c.b.tick(0); err != nil {
				return value, false, err
			}
			equal, err := c.b.equalScalar(value, candidate.value)
			if err != nil {
				return value, false, err
			}
			if equal {
				found = true
				break
			}
		}
		if !found {
			return value, false, fmt.Errorf("attribute constraint violates enumeration facet")
		}
	}
	return value, true, nil
}

func (b *vffSKCBinder) equalScalar(a, c vffSKCScalar) (bool, error) {
	for _, s := range []string{a.text, c.text, a.qname.Space, c.qname.Space, a.qname.Local, c.qname.Local} {
		for range len(s) {
			if err := b.tick(0); err != nil {
				return false, err
			}
		}
	}
	return a == c, nil
}

func vffSKCSupportedScalar(name xml.Name) bool {
	if name.Space != vffXSDNS {
		return false
	}
	switch name.Local {
	case "string", "normalizedString", "token", "boolean", "QName", "decimal":
		return true
	}
	_, _, _, supported := vffSKCIntegerBounds(name.Local)
	return supported
}

type vffSKCConstraintValue struct {
	source, declaration, use vffSKCDeclarationWitness
	kind                     string
	primitive                xml.Name
	value                    vffSKCScalar
	facets                   []vffSKCDeclarationWitness
	known                    bool
}

type vffSKCScalarTypeFact struct {
	declaration vffSKCDeclarationWitness
	primitive   xml.Name
	facets      []vffSKCDeclarationWitness
	known       bool
}

func (b *vffSKCBinder) copyFacetWitnesses(src []vffSKCDeclarationWitness) ([]vffSKCDeclarationWitness, error) {
	for range len(src) {
		if err := b.tick(0); err != nil {
			return nil, err
		}
	}
	return append([]vffSKCDeclarationWitness(nil), src...), nil
}

func (b *vffSKCBinder) checkRestrictionValues(result *vffSKCValueBinding, refs map[vffSKCTypeReferenceKey]vffSKCValueReference) error {
	c := &vffSKCRestrictionChecker{b: b, refs: refs, cache: map[*vffXMLNode]*vffSKCRestrictionProfile{}, active: map[*vffXMLNode]bool{}}
	for i := range result.fixedValues {
		comparison := &result.fixedValues[i]
		if ref, exists := refs[vffSKCTypeReferenceKey{comparison.declaration, "type"}]; exists {
			comparison.typeName = ref.name
		}
		decl := b.nodes[vffSKCNodeKey{comparison.declaration.member, comparison.declaration.begin}]
		use := b.nodes[vffSKCNodeKey{comparison.source.member, comparison.source.begin}]
		p, err := c.profile(decl, 1)
		if err != nil {
			return err
		}
		left, knownLeft, err := c.evaluate(p, use, use.attrs["fixed"])
		if err != nil {
			return err
		}
		right, knownRight, err := c.evaluate(p, decl, decl.attrs["fixed"])
		if err != nil {
			return err
		}
		if knownLeft && knownRight {
			equal, err := b.equalScalar(left, right)
			if err != nil {
				return err
			}
			if !equal {
				return fmt.Errorf("fixed attribute use disagrees with declaration value")
			}
			comparison.useValue, comparison.baseValue, comparison.known = left, right, true
			comparison.primitive = p.primitive
			comparison.facets, err = b.copyFacetWitnesses(p.facets)
			if err != nil {
				return err
			}
		} else {
			result.issues = append(result.issues, "fixed attribute reference typed value equality not proved")
		}
	}
	check := func(use, decl, source vffSKCDeclarationWitness, kind, raw string) error {
		if err := b.tick(0); err != nil {
			return err
		}
		n := b.nodes[vffSKCNodeKey{decl.member, decl.begin}]
		origin := b.nodes[vffSKCNodeKey{source.member, source.begin}]
		p, err := c.profile(n, 1)
		if err != nil {
			return err
		}
		value, known, err := c.evaluate(p, origin, raw)
		if err != nil {
			return err
		}
		facets, err := b.copyFacetWitnesses(p.facets)
		if err != nil {
			return err
		}
		result.constraintValues = append(result.constraintValues, vffSKCConstraintValue{source: source, declaration: decl, use: use, kind: kind, primitive: p.primitive, value: value, facets: facets, known: known})
		return nil
	}
	for _, structure := range result.structures {
		if err := b.tick(0); err != nil {
			return err
		}
		if structure.source.kind != "attribute" || structure.source.name == "" {
			continue
		}
		n := b.nodes[vffSKCNodeKey{structure.source.member, structure.source.begin}]
		mode, err := vffSKCAttributeUse(n.attrs)
		if err != nil {
			return err
		}
		if mode == "prohibited" {
			continue
		}
		p, err := c.profile(n, 1)
		if err != nil {
			return err
		}
		facets, err := b.copyFacetWitnesses(p.facets)
		if err != nil {
			return err
		}
		result.scalarTypes = append(result.scalarTypes, vffSKCScalarTypeFact{declaration: structure.source, primitive: p.primitive, facets: facets, known: p.known})
		for _, kind := range []string{"default", "fixed"} {
			if raw, present := n.attrs[kind]; present {
				if err := check(structure.source, structure.source, structure.source, kind, raw); err != nil {
					return err
				}
			}
		}
	}
	checkUse := func(use vffSKCValueAttribute) error {
		if use.constraint.kind == "" {
			return nil
		}
		return check(use.source, use.declaration, use.constraint.source, use.constraint.kind, use.constraint.lexical)
	}
	for _, set := range result.attributeSets {
		for _, use := range set.uses {
			if err := checkUse(use); err != nil {
				return err
			}
		}
	}
	if result.attributeUse != nil {
		return checkUse(*result.attributeUse)
	}
	return nil
}
