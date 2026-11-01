package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type vffSKCValueReference struct {
	role   string
	name   xml.Name
	source vffSKCDeclarationWitness
	target *vffSKCDeclarationWitness
	via    []vffSKCSchemaLink
}
type vffSKCValueBinding struct {
	references           []vffSKCValueReference
	declarations, facets []vffSKCDeclarationWitness
	terminals            []xml.Name
	issues               []string
	structures           []vffSKCValueStructure
	attributeSets        []*vffSKCValueAttributes
	attributeUse         *vffSKCValueAttribute
	fixedValues          []vffSKCFixedValueComparison
}

func (b *vffSKCBinder) readValueType(n *vffXMLNode) (*vffSKCValueBinding, error) {
	if prior, ok := b.values[n]; ok {
		return prior, nil
	}
	result := &vffSKCValueBinding{}
	active := map[*vffXMLNode]bool{}
	var attributes *vffSKCValueAttributes
	attributeSets := map[*vffXMLNode]*vffSKCValueAttributes{}
	var visit func(*vffXMLNode, int) error
	var reference func(*vffXMLNode, string, string, int) error
	memberReference := func(source *vffXMLNode, raw string, depth int) error {
		if err := b.tick(depth); err != nil {
			return err
		}
		name, err := b.qname(source, raw)
		if err != nil {
			return err
		}
		kind := source.name.Local
		index := len(result.references)
		result.references = append(result.references, vffSKCValueReference{role: kind + "Ref", name: name, source: b.witness(source)})
		target, err := b.resolveValue(source, name, kind)
		if err != nil {
			return err
		}
		if target == nil {
			result.issues = append(result.issues, "complex value "+kind+" reference not uniquely bound: "+raw)
			return nil
		}
		w := b.witness(target.node)
		result.references[index].target, result.references[index].via = &w, target.via
		if kind == "attribute" {
			if err := b.valueAttribute(attributes, source, target.node, result); err != nil {
				return err
			}
		}
		return visit(target.node, depth+1)
	}
	reference = func(source *vffXMLNode, raw, role string, depth int) error {
		if err := b.tick(depth); err != nil {
			return err
		}
		name, err := b.qname(source, raw)
		if err != nil {
			return err
		}
		index := len(result.references)
		result.references = append(result.references, vffSKCValueReference{role: role, name: name, source: b.witness(source)})
		if name.Space == vffXSDNS {
			if !vffSKCBuiltinType(name.Local) {
				result.issues = append(result.issues, "unknown XSD 1.0 builtin type: "+name.Local)
				return nil
			}
			if role == "simpleContentBase" && name.Local == "anyType" {
				result.issues = append(result.issues, "complex builtin is not a simple-content base")
				return nil
			}
			result.terminals = append(result.terminals, name)
			if name.Local == "anyType" || name.Local == "anySimpleType" {
				result.issues = append(result.issues, "open builtin value type")
			}
			return nil
		}
		target, err := b.resolveValue(source, name, "type")
		if err != nil {
			return err
		}
		if target == nil {
			result.issues = append(result.issues, "value type reference not uniquely bound: "+raw)
			return nil
		}
		declaration := target.node
		w := b.witness(declaration)
		result.references[index].target, result.references[index].via = &w, target.via
		if declaration.name.Local == "complexType" {
			if role == "simpleContentBase" {
				valid, err := b.simpleContentType(declaration)
				if err != nil {
					return err
				}
				if !valid {
					result.issues = append(result.issues, "base is not a simple-content complex type")
					return nil
				}
			} else if source.name.Local != "element" || role != "type" {
				result.issues = append(result.issues, "complex value type outside element type position")
				return nil
			}
		}
		if err := visit(declaration, depth+1); err != nil {
			return err
		}
		if role == "simpleContentBase" && declaration.name.Local == "complexType" {
			return b.mergeValueAttributes(attributes, attributeSets[declaration], result)
		}
		return nil
	}
	visit = func(node *vffXMLNode, depth int) error {
		if err := b.tick(depth); err != nil {
			return err
		}
		if active[node] {
			return fmt.Errorf("cyclic XSD value type references")
		}
		active[node] = true
		defer delete(active, node)
		var children []*vffXMLNode
		for _, child := range node.children {
			if err := b.tick(depth); err != nil {
				return err
			}
			if child.name.Local != "annotation" {
				children = append(children, child)
			}
		}
		parentAttributes := attributes
		if node.name.Local == "complexType" || node.name.Local == "attributeGroup" && node.attrs["name"] != "" {
			attributes = &vffSKCValueAttributes{source: b.witness(node), names: map[xml.Name][]int{}}
			attributeSets[node] = attributes
			result.attributeSets = append(result.attributeSets, attributes)
			defer func() { attributes = parentAttributes }()
		}
		switch kind := node.name.Local; kind {
		case "element", "attribute":
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			if name, named := node.attrs["name"]; named && !vffSKCBindingName(name) {
				return fmt.Errorf("invalid complex value member name")
			}
			if _, named := node.attrs["name"]; !named {
				if _, referenced := node.attrs["ref"]; !referenced {
					return fmt.Errorf("complex value member requires name or ref")
				}
			}
			var inline []*vffXMLNode
			for _, child := range children {
				if child.name.Local == "simpleType" || child.name.Local == "complexType" {
					inline = append(inline, child)
				} else {
					result.issues = append(result.issues, "uninterpreted value child: "+child.name.Local)
				}
			}
			typeName, typed := node.attrs["type"]
			if ref, exists := node.attrs["ref"]; exists {
				_, formed := node.attrs["form"]
				if _, named := node.attrs["name"]; named || typed || len(inline) > 0 || kind == "attribute" && formed {
					return fmt.Errorf("conflicting complex value member reference")
				}
				return memberReference(node, ref, depth+1)
			}
			if kind == "attribute" {
				if err := b.valueAttribute(attributes, node, node, result); err != nil {
					return err
				}
			}
			if typed && len(inline) > 0 || len(inline) > 1 {
				return fmt.Errorf("conflicting field value type declarations")
			}
			if typed {
				return reference(node, typeName, "type", depth+1)
			}
			if len(inline) == 1 {
				return visit(inline[0], depth+1)
			}
			result.issues = append(result.issues, "implicit value type not proved")
		case "complexType":
			result.declarations = append(result.declarations, b.witness(node))
			if node.parent != nil && node.parent.name.Local == "attribute" {
				result.issues = append(result.issues, "complex value type outside element type position")
				return nil
			}
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			if len(children) == 0 {
				result.issues = append(result.issues, "empty complex value semantics not proved")
			}
			for _, child := range children {
				if child.name.Local == "simpleContent" && len(children) != 1 {
					return fmt.Errorf("conflicting complex content sources")
				}
			}
			for _, child := range children {
				switch child.name.Local {
				case "sequence", "choice", "all", "attribute", "simpleContent":
					if err := visit(child, depth+1); err != nil {
						return err
					}
				default:
					result.issues = append(result.issues, "unsupported complex value child: "+child.name.Local)
				}
			}
		case "simpleContent":
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			if node.parent == nil || node.parent.name.Local != "complexType" || len(children) != 1 {
				return fmt.Errorf("simple content requires one derivation in a complex type")
			}
			if children[0].name.Local != "extension" {
				result.issues = append(result.issues, "simple content derivation not proved")
				return nil
			}
			return visit(children[0], depth+1)
		case "extension":
			if node.parent == nil || node.parent.name.Local != "simpleContent" {
				result.issues = append(result.issues, "extension outside simple content not proved")
				return nil
			}
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			if err := reference(node, node.attrs["base"], "simpleContentBase", depth+1); err != nil {
				return err
			}
			for _, child := range children {
				if child.name.Local != "attribute" && child.name.Local != "attributeGroup" {
					result.issues = append(result.issues, "unsupported simple content extension child: "+child.name.Local)
					continue
				}
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case "attributeGroup":
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			name, named := node.attrs["name"]
			ref, referenced := node.attrs["ref"]
			if named == referenced || named && !vffSKCBindingName(name) || referenced && len(children) != 0 {
				return fmt.Errorf("conflicting or missing value attribute group source")
			}
			if referenced {
				return memberReference(node, ref, depth+1)
			}
			if node.parent == nil || node.parent.name.Local != "schema" {
				return fmt.Errorf("named value attribute group must be global")
			}
			result.declarations = append(result.declarations, b.witness(node))
			for _, child := range children {
				if child.name.Local != "attribute" && child.name.Local != "attributeGroup" {
					result.issues = append(result.issues, "unsupported value attribute group child: "+child.name.Local)
					continue
				}
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
			return b.mergeValueAttributes(parentAttributes, attributes, result)
		case "sequence", "choice", "all":
			if err := b.valueStructure(node, result); err != nil {
				return err
			}
			for _, child := range children {
				switch child.name.Local {
				case "sequence", "choice", "element":
					if err := visit(child, depth+1); err != nil {
						return err
					}
				default:
					result.issues = append(result.issues, "unsupported complex value particle: "+child.name.Local)
				}
			}
		case "simpleType":
			result.declarations = append(result.declarations, b.witness(node))
			if len(children) != 1 {
				return fmt.Errorf("simple type requires one derivation")
			}
			return visit(children[0], depth+1)
		case "restriction", "list":
			role := "base"
			if kind == "list" {
				role = "itemType"
			}
			value, exists := node.attrs[role]
			var inline []*vffXMLNode
			for _, child := range children {
				if child.name.Local == "simpleType" {
					inline = append(inline, child)
				} else if kind == "restriction" {
					result.facets = append(result.facets, b.witness(child))
					if !strings.Contains(" enumeration pattern whiteSpace length minLength maxLength totalDigits fractionDigits minInclusive maxInclusive minExclusive maxExclusive ", " "+child.name.Local+" ") {
						result.issues = append(result.issues, "unknown restriction facet")
					}
				} else {
					result.issues = append(result.issues, "unsupported list child")
				}
			}
			if exists && len(inline) > 0 || len(inline) > 1 {
				return fmt.Errorf("conflicting simple type derivation sources")
			}
			if exists {
				return reference(node, value, role, depth+1)
			}
			if len(inline) == 1 {
				return visit(inline[0], depth+1)
			}
			return fmt.Errorf("missing simple type derivation source")
		case "union":
			members, exists := node.attrs["memberTypes"]
			parts := strings.Fields(members)
			if exists && len(parts) == 0 {
				return fmt.Errorf("empty union member types")
			}
			count := len(parts)
			for _, part := range parts {
				if err := reference(node, part, "memberTypes", depth+1); err != nil {
					return err
				}
			}
			for _, child := range children {
				if child.name.Local != "simpleType" {
					result.issues = append(result.issues, "unsupported union child")
					continue
				}
				count++
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
			if count == 0 {
				return fmt.Errorf("union without member type")
			}
		default:
			result.issues = append(result.issues, "unsupported value type structure: "+kind)
		}
		return nil
	}
	if err := visit(n, 1); err != nil {
		return nil, err
	}
	if err := b.checkAttributeValues(result); err != nil {
		return nil, err
	}
	b.values[n] = result
	return result, nil
}

func (b *vffSKCBinder) simpleContentType(node *vffXMLNode) (bool, error) {
	var content *vffXMLNode
	for _, child := range node.children {
		if err := b.tick(0); err != nil {
			return false, err
		}
		if child.name.Local == "annotation" {
			continue
		}
		if content != nil || child.name.Local != "simpleContent" {
			return false, nil
		}
		content = child
	}
	return content != nil, nil
}

// XSD 1.0 reference names, not a coercion or instance validator.
func vffSKCBuiltinType(name string) bool {
	for _, candidate := range strings.Fields("anyType anySimpleType string boolean decimal float double duration dateTime time date gYearMonth gYear gMonthDay gDay gMonth hexBinary base64Binary anyURI QName NOTATION normalizedString token language NMTOKEN NMTOKENS Name NCName ID IDREF IDREFS ENTITY ENTITIES integer nonPositiveInteger negativeInteger long int short byte nonNegativeInteger unsignedLong unsignedInt unsignedShort unsignedByte positiveInteger") {
		if name == candidate {
			return true
		}
	}
	return false
}
