package skp

import "fmt"

type vffSKCTypeReferenceKey struct {
	source vffSKCDeclarationWitness
	role   string
}

// Semantics over the graph already resolved by readValueType. No parsing,
// symbol resolution, external loading, or alternative type-reading route.
func (b *vffSKCBinder) checkAttributeValues(result *vffSKCValueBinding) error {
	refs := map[vffSKCTypeReferenceKey]vffSKCValueReference{}
	for _, ref := range result.references {
		if err := b.tick(0); err != nil {
			return err
		}
		if ref.role == "type" || ref.role == "base" {
			refs[vffSKCTypeReferenceKey{ref.source, ref.role}] = ref
		}
	}
	for i := range result.fixedValues {
		comparison := &result.fixedValues[i]
		if err := b.tick(0); err != nil {
			return err
		}
		ref, exists := refs[vffSKCTypeReferenceKey{comparison.declaration, "type"}]
		if exists {
			comparison.typeName = ref.name
		}
		if exists && ref.name.Space == vffXSDNS {
			use := b.nodes[vffSKCNodeKey{comparison.source.member, comparison.source.begin}]
			decl := b.nodes[vffSKCNodeKey{comparison.declaration.member, comparison.declaration.begin}]
			left, knownLeft, err := b.scalar(use, ref.name, use.attrs["fixed"])
			if err != nil {
				return err
			}
			right, knownRight, err := b.scalar(decl, ref.name, decl.attrs["fixed"])
			if err != nil {
				return err
			}
			if knownLeft && knownRight {
				if left != right {
					return fmt.Errorf("fixed attribute use disagrees with declaration value")
				}
				comparison.useValue, comparison.baseValue, comparison.known = left, right, true
			}
		}
		if !comparison.known {
			result.issues = append(result.issues, "fixed attribute reference typed value equality not proved")
		}
	}
	ids := map[*vffXMLNode]string{}
	active := map[*vffXMLNode]bool{}
	var idType func(*vffXMLNode, int) (string, error)
	idType = func(node *vffXMLNode, depth int) (string, error) {
		if err := b.tick(depth); err != nil {
			return "", err
		}
		if prior, exists := ids[node]; exists {
			return prior, nil
		}
		if active[node] {
			return "", fmt.Errorf("cyclic attribute type ancestry")
		}
		active[node] = true
		defer delete(active, node)
		role := "type"
		if node.name.Local == "restriction" {
			role = "base"
		}
		state := "unproved"
		ref, exists := refs[vffSKCTypeReferenceKey{b.witness(node), role}]
		if exists {
			if ref.name.Space == vffXSDNS && vffSKCBuiltinType(ref.name.Local) {
				state = "not_id"
				if ref.name.Local == "ID" {
					state = "id"
				}
			} else if ref.target != nil && ref.target.kind == "simpleType" {
				target := b.nodes[vffSKCNodeKey{ref.target.member, ref.target.begin}]
				var err error
				state, err = idType(target, depth+1)
				if err != nil {
					return "", err
				}
			}
		} else {
			// Only restriction ancestry, never list items or union members.
			var child *vffXMLNode
			for _, candidate := range node.children {
				if err := b.tick(0); err != nil {
					return "", err
				}
				if candidate.name.Local == "simpleType" || node.name.Local == "simpleType" && candidate.name.Local == "restriction" {
					if child != nil {
						return "", fmt.Errorf("conflicting attribute type ancestry")
					}
					child = candidate
				}
			}
			if child != nil {
				var err error
				state, err = idType(child, depth+1)
				if err != nil {
					return "", err
				}
			}
		}
		ids[node] = state
		return state, nil
	}
	for _, structure := range result.structures {
		if err := b.tick(0); err != nil {
			return err
		}
		if structure.source.kind != "attribute" || structure.source.name == "" {
			continue
		}
		node := b.nodes[vffSKCNodeKey{structure.source.member, structure.source.begin}]
		mode, err := vffSKCAttributeUse(node.attrs)
		if err != nil {
			return err
		}
		if mode == "prohibited" {
			continue
		}
		state, err := idType(node, 1)
		if err != nil {
			return err
		}
		_, def := node.attrs["default"]
		_, fixed := node.attrs["fixed"]
		if def || fixed {
			if state == "id" {
				return fmt.Errorf("ID-derived attribute declaration cannot have default or fixed")
			}
			if state == "unproved" {
				result.issues = append(result.issues, "attribute constraint ID ancestry not proved")
			}
		}
	}
	checkUse := func(use *vffSKCValueAttribute) error {
		node := b.nodes[vffSKCNodeKey{use.declaration.member, use.declaration.begin}]
		state, err := idType(node, 1)
		if err != nil {
			return err
		}
		use.idType = state
		if state == "id" && use.constraint.kind != "" && use.source != use.declaration {
			result.issues = append(result.issues, "ID attribute use constraint value not proved")
		}
		return nil
	}
	for _, set := range result.attributeSets {
		idDeclarations := map[vffSKCDeclarationWitness]bool{}
		for i := range set.uses {
			use := &set.uses[i]
			if err := checkUse(use); err != nil {
				return err
			}
			if use.idType == "id" {
				idDeclarations[use.declaration] = true
				if len(idDeclarations) > 1 {
					return fmt.Errorf("multiple distinct ID-derived attributes in one scope")
				}
			}
		}
	}
	if result.attributeUse != nil {
		return checkUse(result.attributeUse)
	}
	return nil
}
