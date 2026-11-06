package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type vffSKCSubstitution struct {
	member, head vffSKCDeclarationWitness
}

type vffSKCSubstitutionAffiliation struct {
	member vffSKCDeclarationWitness
	name   xml.Name
	head   *vffSKCDeclarationWitness
	via    []vffSKCSchemaLink
}

type vffSKCSubstitutionIssue struct {
	member vffSKCDeclarationWitness
	head   xml.Name
	reason string
}

type vffSKCSubstitutionFacts struct {
	affiliations []vffSKCSubstitutionAffiliation
	pairs        []vffSKCSubstitution
	pending      []vffSKCSubstitutionIssue
}

type vffSKCDerivationFlags uint8

const (
	vffSKCExtension vffSKCDerivationFlags = 1 << iota
	vffSKCRestriction
	vffSKCSubstitutionBlock
	vffSKCList
	vffSKCUnion
)

func vffSKCSubstitutionFlags(node *vffXMLNode, attr string) (vffSKCDerivationFlags, error) {
	value, present := node.attrs[attr]
	if !present {
		value = node.parent.attrs[attr+"Default"]
	}
	if strings.Trim(value, " \t\r\n") == "#all" {
		return vffSKCExtension | vffSKCRestriction | vffSKCSubstitutionBlock | vffSKCList | vffSKCUnion, nil
	}
	var flags vffSKCDerivationFlags
	for _, method := range strings.Fields(value) {
		switch method {
		case "extension":
			flags |= vffSKCExtension
		case "restriction":
			flags |= vffSKCRestriction
		case "substitution":
			flags |= vffSKCSubstitutionBlock
		case "list":
			flags |= vffSKCList
		case "union":
			flags |= vffSKCUnion
		default:
			return 0, fmt.Errorf("invalid XSD %s constraint", attr)
		}
	}
	return flags, nil
}

type vffSKCSubstitutionType struct {
	name xml.Name
	node *vffXMLNode
}

func (b *vffSKCBinder) substitutionType(n *vffXMLNode, raw string) (vffSKCSubstitutionType, error) {
	if raw == "" {
		return vffSKCSubstitutionType{}, nil
	}
	name, err := b.qname(n, raw)
	if err != nil {
		return vffSKCSubstitutionType{}, err
	}
	if name.Space == vffXSDNS && vffSKCBuiltinType(name.Local) {
		return vffSKCSubstitutionType{name: name}, nil
	}
	target, err := b.resolveDeclaration(n, name, "type")
	if err != nil || target == nil {
		return vffSKCSubstitutionType{}, err
	}
	return vffSKCSubstitutionType{name: name, node: target.node}, nil
}

// Checks the named derivation chain and its declared constraints. For flat
// sequences, this checks particle occurrence bounds and named type identity.
func (b *vffSKCBinder) substitutionDerivation(source, target vffSKCSubstitutionType) (methods, blocks vffSKCDerivationFlags, issue string, err error) {
	if source.name.Local == "" || target.name.Local == "" {
		return 0, 0, "XSD substitution type derivation unproved", nil
	}
	seen := map[*vffXMLNode]bool{}
	for current := source; current != target; {
		node := current.node
		if node == nil || node.name.Local != "complexType" {
			return 0, 0, "XSD substitution type derivation unproved", nil
		}
		if seen[node] {
			return 0, 0, "XSD substitution type derivation unproved", fmt.Errorf("cyclic XSD substitution type derivation")
		}
		seen[node] = true
		if err := b.tick(len(seen)); err != nil {
			return 0, 0, "XSD substitution type derivation unproved", err
		}
		var content *vffXMLNode
		for _, child := range node.children {
			if err := b.tick(0); err != nil {
				return 0, 0, "XSD substitution type derivation unproved", err
			}
			if child.name.Local == "annotation" {
				continue
			}
			if content != nil || child.name.Local != "complexContent" {
				return 0, 0, "XSD substitution type derivation unproved", nil
			}
			content = child
		}
		if content == nil || len(content.children) != 1 || strings.TrimSpace(content.text) != "" {
			return 0, 0, "XSD substitution type derivation unproved", nil
		}
		derivation := content.children[0]
		if strings.TrimSpace(derivation.text) != "" {
			return 0, 0, "XSD substitution type derivation unproved", nil
		}
		method := vffSKCExtension
		switch derivation.name.Local {
		case "extension":
		case "restriction":
			method = vffSKCRestriction
			supported, err := b.particleOnlyRestriction(content, derivation, len(seen))
			if err != nil || !supported {
				return 0, 0, "XSD substitution type derivation unproved", err
			}
		default:
			return 0, 0, "XSD substitution type derivation unproved", nil
		}
		base, err := b.substitutionType(derivation, derivation.attrs["base"])
		if err != nil || base.node == nil || base.node.name.Local != "complexType" {
			return 0, 0, "XSD substitution type derivation unproved", err
		}
		if method == vffSKCRestriction {
			issue, err := b.particleRestrictionIssue(derivation, base.node, len(seen))
			if err != nil || issue != "" {
				return 0, 0, issue, err
			}
		}
		final, err := vffSKCSubstitutionFlags(base.node, "final")
		if err != nil || final&method != 0 {
			return 0, 0, "XSD substitution type derivation unproved", err
		}
		block, err := vffSKCSubstitutionFlags(base.node, "block")
		if err != nil {
			return 0, 0, "XSD substitution type derivation unproved", err
		}
		methods |= method
		blocks |= block
		current = base
	}
	return methods, blocks, "", nil
}

