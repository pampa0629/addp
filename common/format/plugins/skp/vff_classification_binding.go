package skp

// Bounded declaration witnesses, not XSD validation or SketchUp UI semantics.
import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"strings"
)

type vffSKCBindingLimits struct {
	documents     vffClassificationDocumentLimits
	xml           vffXMLLimits
	visits, depth int
}
type vffSKCDeclarationWitness struct {
	// Schema ownership, not the QName of an instance's local field. Form and
	// value-type validation remain outside this declaration witness layer.
	member, schemaNamespace, kind, name string
	begin, end                          int
	sha256                              [32]byte
}
type vffSKCFieldCandidate struct {
	source      vffSKCDeclarationWitness
	declaration vffSKCDeclarationWitness
	via         []vffSKCDeclarationWitness
	value       *vffSKCValueBinding
}
type vffSKCFieldBinding struct {
	name       string
	candidates []vffSKCFieldCandidate
}
type vffSKCRuleBinding struct {
	rule            int
	typeDeclaration *vffSKCDeclarationWitness
	fields          []vffSKCFieldBinding
	issues          []string
	scopes          []vffSKCBlacklistScope
}
type vffSKCFilterBindings struct {
	packageMember, schemaMember, filterMember string
	schemaSHA256, filterSHA256                [32]byte
	rules                                     []vffSKCRuleBinding
}
type vffSKCBindingBudget struct{ remaining, depth int }
type vffSKCBinder struct {
	ctx               context.Context
	member, namespace string
	raw               []byte
	definitions       map[string]map[string]*vffXMLNode
	anonymousTypes    []*vffXMLNode
	budget            *vffSKCBindingBudget
	active            map[*vffXMLNode]bool
	witnesses         map[*vffXMLNode]vffSKCDeclarationWitness
	issues            []string
	nodes             map[vffSKCNodeKey]*vffXMLNode
	graph             *vffSKCBindingGraph
	groupUses         []vffSKCGroupUse
	values            map[*vffXMLNode]*vffSKCValueBinding
}

