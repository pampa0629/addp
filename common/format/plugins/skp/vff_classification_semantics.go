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
	for i, rule := range filter.rules {
		if rule.typeName != "" {
			continue
		}
		for _, name := range names {
			b.issues = nil
			b.groupUses = nil
			node := b.definitions["complexType"][name]
			candidates, err := b.expand(node, nil, 1)
			if err != nil {
				return err
			}
			scope := vffSKCBlacklistScope{typeDeclaration: b.witness(node), issues: append([]string(nil), b.issues...)}
			for _, fieldName := range rule.attributes {
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
				scope.fields = append(scope.fields, field)
				scope.groups = append(scope.groups, group)
			}
			result.rules[i].scopes = append(result.rules[i].scopes, scope)
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
