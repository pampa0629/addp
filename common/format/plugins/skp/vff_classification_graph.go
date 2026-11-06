package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
)

type vffSKCNodeKey struct {
	member string
	begin  int
}
type vffSKCBindingEdge struct {
	link            vffSKCSchemaLink
	kind, namespace string
}
type vffSKCBindingGraph struct {
	binders map[string]*vffSKCBinder
	owners  map[*vffXMLNode]*vffSKCBinder
	edges   map[string][]vffSKCBindingEdge
	facts   map[string]*vffSKCSchemaFact
}
type vffSKCDeclarationScope struct {
	owner *vffSKCBinder
	via   []vffSKCSchemaLink
}
type vffSKCDeclarationTarget struct {
	node *vffXMLNode
	via  []vffSKCSchemaLink
}

// One graph per freshly read exact SKC. It never loads a location or adds a
// schema by namespace, name, digest, or an import from another document.
func newVFFSKCBindingGraph(ctx context.Context, files map[string][]byte, doc vffSKCDocumentFact, xmlBudget *vffXMLBudget, budget *vffSKCBindingBudget) (*vffSKCBindingGraph, error) {
	g := &vffSKCBindingGraph{binders: map[string]*vffSKCBinder{}, owners: map[*vffXMLNode]*vffSKCBinder{}, edges: map[string][]vffSKCBindingEdge{}, facts: map[string]*vffSKCSchemaFact{}}
	for i := range doc.schemas {
		fact := &doc.schemas[i]
		raw, exists := files[fact.member]
		if !exists || g.binders[fact.member] != nil || sha256.Sum256(raw) != fact.sha256 {
			return nil, fmt.Errorf("SKC value graph requires unique current schema bytes")
		}
		root, err := readVFFXML(ctx, raw, xmlBudget, map[string]bool{vffXSDNS: true}, nil)
		if err != nil {
			return nil, err
		}
		if root.name != (xml.Name{Space: vffXSDNS, Local: "schema"}) {
			return nil, fmt.Errorf("binding requires exact XSD root")
		}
		b := &vffSKCBinder{ctx: ctx, member: fact.member, namespace: root.attrs["targetNamespace"], raw: raw, definitions: map[string]map[string]*vffXMLNode{}, budget: budget, active: map[*vffXMLNode]bool{}, witnesses: map[*vffXMLNode]vffSKCDeclarationWitness{}, nodes: map[vffSKCNodeKey]*vffXMLNode{}, values: map[*vffXMLNode]*vffSKCValueBinding{}, graph: g}
		if b.namespace == vffXSDNS {
			return nil, fmt.Errorf("schema overriding the builtin XSD namespace is unsupported")
		}
		g.binders[fact.member] = b
		g.facts[fact.member] = fact
		var index func(*vffXMLNode, bool) error
		index = func(n *vffXMLNode, inAnnotation bool) error {
			if err := b.tick(0); err != nil {
				return err
			}
			g.owners[n] = b
			if !inAnnotation && n.name.Local == "complexType" && n.parent != nil && n.parent.name.Local == "element" {
				if _, named := n.attrs["name"]; !named {
					parent := n.parent
					_, typed := parent.attrs["type"]
					_, referenced := parent.attrs["ref"]
					if !vffSKCBindingName(parent.attrs["name"]) || typed || referenced {
						return fmt.Errorf("conflicting anonymous XSD type owner")
					}
					for _, sibling := range parent.children {
						if err := b.tick(0); err != nil {
							return err
						}
						if sibling != n && (sibling.name.Local == "simpleType" || sibling.name.Local == "complexType") {
							return fmt.Errorf("multiple inline XSD types for one element")
						}
					}
					b.anonymousTypes = append(b.anonymousTypes, n)
				}
			}
			inAnnotation = inAnnotation || n.name.Local == "annotation"
			for _, child := range n.children {
				if err := index(child, inAnnotation); err != nil {
					return err
				}
			}
			return nil
		}
		if err := index(root, false); err != nil {
			return nil, err
		}
		for _, child := range root.children {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			kind := child.name.Local
			if kind != "complexType" && kind != "simpleType" && kind != "element" && kind != "attribute" && kind != "attributeGroup" && kind != "group" {
				continue
			}
			name := child.attrs["name"]
			if !vffSKCBindingName(name) || child.attrs["ref"] != "" {
				return nil, fmt.Errorf("unsupported XSD declaration name")
			}
			if b.definitions[kind] == nil {
				b.definitions[kind] = map[string]*vffXMLNode{}
			}
			if b.definitions[kind][name] != nil || kind == "complexType" && b.definitions["simpleType"][name] != nil || kind == "simpleType" && b.definitions["complexType"][name] != nil {
				return nil, fmt.Errorf("duplicate XSD declaration in one symbol space")
			}
			b.definitions[kind][name] = child
		}
	}
	for _, link := range doc.links {
		source := g.binders[link.source.member]
		if source == nil || g.facts[source.member].sha256 != link.source.sha256 {
			return nil, fmt.Errorf("SKC value graph dependency source differs")
		}
		if err := source.tick(0); err != nil {
			return nil, err
		}
		fact := g.facts[source.member]
		var dependency *vffSKCDependency
		if link.dependency >= 0 && link.dependency < len(fact.dependencies) {
			dependency = &fact.dependencies[link.dependency]
		}

		if dependency == nil || dependency.begin != link.begin || dependency.end != link.end || dependency.member != link.target.member || dependency.sha256 != link.target.sha256 || link.begin < 0 || link.end <= link.begin || link.end > len(source.raw) || sha256.Sum256(source.raw[link.begin:link.end]) != link.declarationSHA256 {
			return nil, fmt.Errorf("SKC value graph dependency identity differs")
		}
		if link.namespaceChecked {
			target := g.binders[link.target.member]
			if target == nil || g.facts[target.member].sha256 != link.target.sha256 || link.sourceNamespace != source.namespace || link.targetNamespace != target.namespace || len(link.issues) != 0 {
				return nil, fmt.Errorf("SKC value graph dependency target differs")
			}
		}
		g.edges[source.member] = append(g.edges[source.member], vffSKCBindingEdge{link, dependency.kind, dependency.namespace})
	}
	return g, nil
}

