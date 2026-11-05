package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Only projects type edges already resolved by readValueType.
func (c *vffSKCRestrictionChecker) referenceProfile(ref vffSKCValueReference, depth int) (*vffSKCRestrictionProfile, error) {
	if ref.name.Space == vffXSDNS {
		if ref.name.Local == "NMTOKENS" {
			item := &vffSKCRestrictionProfile{primitive: xml.Name{Space: vffXSDNS, Local: "NMTOKEN"}, known: true}
			return &vffSKCRestrictionProfile{item: item, bounds: map[string]string{"minLength": "1"}, known: true}, nil
		}
		return &vffSKCRestrictionProfile{primitive: ref.name, known: vffSKCSupportedScalar(ref.name)}, nil
	}
	if ref.target != nil && ref.target.kind == "simpleType" {
		return c.profile(c.b.nodes[vffSKCNodeKey{ref.target.member, ref.target.begin}], depth+1)
	}
	return nil, nil
}

func (c *vffSKCRestrictionChecker) listProfile(node *vffXMLNode, depth int) (*vffSKCRestrictionProfile, error) {
	unknown := &vffSKCRestrictionProfile{}
	var inline *vffXMLNode
	annotations := 0
	for _, child := range node.children {
		if err := c.b.tick(0); err != nil {
			return nil, err
		}
		switch child.name.Local {
		case "annotation":
			annotations++
			if annotations > 1 {
				return unknown, nil
			}
		case "simpleType":
			if inline != nil {
				return nil, fmt.Errorf("multiple list item type declarations")
			}
			inline = child
		default:
			return unknown, nil
		}
	}
	var item *vffSKCRestrictionProfile
	var err error
	ref, exists := c.refs[vffSKCTypeReferenceKey{c.b.witness(node), "itemType"}]
	if exists {
		if inline != nil {
			return nil, fmt.Errorf("conflicting list item type sources")
		}
		item, err = c.referenceProfile(ref, depth)
	} else if inline != nil {
		item, err = c.profile(inline, depth+1)
	}
	if err != nil {
		return nil, err
	}
	if item == nil || !item.known {
		c.cache[node] = unknown
		return unknown, nil
	}
	if item.item != nil {
		return nil, fmt.Errorf("list item type cannot be a list")
	}
	// Both supported item varieties preserve token boundaries in their
	// canonical text. Text items must have a proved enumeration layer.
	_, _, _, integer := vffSKCIntegerBounds(item.primitive.Local)
	enumText := item.primitive.Local == "normalizedString" && len(item.levels) > 0
	if item.primitive.Space != vffXSDNS || (!integer && !enumText) {
		c.cache[node] = unknown
		return unknown, nil
	}
	p := &vffSKCRestrictionProfile{item: item, known: true}
	c.cache[node] = p
	return p, nil
}

func (p *vffSKCRestrictionProfile) itemPrimitive() xml.Name {
	if p.item != nil {
		return p.item.primitive
	}
	return xml.Name{}
}

func (b *vffSKCBinder) profileFacets(p *vffSKCRestrictionProfile) ([]vffSKCDeclarationWitness, []vffSKCDeclarationWitness, error) {
	facets, err := b.copyFacetWitnesses(p.facets)
	if err != nil || p.item == nil {
		return facets, nil, err
	}
	items, err := b.copyFacetWitnesses(p.item.facets)
	return facets, items, err
}

func (c *vffSKCRestrictionChecker) listValue(item *vffSKCRestrictionProfile, node *vffXMLNode, raw string) (vffSKCValue, bool, error) {
	value := vffSKCValue{list: true}
	if err := c.b.tick(0); err != nil {
		return value, false, err
	}
	for range len(raw) {
		if err := c.b.tick(0); err != nil {
			return value, false, err
		}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' })
	var text strings.Builder
	for _, part := range parts {
		if err := c.b.tick(0); err != nil {
			return value, false, err
		}
		v, known, err := c.evaluate(item, node, part)
		if err != nil || !known {
			return value, known, err
		}
		for range len(v.text) + 1 {
			if err := c.b.tick(0); err != nil {
				return value, false, err
			}
		}
		if value.items > 0 {
			text.WriteByte(' ')
		}
		text.WriteString(v.text)
		value.items++
	}
	value.text = text.String()
	return value, true, nil
}
