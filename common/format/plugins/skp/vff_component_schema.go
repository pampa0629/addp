package skp

import (
	"context"
	"encoding/xml"
	"fmt"
)

// Current-source declaration identity only, not instance or composition proof.
type vffComponentSchemaBinding struct {
	component   int
	declaration vffSKCDeclarationWitness
	via         []vffSKCSchemaLink
}
type vffComponentSchemaFacts struct {
	components        *vffComponentClassificationFacts
	packageReferences *vffClassificationFacts
	document          vffSKCDocumentFact
	bindings          []vffComponentSchemaBinding
}

func readVFFComponentSchemaBindings(ctx context.Context, c *vffContainer, schemaName, packageMember string, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentSchemaFacts, error) {
	if err := validateVFFSKCBindingLimits(limits); err != nil {
		return nil, err
	}
	if err := validateVFFClassificationDocumentLimits(limits.documents); err != nil {
		return nil, err
	}
	components, err := readVFFComponentClassifications(ctx, c, schemaName, componentLimits)
	if err != nil {
		return nil, err
	}
	if packageMember == "" {
		return nil, fmt.Errorf("component schema requires an explicit SKC member")
	}
	packages, err := readVFFClassificationPackageSelection(ctx, c, limits.documents.packages, packageMember)
	if err != nil {
		return nil, err
	}
	selected := &packages.packages[0] // Exact selection was established at the package boundary.
	xmlBudget := &vffXMLBudget{limits: limits.documents.xml}
	filterBudget := &vffSKCFilterBudget{limits: limits.documents.filters}
	images, dependencies := limits.documents.images, limits.documents.dependencies
	doc, err := readVFFSKCDocument(ctx, *selected, xmlBudget, filterBudget, &images, &dependencies)
	if err != nil {
		return nil, err
	}
	if doc.title != schemaName {
		return nil, fmt.Errorf("component classification name differs from selected SKC title")
	}
	graph, err := newVFFSKCBindingGraph(ctx, selected.files, *doc, &vffXMLBudget{limits: limits.xml}, &vffSKCBindingBudget{limits.visits, limits.depth})
	if err != nil {
		return nil, err
	}
	root := graph.binders[doc.rootSchema]
	result := &vffComponentSchemaFacts{components: components, packageReferences: packages.references, document: *doc}
	for i, component := range components.bindings {
		if err := root.tick(0); err != nil {
			return nil, err
		}
		// This observed SKP string is namespace URI + ':' + local type, not
		// an XML QName whose prefix can be resolved or a location to fetch.
		if !vffSKCBindingName(component.schemaType) || component.declaredType != root.namespace+":"+component.schemaType || root.namespace == "" {
			return nil, fmt.Errorf("component SchemaType differs from exact root schema namespace")
		}
		target, err := root.resolveDeclaration(root.root, xml.Name{Space: root.namespace, Local: component.schemaType}, "type")
		if err != nil {
			return nil, err
		}
		if target == nil || target.node.name.Local != "complexType" {
			return nil, fmt.Errorf("component classification has no unique complex type declaration")
		}
		result.bindings = append(result.bindings, vffComponentSchemaBinding{component: i, declaration: root.witness(target.node), via: target.via})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
