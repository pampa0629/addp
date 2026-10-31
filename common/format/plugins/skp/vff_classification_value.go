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
}
type vffSKCValueBinding struct {
	references           []vffSKCValueReference
	declarations, facets []vffSKCDeclarationWitness
	terminals            []xml.Name
	issues               []string
}

func (b *vffSKCBinder) readValueType(n *vffXMLNode) (*vffSKCValueBinding, error) {
	if prior, ok := b.values[n]; ok {
		return prior, nil
	}
	result := &vffSKCValueBinding{}
	active := map[*vffXMLNode]bool{}
	var visit func(*vffXMLNode, int) error
	var reference func(*vffXMLNode, string, string, int) error
	reference = func(source *vffXMLNode, raw, role string, depth int) error {
		if err := b.tick(depth); err != nil {
			return err
		}
		name, err := b.qname(source, raw)
		if err != nil {
			return err
		}
		result.references = append(result.references, vffSKCValueReference{role, name, b.witness(source)})
		if name.Space == vffXSDNS {
			if !vffSKCBuiltinType(name.Local) {
				result.issues = append(result.issues, "unknown XSD 1.0 builtin type: "+name.Local)
				return nil
			}
			result.terminals = append(result.terminals, name)
			if name.Local == "anyType" || name.Local == "anySimpleType" {
				result.issues = append(result.issues, "open builtin value type")
			}
			return nil
		}
		if name.Space != b.namespace {
			result.issues = append(result.issues, "external value type: "+raw)
			return nil
		}
		if declaration := b.definitions["simpleType"][name.Local]; declaration != nil {
			return visit(declaration, depth+1)
		}
		if declaration := b.definitions["complexType"][name.Local]; declaration != nil {
			result.declarations = append(result.declarations, b.witness(declaration))
			result.issues = append(result.issues, "complex value type not expanded")
			return nil
		}
		result.issues = append(result.issues, "value type declaration missing: "+raw)
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
		switch kind := node.name.Local; kind {
		case "element", "attribute":
			var inline []*vffXMLNode
			for _, child := range children {
				if child.name.Local == "simpleType" || child.name.Local == "complexType" {
					inline = append(inline, child)
				} else {
					result.issues = append(result.issues, "uninterpreted value child: "+child.name.Local)
				}
			}
			typeName, typed := node.attrs["type"]
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
			result.issues = append(result.issues, "anonymous complex value not expanded")
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
	b.values[n] = result
	return result, nil
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