func readVFFClassificationBindings(ctx context.Context, c *vffContainer, limits vffSKCBindingLimits) ([]vffSKCFilterBindings, error) {
	if limits.visits <= 0 || limits.depth <= 0 || limits.xml.bytes <= 0 || limits.xml.nodes <= 0 || limits.xml.attributes <= 0 || limits.xml.text <= 0 || limits.xml.depth <= 0 {
		return nil, fmt.Errorf("SKC bindings require explicit XML and expansion budgets")
	}
	documents, err := readVFFClassificationDocuments(ctx, c, limits.documents)
	if err != nil {
		return nil, err
	}
	xmlBudget := &vffXMLBudget{limits: limits.xml}
	visits := &vffSKCBindingBudget{limits.visits, limits.depth}
	var result []vffSKCFilterBindings
	for i, item := range documents.packages.packages {
		graph, err := newVFFSKCBindingGraph(ctx, item.files, documents.documents[i], xmlBudget, visits)
		if err != nil {
			return nil, err
		}
		for _, filter := range documents.documents[i].filters {
			schemaMember := strings.TrimSuffix(filter.member, ".filter")
			var schema *vffSKCSchemaFact
			for j := range documents.documents[i].schemas {
				fact := &documents.documents[i].schemas[j]
				if fact.member == schemaMember {
					schema = fact
					break
				}
			}
			if schema == nil || sha256.Sum256(item.files[schemaMember]) != schema.sha256 || sha256.Sum256(item.files[filter.member]) != filter.sha256 {
				return nil, fmt.Errorf("SKC binding source facts differ")
			}
			binding, err := graph.binders[schemaMember].readFilterBindings(filter)
			if err != nil {
				return nil, err
			}
			binding.packageMember = item.member
			if len(schema.dependencies) > 0 {
				for j := range binding.rules {
					binding.rules[j].issues = append(binding.rules[j].issues, "schema dependency graph not bound")
				}
			}
			result = append(result, *binding)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *vffSKCBinder) readFilterBindings(filter vffSKCFilterFact) (*vffSKCFilterBindings, error) {
	member, raw := b.member, b.raw
	result := &vffSKCFilterBindings{schemaMember: member, filterMember: filter.member, schemaSHA256: sha256.Sum256(raw), filterSHA256: filter.sha256}
	for index, rule := range filter.rules {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		b.issues = nil
		b.groupUses = nil
		fact := vffSKCRuleBinding{rule: index}
		var candidates []vffSKCFieldCandidate
		var err error
		if rule.typeName == "" {
			for _, name := range rule.attributes {
				if err := b.tick(0); err != nil {
					return nil, err
				}
				field := vffSKCFieldBinding{name: name}
				for _, kind := range []string{"element", "attribute", "attributeGroup"} {
					if node := b.definitions[kind][name]; node != nil {
						field.candidates = append(field.candidates, vffSKCFieldCandidate{source: b.witness(node), declaration: b.witness(node)})
					}
				}
				fact.fields = append(fact.fields, field)
			}
		} else if node := b.definitions["complexType"][rule.typeName]; node == nil {
			b.issues = append(b.issues, "named complex type missing: "+rule.typeName)
		} else {
			w := b.witness(node)
			fact.typeDeclaration = &w
			candidates, err = b.expand(node, nil, 1)
			if err != nil {
				return nil, err
			}
			for _, name := range rule.attributes {
				if err := b.tick(0); err != nil {
					return nil, err
				}
				field := vffSKCFieldBinding{name: name}
				for _, candidate := range candidates {
					if err := b.tick(0); err != nil {
						return nil, err
					}
					if candidate.declaration.name == name {
						field.candidates = append(field.candidates, candidate)
					}
				}
				if len(field.candidates) != 1 {
					b.issues = append(b.issues, "field declaration missing or ambiguous: "+name)
				}
				fact.fields = append(fact.fields, field)
			}
		}
		fact.issues = append(fact.issues, b.issues...)
		result.rules = append(result.rules, fact)
	}
	if err := b.bindSemantics(filter, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *vffSKCBinder) tick(depth int) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if b.budget == nil || b.budget.remaining <= 0 || depth > b.budget.depth {
		return fmt.Errorf("SKC binding expansion budget exceeded")
	}
	b.budget.remaining--
	return nil
}
func (b *vffSKCBinder) witness(n *vffXMLNode) vffSKCDeclarationWitness {
	if prior, ok := b.witnesses[n]; ok {
		return prior
	}
	owner := b.graph.owners[n]
	w := vffSKCDeclarationWitness{owner.member, owner.namespace, n.name.Local, n.attrs["name"], n.begin, n.end, sha256.Sum256(owner.raw[n.begin:n.end])}
	b.witnesses[n] = w
	b.nodes[vffSKCNodeKey{w.member, n.begin}] = n
	return w
}

func vffSKCBindingName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !vffSKCFilterNameByte(name[i], i == 0) {
			return false
		}
	}
	return true
}

func (b *vffSKCBinder) qname(n *vffXMLNode, value string) (xml.Name, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) > 2 || !vffSKCBindingName(parts[len(parts)-1]) || len(parts) == 2 && !vffSKCBindingName(parts[0]) {
		return xml.Name{}, fmt.Errorf("invalid binding QName")
	}
	prefix, local := "", parts[len(parts)-1]
	if len(parts) == 2 {
		prefix = parts[0]
	}
	namespace := ""
	found := false
	if prefix == "xml" {
		namespace, found = "http://www.w3.org/XML/1998/namespace", true
	}
	for scope := n; !found && scope != nil; scope = scope.parent {
		if err := b.tick(0); err != nil {
			return xml.Name{}, err
		}
		if uri, ok := scope.bindings[prefix]; ok {
			namespace, found = uri, true
			break
		}
	}
	if prefix != "" && !found {
		return xml.Name{}, fmt.Errorf("undeclared QName prefix")
	}
	return xml.Name{Space: namespace, Local: local}, nil
}

func (b *vffSKCBinder) lookup(n *vffXMLNode, value, kind string) (*vffXMLNode, error) {
	name, err := b.qname(n, value)
	if err != nil {
		return nil, err
	}
	symbol := kind
	if kind == "complexType" {
		symbol = "type"
	}
	target, err := b.resolveDeclaration(n, name, symbol)
	if err != nil {
		return nil, err
	}
	if target == nil || target.node.name.Local != kind {
		b.issues = append(b.issues, "unresolved "+kind+" QName: "+value)
		return nil, nil
	}
	return target.node, nil
}