func (g *vffSKCBindingGraph) readSubstitutions() (*vffSKCSubstitutionFacts, error) {
	result := &vffSKCSubstitutionFacts{}
	heads := map[*vffXMLNode]*vffXMLNode{}
	verified := map[*vffXMLNode]bool{}
	types := map[*vffXMLNode]vffSKCSubstitutionType{}
	for _, member := range g.substitutionMembers {
		binder := g.owners[member]
		name, err := binder.qname(member, member.attrs["substitutionGroup"])
		if err != nil {
			return nil, err
		}
		target, err := binder.resolveDeclaration(member, name, "element")
		if err != nil {
			return nil, err
		}
		fact := vffSKCSubstitutionAffiliation{member: binder.witness(member), name: name}
		if target == nil {
			result.affiliations = append(result.affiliations, fact)
			result.pending = append(result.pending, vffSKCSubstitutionIssue{fact.member, name, "group head unresolved"})
			continue
		}
		head := target.node
		heads[member] = head
		witness := binder.witness(head)
		fact.head, fact.via = &witness, target.via
		result.affiliations = append(result.affiliations, fact)
		for _, element := range []*vffXMLNode{member, head} {
			if _, seen := types[element]; !seen {
				for _, child := range element.children {
					if err := binder.tick(0); err != nil {
						return nil, err
					}
					if element.attrs["type"] != "" && (child.name.Local == "simpleType" || child.name.Local == "complexType") {
						return nil, fmt.Errorf("conflicting XSD substitution element type")
					}
				}
				typ, err := binder.substitutionType(element, element.attrs["type"])
				if err != nil {
					return nil, err
				}
				types[element] = typ
			}
		}
		methods, _, issue, err := binder.substitutionDerivation(types[member], types[head])
		if err != nil {
			return nil, err
		}
		final, err := vffSKCSubstitutionFlags(head, "final")
		if err != nil {
			return nil, err
		}
		if issue == "" && methods&final != 0 {
			issue = "group affiliation type derivation excluded by final"
		}
		verified[member] = issue == ""
		if !verified[member] {
			result.pending = append(result.pending, vffSKCSubstitutionIssue{fact.member, name, issue})
		}
	}

	for _, member := range g.substitutionMembers {
		binder := g.owners[member]
		abstract := false
		value, declared := member.attrs["abstract"]
		if !declared {
			value = "false"
		}
		switch strings.Trim(value, " \t\r\n") {
		case "true", "1":
			abstract = true
		case "false", "0":
		default:
			return nil, fmt.Errorf("invalid XSD abstract element flag")
		}
		seen := map[*vffXMLNode]bool{member: true}
		proved := true
		for child, head := member, heads[member]; head != nil; child, head = head, heads[head] {
			if err := binder.tick(len(seen)); err != nil {
				return nil, err
			}
			if seen[head] {
				return nil, fmt.Errorf("cyclic XSD substitution group")
			}
			seen[head] = true
			proved = proved && verified[child]
			if !proved || abstract {
				continue
			}
			block, err := vffSKCSubstitutionFlags(head, "block")
			if err != nil {
				return nil, err
			}
			if block&vffSKCSubstitutionBlock != 0 {
				continue
			}
			methods, typeBlocks, issue, err := binder.substitutionDerivation(types[member], types[head])
			if err != nil {
				return nil, err
			}
			witness := binder.witness(head)
			if issue != "" {
				name := xml.Name{Space: witness.schemaNamespace, Local: witness.name}
				result.pending = append(result.pending, vffSKCSubstitutionIssue{binder.witness(member), name, issue})
			} else if methods&(block|typeBlocks) == 0 {
				result.pairs = append(result.pairs, vffSKCSubstitution{binder.witness(member), witness})
			}
		}
	}
	return result, nil
}
