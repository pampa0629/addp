package skp

import "sort"

type vffSKCGroupUse struct {
	declaration, reference vffSKCDeclarationWitness
	via                    []vffSKCDeclarationWitness
}
type vffSKCBlacklistGroup struct {
	name string
	uses []vffSKCGroupUse
}
type vffSKCBlacklistScope struct {
	typeDeclaration vffSKCDeclarationWitness
	element         *vffSKCDeclarationWitness
	fields          []vffSKCFieldBinding
	groups          []vffSKCBlacklistGroup
	issues          []string
}

// This is the declared local type universe, not SketchUp's UI eligibility
// rules. Every scope remains separate, including an empty candidate set.
func (b *vffSKCBinder) bindSemantics(filter vffSKCFilterFact, result *vffSKCFilterBindings) error {
	white := map[string]bool{}
	for i, rule := range filter.rules {
		if rule.typeName != "" {
			white[rule.typeName] = true
		}
		for j := range result.rules[i].fields {
			if err := b.bindValues(&result.rules[i].fields[j]); err != nil {
				return err
			}
		}
	}
	var names []string
	for name := range b.definitions["complexType"] {
		if err := b.tick(0); err != nil {
			return err
		}
		if !white[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	types := make([]*vffXMLNode, 0, len(names)+len(b.anonymousTypes))
	for _, name := range names {
		types = append(types, b.definitions["complexType"][name])
	}
	types = append(types, b.anonymousTypes...)
	for i, rule := range filter.rules {
		if rule.typeName != "" {
			continue
		}
		binding := &result.rules[i]
		scoped := make([]bool, len(rule.attributes))
		for _, node := range types {
			b.issues = nil
			b.groupUses = nil
			candidates, err := b.expand(node, nil, 1)
			if err != nil {
				return err
			}
			scope := vffSKCBlacklistScope{typeDeclaration: b.witness(node), issues: append([]string(nil), b.issues...)}
			if scope.typeDeclaration.name == "" {
				element := b.witness(node.parent)
				scope.element = &element
			}
			for j, fieldName := range rule.attributes {
				if err := b.tick(0); err != nil {
					return err
				}
				field := vffSKCFieldBinding{name: fieldName}
				group := vffSKCBlacklistGroup{name: fieldName}
				for _, candidate := range candidates {
					if err := b.tick(0); err != nil {
						return err
					}
					if candidate.declaration.name == fieldName {
						field.candidates = append(field.candidates, candidate)
					}
				}
				for _, use := range b.groupUses {
					if err := b.tick(0); err != nil {
						return err
					}
					if use.declaration.name == fieldName {
						group.uses = append(group.uses, use)
					}
				}
				if err := b.bindValues(&field); err != nil {
					return err
				}
				scoped[j] = scoped[j] || len(field.candidates) > 0 || len(group.uses) > 0
				scope.fields = append(scope.fields, field)
				scope.groups = append(scope.groups, group)
			}
			binding.scopes = append(binding.scopes, scope)
		}
		for j, field := range binding.fields {
			switch count := len(field.candidates); {
			case count > 1:
				binding.issues = append(binding.issues, "blacklist global declaration ambiguous: "+field.name)
			case count == 0 && !scoped[j]:
				binding.issues = append(binding.issues, "blacklist declaration not found: "+field.name)
			}
		}
	}
	return nil
}

func (b *vffSKCBinder) bindValues(field *vffSKCFieldBinding) error {
	for i := range field.candidates {
		if err := b.tick(0); err != nil {
			return err
		}
		candidate := &field.candidates[i]
		node := b.nodes[vffSKCNodeKey{candidate.source.member, candidate.source.begin}]
		if node == nil || node.name.Local != "element" && node.name.Local != "attribute" {
			continue
		}
		value, err := b.readValueType(node)
		if err != nil {
			return err
		}
		candidate.value = value
	}
	return nil
}
