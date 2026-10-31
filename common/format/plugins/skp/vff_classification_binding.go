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
	declaration vffSKCDeclarationWitness
	via         []vffSKCDeclarationWitness
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
	budget            *vffSKCBindingBudget
	active            map[*vffXMLNode]bool
	witnesses         map[*vffXMLNode]vffSKCDeclarationWitness
	issues            []string
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
			binding, err := readVFFSKCFilterBindings(ctx, schemaMember, item.files[schemaMember], filter, xmlBudget, visits)
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

func readVFFSKCFilterBindings(ctx context.Context, member string, raw []byte, filter vffSKCFilterFact, xmlBudget *vffXMLBudget, budget *vffSKCBindingBudget) (*vffSKCFilterBindings, error) {
	root, err := readVFFXML(ctx, raw, xmlBudget, map[string]bool{vffXSDNS: true}, nil)
	if err != nil {
		return nil, err
	}
	if root.name != (xml.Name{Space: vffXSDNS, Local: "schema"}) {
		return nil, fmt.Errorf("binding requires exact XSD root")
	}
	b := &vffSKCBinder{ctx: ctx, member: member, namespace: root.attrs["targetNamespace"], raw: raw, definitions: map[string]map[string]*vffXMLNode{}, budget: budget, active: map[*vffXMLNode]bool{}, witnesses: map[*vffXMLNode]vffSKCDeclarationWitness{}}
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
	result := &vffSKCFilterBindings{schemaMember: member, filterMember: filter.member, schemaSHA256: sha256.Sum256(raw), filterSHA256: filter.sha256}
	for index, rule := range filter.rules {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		b.issues = nil
		fact := vffSKCRuleBinding{rule: index}
		var candidates []vffSKCFieldCandidate
		if rule.typeName == "" {
			for _, name := range rule.attributes {
				if err := b.tick(0); err != nil {
					return nil, err
				}
				field := vffSKCFieldBinding{name: name}
				for _, kind := range []string{"element", "attribute", "attributeGroup"} {
					if node := b.definitions[kind][name]; node != nil {
						field.candidates = append(field.candidates, vffSKCFieldCandidate{declaration: b.witness(node)})
					}
				}
				if len(field.candidates) != 1 {
					b.issues = append(b.issues, "blacklist global declaration missing or ambiguous: "+name)
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
	w := vffSKCDeclarationWitness{b.member, b.namespace, n.name.Local, n.attrs["name"], n.begin, n.end, sha256.Sum256(b.raw[n.begin:n.end])}
	b.witnesses[n] = w
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

func (b *vffSKCBinder) lookup(n *vffXMLNode, value, kind string) (*vffXMLNode, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) > 2 || !vffSKCBindingName(parts[len(parts)-1]) || len(parts) == 2 && !vffSKCBindingName(parts[0]) {
		return nil, fmt.Errorf("invalid binding QName")
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
			return nil, err
		}
		if uri, ok := scope.bindings[prefix]; ok {
			namespace, found = uri, true
			break
		}
	}
	if prefix != "" && !found {
		return nil, fmt.Errorf("undeclared QName prefix")
	}
	if namespace != b.namespace || b.definitions[kind][local] == nil {
		b.issues = append(b.issues, "unresolved "+kind+" QName: "+value)
		return nil, nil
	}
	return b.definitions[kind][local], nil
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
		switch kind := child.name.Local; kind {
		case "annotation":
		case "complexContent":
			if len(child.children) != 1 || child.children[0].name.Local != "extension" {
				b.issues = append(b.issues, "unsupported complex content derivation")
				continue
			}
			extension := child.children[0]
			base, err := b.lookup(extension, extension.attrs["base"], "complexType")
			if err != nil {
				return nil, err
			}
			if base != nil {
				inherited, err := b.expand(base, via, depth+1)
				if err != nil {
					return nil, err
				}
				fields = append(fields, inherited...)
			}
			local, err := b.expand(extension, via, depth+1)
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
			if child.attrs["use"] == "prohibited" {
				b.issues = append(b.issues, "prohibited field not bound")
				continue
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
			fields = append(fields, vffSKCFieldCandidate{b.witness(declaration), append([]vffSKCDeclarationWitness(nil), via...)})
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
