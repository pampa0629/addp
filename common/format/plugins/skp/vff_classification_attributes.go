package skp

import (
	"encoding/xml"
	"fmt"
)

// Declaration identities in one complex type or attribute group, not a complete
// XSD attribute-use component or instance validator. Existing reference edges
// retain every group/base route, including routes to an identical use.
type vffSKCValueAttribute struct {
	name                xml.Name
	source, declaration vffSKCDeclarationWitness
}
type vffSKCValueAttributes struct {
	source vffSKCDeclarationWitness
	uses   []vffSKCValueAttribute
	names  map[xml.Name][]int
}

func (b *vffSKCBinder) valueAttribute(set *vffSKCValueAttributes, use, declaration *vffXMLNode, result *vffSKCValueBinding) error {
	name, err := b.valueAttributeName(declaration)
	if err != nil {
		return err
	}
	if set == nil || use.parent.name.Local == "schema" || use.attrs["use"] == "prohibited" {
		return nil
	}
	return b.addValueAttribute(set, vffSKCValueAttribute{name, b.witness(use), b.witness(declaration)}, result)
}

func (b *vffSKCBinder) valueAttributeName(n *vffXMLNode) (xml.Name, error) {
	owner := b.graph.owners[n]
	root := n
	for root.parent != nil {
		if err := b.tick(0); err != nil {
			return xml.Name{}, err
		}
		root = root.parent
	}
	form, formed := n.attrs["form"]
	global := n.parent != nil && n.parent.name.Local == "schema"
	_, used := n.attrs["use"]
	if global && (formed || used) {
		return xml.Name{}, fmt.Errorf("global attribute cannot have form or use")
	}
	defaultForm, defaulted := root.attrs["attributeFormDefault"]
	if formed && form != "qualified" && form != "unqualified" || defaulted && defaultForm != "qualified" && defaultForm != "unqualified" {
		return xml.Name{}, fmt.Errorf("invalid attribute form declaration")
	}
	name := xml.Name{Local: n.attrs["name"]}
	if !vffSKCBindingName(name.Local) || name.Local == "xmlns" {
		return xml.Name{}, fmt.Errorf("invalid attribute declaration name")
	}
	if global || formed && form == "qualified" || !formed && defaultForm == "qualified" {
		name.Space = owner.namespace
	}
	if name.Space == "http://www.w3.org/2001/XMLSchema-instance" {
		return xml.Name{}, fmt.Errorf("reserved attribute namespace")
	}
	return name, nil
}

func (b *vffSKCBinder) addValueAttribute(set *vffSKCValueAttributes, use vffSKCValueAttribute, result *vffSKCValueBinding) error {
	for _, index := range set.names[use.name] {
		if err := b.tick(0); err != nil {
			return err
		}
		prior := set.uses[index]
		if prior == use {
			result.issues = append(result.issues, "repeated attribute use component identity not proved")
			continue
		}
		if prior.declaration != use.declaration || set.source.kind == "attributeGroup" {
			return fmt.Errorf("conflicting attribute declarations or group uses with identical QName")
		}
		// Distinct uses of the same global declaration are not necessarily
		// equivalent (required/default/fixed). Do not select or merge one.
		result.issues = append(result.issues, "multiple uses of one attribute declaration not proved")
	}
	set.names[use.name] = append(set.names[use.name], len(set.uses))
	set.uses = append(set.uses, use)
	return nil
}

func (b *vffSKCBinder) mergeValueAttributes(to, from *vffSKCValueAttributes, result *vffSKCValueBinding) error {
	if to == nil || from == nil {
		return nil
	}
	for _, use := range from.uses {
		if err := b.tick(0); err != nil {
			return err
		}
		if err := b.addValueAttribute(to, use, result); err != nil {
			return err
		}
	}
	return nil
}