// This bounded subset follows forward includes only. Reverse include assembly,
// chameleon namespaces and redefine semantics remain explicitly unproved.
func (b *vffSKCBinder) declarationIncludes(start *vffSKCBinder, via []vffSKCSchemaLink) ([]vffSKCDeclarationScope, bool, error) {
	queue := []vffSKCDeclarationScope{{owner: start, via: via}}
	seen := map[string]bool{}
	var scopes []vffSKCDeclarationScope
	complete := true
	for i := 0; i < len(queue); i++ {
		route := queue[i]
		owner := route.owner
		if seen[owner.member] {
			continue
		}
		seen[owner.member] = true
		if err := b.tick(len(route.via)); err != nil {
			return nil, false, err
		}
		scopes = append(scopes, route)
		for _, edge := range b.graph.edges[owner.member] {
			if err := b.tick(0); err != nil {
				return nil, false, err
			}
			if edge.kind != "include" && edge.kind != "redefine" {
				continue
			}
			if !edge.link.namespaceChecked || edge.kind != "include" {
				complete = false
				continue
			}
			queue = append(queue, vffSKCDeclarationScope{owner: b.graph.binders[edge.link.target.member], via: append(append([]vffSKCSchemaLink(nil), route.via...), edge.link)})
		}
	}
	return scopes, complete, nil
}

func (b *vffSKCBinder) resolveDeclaration(source *vffXMLNode, name xml.Name, kind string) (*vffSKCDeclarationTarget, error) {
	owner := b.graph.owners[source]
	var starts []vffSKCDeclarationScope
	complete := true
	if name.Space == owner.namespace {
		for _, edge := range b.graph.edges[owner.member] {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if edge.kind == "import" && edge.namespace == name.Space && !edge.link.namespaceChecked {
				return nil, nil
			}
		}
		start, ok, err := b.declarationIncludes(owner, nil)
		if err != nil {
			return nil, err
		}
		starts, complete = start, ok
	} else {
		_, ok, err := b.declarationIncludes(owner, nil)
		if err != nil {
			return nil, err
		}
		complete = ok
		for _, edge := range b.graph.edges[owner.member] {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if edge.kind != "import" || edge.namespace != name.Space {
				continue
			}
			if !edge.link.namespaceChecked {
				complete = false
				continue
			}
			start, ok, err := b.declarationIncludes(b.graph.binders[edge.link.target.member], []vffSKCSchemaLink{edge.link})
			if err != nil {
				return nil, err
			}
			starts = append(starts, start...)
			complete = complete && ok
		}
	}
	if !complete {
		return nil, nil
	}
	var target *vffSKCDeclarationTarget
	for _, scope := range starts {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		binder := scope.owner
		kinds := []string{kind}
		if kind == "type" {
			kinds = []string{"simpleType", "complexType"}
		}
		for _, symbol := range kinds {
			node := binder.definitions[symbol][name.Local]
			if node == nil {
				continue
			}
			if target != nil && target.node != node {
				return nil, fmt.Errorf("ambiguous cross-schema declaration")
			}
			if target == nil {
				target = &vffSKCDeclarationTarget{node, scope.via}
			}
		}
	}
	return target, nil
}
