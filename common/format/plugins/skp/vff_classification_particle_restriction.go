package skp

import (
	"encoding/xml"
	"fmt"
)

type vffSKCParticle struct {
	name        xml.Name
	min, max    string
	declaration *vffXMLNode
}

func vffSKCOccurrenceGreater(a, b string) bool {
	if b == "unbounded" {
		return false
	}
	return a == "unbounded" || len(a) > len(b) || len(a) == len(b) && a > b
}

// Reads a single flat sequence. A nil sequence represents empty content.
func (b *vffSKCBinder) particleOccurrences(sequence *vffXMLNode) ([]vffSKCParticle, bool, error) {
	if sequence == nil {
		return nil, true, nil
	}
	min, err := vffSKCOccurs(sequence.attrs, "minOccurs", false)
	if err != nil {
		return nil, false, err
	}
	max, err := vffSKCOccurs(sequence.attrs, "maxOccurs", true)
	if err != nil {
		return nil, false, err
	}
	if min != "1" || max != "1" {
		return nil, false, nil
	}
	schema := sequence
	for schema.parent != nil {
		schema = schema.parent
	}
	var result []vffSKCParticle
	for _, element := range sequence.children {
		if err := b.tick(0); err != nil {
			return nil, false, err
		}
		if element.name.Local == "annotation" {
			continue
		}
		if element.name.Local != "element" || !vffSKCBindingName(element.attrs["name"]) || element.attrs["ref"] != "" {
			return nil, false, nil
		}
		form, declared := element.attrs["form"]
		if !declared {
			form, declared = schema.attrs["elementFormDefault"]
		}
		if !declared {
			form = "unqualified"
		}
		name := xml.Name{Local: element.attrs["name"]}
		switch form {
		case "qualified":
			name.Space = schema.attrs["targetNamespace"]
		case "unqualified":
		default:
			return nil, false, nil
		}
		min, err := vffSKCOccurs(element.attrs, "minOccurs", false)
		if err != nil {
			return nil, false, err
		}
		max, err := vffSKCOccurs(element.attrs, "maxOccurs", true)
		if err != nil {
			return nil, false, err
		}
		if vffSKCOccurrenceGreater(min, max) {
			return nil, false, fmt.Errorf("reversed XSD occurrence bounds for %s", name.Local)
		}
		result = append(result, vffSKCParticle{name, min, max, element})
	}
	return result, true, nil
}

// Flattens the supported base-first extension shape without expanding repeats.
func (b *vffSKCBinder) baseParticleOccurrences(node *vffXMLNode, depth int) ([]vffSKCParticle, bool, error) {
	if err := b.tick(depth); err != nil {
		return nil, false, err
	}
	if mixed := node.attrs["mixed"]; mixed != "" && mixed != "false" && mixed != "0" {
		return nil, false, nil
	}
	var inherited []vffSKCParticle
	var sequence *vffXMLNode
	var content *vffXMLNode
	for _, child := range node.children {
		if err := b.tick(depth); err != nil {
			return nil, false, err
		}
		switch child.name.Local {
		case "annotation", "attribute", "attributeGroup":
		case "sequence":
			if sequence != nil || content != nil {
				return nil, false, nil
			}
			sequence = child
		case "complexContent":
			if sequence != nil || content != nil || len(child.children) != 1 || len(child.attrs) != 0 {
				return nil, false, nil
			}
			content = child
		default:
			return nil, false, nil
		}
	}
	if content != nil {
		extension := content.children[0]
		if extension.name.Local != "extension" {
			return nil, false, nil
		}
		base, err := b.substitutionType(extension, extension.attrs["base"])
		if err != nil || base.node == nil || base.node.name.Local != "complexType" {
			return nil, false, err
		}
		var known bool
		inherited, known, err = b.baseParticleOccurrences(base.node, depth+1)
		if err != nil || !known {
			return nil, false, err
		}
		for _, child := range extension.children {
			if err := b.tick(depth); err != nil {
				return nil, false, err
			}
			switch child.name.Local {
			case "annotation", "attribute", "attributeGroup":
			case "sequence":
				if sequence != nil {
					return nil, false, nil
				}
				sequence = child
			default:
				return nil, false, nil
			}
		}
	}
	local, known, err := b.particleOccurrences(sequence)
	if err != nil || !known {
		return nil, false, err
	}
	return append(inherited, local...), true, nil
}

// A type is comparable only when it is explicitly named and resolved in the
// element's declaration scope. An inline type requires its own proof.
func (b *vffSKCBinder) particleType(element *vffXMLNode) (vffSKCSubstitutionType, error) {
	for _, child := range element.children {
		if err := b.tick(0); err != nil {
			return vffSKCSubstitutionType{}, err
		}
		if child.name.Local != "annotation" {
			return vffSKCSubstitutionType{}, nil
		}
	}
	return b.substitutionType(element, element.attrs["type"])
}

func (b *vffSKCBinder) particleRestrictionIssue(restriction, base *vffXMLNode, depth int) (string, error) {
	const unproved = "XSD particle occurrence restriction unproved"
	var sequence *vffXMLNode
	for _, child := range restriction.children {
		if child.name.Local == "sequence" {
			sequence = child
		}
	}
	derived, known, err := b.particleOccurrences(sequence)
	if err != nil || !known {
		return unproved, err
	}
	original, known, err := b.baseParticleOccurrences(base, depth+1)
	if err != nil || !known {
		return unproved, err
	}
	positions := make(map[xml.Name]int, len(original))
	for i, particle := range original {
		if err := b.tick(depth); err != nil {
			return "", err
		}
		if _, duplicate := positions[particle.name]; duplicate {
			return unproved, nil
		}
		positions[particle.name] = i
	}
	next := 0
	for _, particle := range derived {
		if err := b.tick(depth); err != nil {
			return "", err
		}
		i, found := positions[particle.name]
		if !found || i < next {
			return unproved, nil
		}
		for _, skipped := range original[next:i] {
			if skipped.min != "0" {
				return fmt.Sprintf("XSD particle restriction omits required %s", skipped.name.Local), nil
			}
		}
		from := original[i]
		if vffSKCOccurrenceGreater(particle.max, from.max) {
			return fmt.Sprintf("XSD particle restriction widens %s maxOccurs from %s to %s", particle.name.Local, from.max, particle.max), nil
		}
		if vffSKCOccurrenceGreater(from.min, particle.min) {
			return fmt.Sprintf("XSD particle restriction lowers %s minOccurs from %s to %s", particle.name.Local, from.min, particle.min), nil
		}
		derivedType, err := b.particleType(particle.declaration)
		if err != nil {
			return "", err
		}
		baseType, err := b.particleType(from.declaration)
		if err != nil {
			return "", err
		}
		if derivedType.name.Local == "" || derivedType != baseType {
			return fmt.Sprintf("XSD particle restriction type compatibility unproved for %s", particle.name.Local), nil
		}
		next = i + 1
	}
	for _, skipped := range original[next:] {
		if skipped.min != "0" {
			return fmt.Sprintf("XSD particle restriction omits required %s", skipped.name.Local), nil
		}
	}
	return "", nil
}
