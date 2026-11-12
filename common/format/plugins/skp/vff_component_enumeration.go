package skp

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
)

type vffComponentEnumerationOption struct {
	vffComponentOptionString
	value        vffSKCValue
	declarations []vffSKCDeclarationWitness
}
type vffComponentEnumerationFact struct {
	component       int
	path            vffComponentFieldPath
	field           vffSKCFieldCandidate
	typeDeclaration vffSKCDeclarationWitness
	primitive       xml.Name
	facets          []vffSKCDeclarationWitness
	metadata        vffComponentFieldMetadata
	options         []vffComponentEnumerationOption
}
type vffComponentEnumerationFacts struct {
	schema       *vffComponentSchemaFacts
	enumerations []vffComponentEnumerationFact
	opaque       []vffOpaqueRecord
}

// Preserve all facet witnesses while comparing distinct values, not lexical
// spellings. Inconsistent or unproved final profiles are never approximated.
func (p *vffComponentStringField) enumerationSet() (map[vffSKCValue][]vffSKCDeclarationWitness, error) {
	if len(p.profile.levels) == 0 {
		return nil, fmt.Errorf("enumeration metadata requires an explicit enumeration facet")
	}
	b := p.checker.b
	result := map[vffSKCValue][]vffSKCDeclarationWitness{}
	for _, candidate := range p.profile.levels[len(p.profile.levels)-1] {
		node := b.nodes[vffSKCNodeKey{candidate.source.member, candidate.source.begin}]
		value, known, err := p.checker.evaluate(p.profile, node, node.attrs["value"])
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf("final enumeration member remains unproved")
		}
		result[value] = nil
	}
	for _, level := range p.profile.levels {
		for _, candidate := range level {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if witnesses, exists := result[candidate.value]; exists {
				result[candidate.value] = append(witnesses, candidate.source)
			}
		}
	}
	return result, nil
}

// Metadata option sets only. The selected value, complete XML instance and
// actual SketchUp UI behavior are outside this private declaration proof.
func readVFFComponentEnumerationMetadata(ctx context.Context, c *vffContainer, schemaName, packageMember, componentType string, paths []vffComponentFieldPath, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentEnumerationFacts, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("enumeration metadata requires explicit paths")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !vffSKCBindingName(path.field) || !vffSKCBindingName(path.valueType) || seen[path.field] {
			return nil, fmt.Errorf("enumeration paths must be unique explicit names")
		}
		seen[path.field] = true
	}
	scope, err := readVFFComponentFieldScope(ctx, c, schemaName, packageMember, componentType, componentLimits, limits)
	if err != nil {
		return nil, err
	}
	type plan struct {
		field    vffComponentStringField
		expected map[vffSKCValue][]vffSKCDeclarationWitness
	}
	var plans []plan
	for _, path := range paths {
		field, err := scope.stringField(path)
		if err != nil {
			return nil, err
		}
		expected, err := field.enumerationSet()
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan{field, expected})
	}
	r := &vffComponentClassificationReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: componentLimits.records, maxDepth: componentLimits.depth}, result: &vffComponentClassificationFacts{}}
	result := &vffComponentEnumerationFacts{schema: scope.schema}
	for _, component := range scope.selected {
		dictionaries, err := r.nestedDictionaries(scope.schema.components.bindings[component].nestedDictionary, 13)
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			field, exists := dictionaries[p.field.path.field]
			if !exists {
				return nil, fmt.Errorf("enumeration field dictionary is missing")
			}
			leaves, err := r.nestedDictionaries(field.nested, 19)
			if err != nil {
				return nil, err
			}
			leaf, exists := leaves[p.field.path.valueType]
			if !exists {
				return nil, fmt.Errorf("enumeration value type dictionary is missing")
			}
			metadata, err := r.fieldMetadata(scope.schema, componentType, p.field.path.field, field, leaf, "enumeration")
			if err != nil {
				return nil, err
			}
			items, err := r.optionStrings(leaf)
			if err != nil {
				return nil, err
			}
			if len(items) != len(p.expected) {
				return nil, fmt.Errorf("enumeration options do not cover the proved value set")
			}
			facets, _, err := scope.b.profileFacets(p.field.profile)
			if err != nil {
				return nil, err
			}
			fact := vffComponentEnumerationFact{component: component, path: p.field.path, field: p.field.field, typeDeclaration: p.field.declaration, primitive: p.field.profile.primitive, facets: facets, metadata: metadata}
			used := map[vffSKCValue]bool{}
			for _, item := range items {
				value, known, err := p.field.checker.evaluate(p.field.profile, p.field.node, item.raw)
				if err != nil {
					return nil, err
				}
				if !known {
					return nil, fmt.Errorf("enumeration option typed value remains unproved")
				}
				declarations, exists := p.expected[value]
				if !exists || used[value] {
					return nil, fmt.Errorf("enumeration option is duplicate or outside the proved set")
				}
				used[value] = true
				fact.options = append(fact.options, vffComponentEnumerationOption{item, value, declarations})
			}
			for _, entry := range field.entries {
				r.keep([]vffClassificationRecord{entry})
			}
			for key, entry := range leaf.entries {
				if key != "attribute_type" && key != "is_hidden" && key != "options" {
					r.keep([]vffClassificationRecord{entry})
				}
			}
			result.enumerations = append(result.enumerations, fact)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.opaque = r.result.opaque
	sort.Slice(result.opaque, func(i, j int) bool { return result.opaque[i].offset < result.opaque[j].offset })
	return result, nil
}
