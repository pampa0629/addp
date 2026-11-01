package skp

import (
	"fmt"
	"math/big"
	"strings"
)

// Ordered declaration facts, not expanded instance values or XSD validity.
// parent is the exact XML parent offset, including when a reference crosses
// to a separate global declaration. Reference edges retain that distinction.
type vffSKCValueStructure struct {
	source               vffSKCDeclarationWitness
	parent               int
	attributes           map[string]string
	minOccurs, maxOccurs string
}

func (b *vffSKCBinder) valueStructure(n *vffXMLNode, result *vffSKCValueBinding) error {
	fact := vffSKCValueStructure{source: b.witness(n), parent: -1, attributes: map[string]string{}}
	if n.parent != nil {
		fact.parent = n.parent.begin
	}
	kind := n.name.Local
	allowed := " id "
	switch kind {
	case "complexType":
		allowed += "name "
	case "extension":
		allowed += "base "
	case "attributeGroup":
		allowed += "name ref "
	case "sequence", "choice", "all":
		allowed += "minOccurs maxOccurs "
	case "element":
		allowed += "name ref type minOccurs maxOccurs nillable form default fixed "
	case "attribute":
		allowed += "name ref type use form default fixed "
	}
	for name, raw := range n.attrs {
		if err := b.tick(0); err != nil {
			return err
		}
		fact.attributes[name] = raw
		if !strings.Contains(allowed, " "+name+" ") {
			result.issues = append(result.issues, "uninterpreted complex value declaration attribute: "+name)
		}
	}
	if kind == "attribute" {
		if _, err := vffSKCAttributeUse(n.attrs); err != nil {
			return err
		}
	} else if _, fixed := n.attrs["fixed"]; fixed {
		if _, def := n.attrs["default"]; def {
			return fmt.Errorf("conflicting complex value default and fixed declarations")
		}
	}
	if kind == "sequence" || kind == "choice" || kind == "all" || kind == "element" && n.parent != nil && n.parent.name.Local != "schema" {
		min, err := vffSKCOccurs(n.attrs, "minOccurs", false)
		if err != nil {
			return err
		}
		max, err := vffSKCOccurs(n.attrs, "maxOccurs", true)
		if err != nil {
			return err
		}
		if max != "unbounded" {
			lo, _ := new(big.Int).SetString(min, 10)
			hi, _ := new(big.Int).SetString(max, 10)
			if lo.Cmp(hi) > 0 {
				return fmt.Errorf("reversed complex value occurrence bounds")
			}
		}
		fact.minOccurs, fact.maxOccurs = min, max
	}
	result.structures = append(result.structures, fact)
	return nil
}

func vffSKCOccurs(attrs map[string]string, key string, unbounded bool) (string, error) {
	raw, exists := attrs[key]
	if !exists {
		return "1", nil
	}
	raw = strings.Trim(raw, " \t\r\n")
	if unbounded && raw == "unbounded" {
		return raw, nil
	}
	digits := strings.TrimPrefix(strings.TrimPrefix(raw, "+"), "-")
	if digits == "" {
		return "", fmt.Errorf("invalid complex value occurrence bound")
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("invalid complex value occurrence bound")
		}
	}
	number, ok := new(big.Int).SetString(raw, 10)
	if !ok || number.Sign() < 0 {
		return "", fmt.Errorf("invalid complex value occurrence bound")
	}
	return number.String(), nil
}