func (b *vffSKCBinder) expand(n *vffXMLNode, via []vffSKCDeclarationWitness, depth int) ([]vffSKCFieldCandidate, error) {
	if err := b.tick(depth); err != nil {
		return nil, err
	}
	if b.active[n] {
		return nil, fmt.Errorf("cyclic XSD inheritance or group")
	}
	b.active[n] = true
	defer delete(b.active, n)
	via = append(append([]vffSKCDeclarationWitness(nil), via...), b.witness(n))
	var fields []vffSKCFieldCandidate
	for _, child := range n.children {
		if err := b.tick(depth); err != nil {
			return nil, err
		}
		kind := child.name.Local
		if n.name.Local == "extension" && n.parent != nil && n.parent.name.Local == "simpleContent" &&
			kind != "annotation" && kind != "attribute" && kind != "attributeGroup" {
			b.issues = append(b.issues, "unsupported simple content extension field: "+kind)
			continue
		}
		switch kind {
		case "annotation":
		case "simpleContent":
			local, err := b.expandSimpleContent(child, via, depth+1)
			if err != nil {
				return nil, err
			}
			fields = append(fields, local...)
		case "complexContent":
			if len(child.children) != 1 {
				b.issues = append(b.issues, "unsupported complex content derivation")
				continue
			}
			derivation := child.children[0]
			restricted := derivation.name.Local == "restriction"
			if restricted {
				ok, err := b.particleOnlyRestriction(child, derivation, depth)
				if err != nil {
					return nil, err
				}
				if !ok {
					b.issues = append(b.issues, "unsupported complex content restriction shape")
					continue
				}
			} else if derivation.name.Local != "extension" {
				b.issues = append(b.issues, "unsupported complex content derivation")
				continue
			}
			base, err := b.lookup(derivation, derivation.attrs["base"], "complexType")
			if err != nil {
				return nil, err
			}
			if base != nil {
				groupStart := len(b.groupUses)
				inherited, err := b.expand(base, via, depth+1)
				if err != nil {
					return nil, err
				}
				if restricted {
					for _, field := range inherited {
						if err := b.tick(depth); err != nil {
							return nil, err
						}
						if field.declaration.kind == "attribute" {
							fields = append(fields, field)
						}
					}
					groups := b.groupUses[:groupStart]
					for _, use := range b.groupUses[groupStart:] {
						if err := b.tick(depth); err != nil {
							return nil, err
						}
						if use.declaration.kind == "attributeGroup" {
							groups = append(groups, use)
						}
					}
					b.groupUses = groups
				} else {
					fields = append(fields, inherited...)
				}
			}
			local, err := b.expand(derivation, via, depth+1)
			if err != nil {
				return nil, err
			}
			fields = append(fields, local...)
		case "attribute", "element":
			parent := n.name.Local
			if kind == "element" && parent != "sequence" && parent != "choice" && parent != "all" || kind == "attribute" && parent != "complexType" && parent != "extension" && parent != "attributeGroup" {
				b.issues = append(b.issues, "unsupported field declaration position")
				continue
			}
			if kind == "attribute" {
				mode, err := vffSKCAttributeUse(child.attrs)
				if err != nil {
					return nil, err
				}
				if mode == "prohibited" {
					b.issues = append(b.issues, "prohibited field not bound")
					continue
				}
			}
			name, ref := child.attrs["name"], child.attrs["ref"]
			if (name == "") == (ref == "") {
				return nil, fmt.Errorf("field must have one name or ref")
			}
			declaration := child
			if ref != "" {
				resolved, err := b.lookup(child, ref, kind)
				if err != nil {
					return nil, err
				}
				if resolved == nil {
					continue
				}
				declaration = resolved
			} else if !vffSKCBindingName(name) {
				return nil, fmt.Errorf("invalid field name")
			}
			fields = append(fields, vffSKCFieldCandidate{source: b.witness(child), declaration: b.witness(declaration), via: append([]vffSKCDeclarationWitness(nil), via...)})
		case "attributeGroup", "group":
			parent := n.name.Local
			if kind == "attributeGroup" && parent != "complexType" && parent != "extension" && parent != "attributeGroup" || kind == "group" && parent != "complexType" && parent != "extension" && parent != "sequence" && parent != "choice" && parent != "all" {
				b.issues = append(b.issues, "unsupported group reference position")
				continue
			}
			ref := child.attrs["ref"]
			if ref == "" {
				b.issues = append(b.issues, "unsupported inline group declaration")
				continue
			}
			group, err := b.lookup(child, ref, kind)
			if err != nil {
				return nil, err
			}
			if group == nil {
				continue
			}
			b.groupUses = append(b.groupUses, vffSKCGroupUse{declaration: b.witness(group), reference: b.witness(child), via: append([]vffSKCDeclarationWitness(nil), via...)})
			more, err := b.expand(group, via, depth+1)
			if err != nil {
				return nil, err
			}
			fields = append(fields, more...)
		case "sequence", "choice", "all":
			if n.name.Local == "attributeGroup" {
				b.issues = append(b.issues, "particle inside attribute group not bound")
				continue
			}
			more, err := b.expand(child, via, depth+1)
			if err != nil {
				return nil, err
			}
			fields = append(fields, more...)
		default:
			b.issues = append(b.issues, "unsupported field structure: "+kind)
		}
	}
	return fields, nil
}

