package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Declaration identities in one complex type or attribute group, not a complete
// XSD attribute-use component or instance validator. Existing reference edges
// retain every group/base route, including routes to an identical use.
type vffSKCValueAttribute struct {
	name                xml.Name
	source, declaration vffSKCDeclarationWitness
	use                 string
	constraint          vffSKCAttributeConstraint
}

// Decoded declaration text and its source, never a typed/canonical value.
// Empty kind means absent; an empty lexical value can be explicitly present.
type vffSKCAttributeConstraint struct {
	kind, lexical string
	source        vffSKCDeclarationWitness
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
	mode, err := vffSKCAttributeUse(use.attrs)
	if err != nil {
		return err
	}
	if mode == "prohibited" {
		return nil
	}
	constraint, err := b.attributeConstraint(use, declaration, result)
	if err != nil {
		return err
	}
	if use.parent.name.Local == "schema" {
		return nil
	}
	fact := vffSKCValueAttribute{name: name, source: b.witness(use), declaration: b.witness(declaration), use: mode, constraint: constraint}
	if set == nil {
		result.attributeUse = &fact
		return nil
	}
	return b.addValueAttribute(set, fact, result)
}

// One declaration-combination rule for both structural facts and actual uses.
func vffSKCAttributeUse(attrs map[string]string) (string, error) {
	mode := "optional"
	if raw, present := attrs["use"]; present {
		mode = strings.Trim(raw, " \t\r\n")
		if mode != "optional" && mode != "required" && mode != "prohibited" {
			return "", fmt.Errorf("invalid complex value attribute use")
		}
	}
	_, def := attrs["default"]
	_, fixed := attrs["fixed"]
	if def && fixed {
		return "", fmt.Errorf("conflicting complex value default and fixed declarations")
	}
	if def && mode != "optional" {
		return "", fmt.Errorf("complex value attribute default requires optional use")
	}
	return mode, nil
}

func (b *vffSKCBinder) attributeConstraint(use, declaration *vffXMLNode, result *vffSKCValueBinding) (vffSKCAttributeConstraint, error) {
	if _, err := vffSKCAttributeUse(declaration.attrs); err != nil {
		return vffSKCAttributeConstraint{}, err
	}
	read := func(n *vffXMLNode) vffSKCAttributeConstraint {
		for _, kind := range []string{"default", "fixed"} {
			if raw, present := n.attrs[kind]; present {
				return vffSKCAttributeConstraint{kind, raw, b.witness(n)}
			}
		}
		return vffSKCAttributeConstraint{}
	}
	local, global := read(use), read(declaration)
	if use != declaration && global.kind == "fixed" && local.kind != "" {
		if local.kind != "fixed" {
			return vffSKCAttributeConstraint{}, fmt.Errorf("attribute use cannot replace a fixed declaration with default")
		}
		// Equal strings can be different QName values in different scopes;
		// different strings can denote equal numeric/boolean values.
		result.issues = append(result.issues, "fixed attribute reference typed value equality not proved")
	}
	if local.kind != "" {
		return local, nil
	}
	return global, nil
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