func (b *vffSKCBinder) particleOnlyRestriction(content, restriction *vffXMLNode, depth int) (bool, error) {
	if content.parent.name.Local != "complexType" || strings.TrimSpace(content.text) != "" || strings.TrimSpace(restriction.text) != "" {
		return false, nil
	}
	if mixed, declared := content.parent.attrs["mixed"]; declared && mixed != "false" && mixed != "0" {
		return false, nil
	}
	for _, sibling := range content.parent.children {
		if err := b.tick(depth); err != nil {
			return false, err
		}
		if sibling != content && sibling.name.Local != "annotation" {
			return false, nil
		}
	}
	for name := range content.attrs {
		if name != "id" {
			return false, nil
		}
	}
	for name := range restriction.attrs {
		if name != "id" && name != "base" {
			return false, nil
		}
	}
	seenSequence := false
	for _, child := range restriction.children {
		if err := b.tick(depth); err != nil {
			return false, err
		}
		if child.name.Local == "annotation" {
			continue
		}
		if child.name.Local != "sequence" || seenSequence {
			return false, nil
		}
		seenSequence = true
	}
	return true, nil
}

func (b *vffSKCBinder) expandSimpleContent(content *vffXMLNode, via []vffSKCDeclarationWitness, depth int) ([]vffSKCFieldCandidate, error) {
	if err := b.tick(depth); err != nil {
		return nil, err
	}
	onlyContent, err := b.simpleContentType(content.parent)
	if err != nil {
		return nil, err
	}
	var extension *vffXMLNode
	for _, child := range content.children {
		if err := b.tick(depth); err != nil {
			return nil, err
		}
		if child.name.Local == "annotation" {
			continue
		}
		if extension != nil || child.name.Local != "extension" {
			b.issues = append(b.issues, "unsupported simple content field derivation")
			return nil, nil
		}
		extension = child
	}
	if content.parent.name.Local != "complexType" || !onlyContent || extension == nil {
		b.issues = append(b.issues, "unsupported simple content field shape")
		return nil, nil
	}
	if strings.TrimSpace(content.text) != "" || strings.TrimSpace(extension.text) != "" {
		b.issues = append(b.issues, "unexpected text in simple content declaration")
		return nil, nil
	}
	for name := range content.attrs {
		if name != "id" {
			b.issues = append(b.issues, "unsupported simple content declaration attribute")
			return nil, nil
		}
	}
	for name := range extension.attrs {
		if name != "id" && name != "base" {
			b.issues = append(b.issues, "unsupported simple content extension attribute")
			return nil, nil
		}
	}
	name, err := b.qname(extension, extension.attrs["base"])
	if err != nil {
		return nil, err
	}
	owner := b.graph.owners[extension]
	var inherited []vffSKCFieldCandidate
	switch {
	case name.Space == vffXSDNS && vffSKCBuiltinType(name.Local) && name.Local != "anyType" && name.Local != "anySimpleType":
	case name.Space == owner.namespace && owner.definitions["simpleType"][name.Local] != nil:
	case name.Space == owner.namespace && owner.definitions["complexType"][name.Local] != nil:
		base := owner.definitions["complexType"][name.Local]
		simple, err := b.simpleContentType(base)
		if err != nil {
			return nil, err
		}
		if simple {
			inherited, err = b.expand(base, via, depth+1)
			if err != nil {
				return nil, err
			}
		} else {
			b.issues = append(b.issues, "base is not a simple-content complex type")
		}
	default:
		b.issues = append(b.issues, "unresolved simple content base QName: "+extension.attrs["base"])
	}
	local, err := b.expand(extension, append(append([]vffSKCDeclarationWitness(nil), via...), b.witness(content)), depth+1)
	if err != nil {
		return nil, err
	}
	return append(inherited, local...), nil
}
