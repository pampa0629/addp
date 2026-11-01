package skp

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type vffSKCScalar struct {
	text  string
	qname xml.Name
}
type vffSKCFixedValueComparison struct {
	source, declaration vffSKCDeclarationWitness
	typeName            xml.Name
	useValue, baseValue vffSKCScalar
	known               bool
	primitive           xml.Name
	facets              []vffSKCDeclarationWitness
}

// Only explicit builtins: no user facets, floating-point coercion, or source
// rewriting. Each decoded byte is charged before any scalar work.
func (b *vffSKCBinder) scalar(n *vffXMLNode, typ xml.Name, raw string) (vffSKCScalar, bool, error) {
	for range len(raw) {
		if err := b.tick(0); err != nil {
			return vffSKCScalar{}, false, err
		}
	}
	if typ.Space != vffXSDNS {
		return vffSKCScalar{}, false, nil
	}
	text := strings.Trim(raw, " \t\r\n")
	value := vffSKCScalar{}
	switch typ.Local {
	case "string":
		// XML attribute normalization distinguishes literal whitespace from
		// character references. Decoded text alone cannot recover that fact.
		if strings.ContainsAny(raw, "\t\r\n") {
			return value, false, nil
		}
		value.text = raw
	case "normalizedString":
		value.text = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(raw)
	case "token":
		value.text = strings.Join(strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }), " ")
	case "boolean":
		switch text {
		case "true", "1":
			value.text = "true"
		case "false", "0":
			value.text = "false"
		default:
			return value, false, fmt.Errorf("invalid fixed boolean value")
		}
	case "QName":
		for _, r := range text {
			if r >= 128 {
				return value, false, nil
			}
		}
		name, err := b.qname(n, text)
		if err != nil {
			return value, false, err
		}
		value.qname = name
	default:
		min, max, integer, supported := vffSKCIntegerBounds(typ.Local)
		if typ.Local != "decimal" && !supported {
			return value, false, nil
		}
		canonical, err := vffSKCDecimal(text, integer)
		if err != nil {
			return value, false, err
		}
		if min != "" && vffSKCCompareInteger(canonical, min) < 0 || max != "" && vffSKCCompareInteger(canonical, max) > 0 {
			return value, false, fmt.Errorf("fixed integer value outside builtin range")
		}
		value.text = canonical
	}
	return value, true, nil
}

func vffSKCDecimal(raw string, integer bool) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	text := raw
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	digits, dots := 0, 0
	for _, c := range text {
		if c == '.' && !integer {
			dots++
		} else if c >= '0' && c <= '9' {
			digits++
		} else {
			return "", fmt.Errorf("invalid fixed decimal or integer value")
		}
	}
	if digits == 0 || dots > 1 {
		return "", fmt.Errorf("invalid fixed decimal or integer value")
	}
	whole, fraction, _ := strings.Cut(text, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(fraction, "0")
	canonical := whole
	if fraction != "" {
		canonical += "." + fraction
	}
	if negative && canonical != "0" {
		canonical = "-" + canonical
	}
	return canonical, nil
}

// Canonical signed integers only. Never called for a non-integer type.
func vffSKCCompareInteger(a, b string) int {
	negA, negB := strings.HasPrefix(a, "-"), strings.HasPrefix(b, "-")
	if negA != negB {
		if negA {
			return -1
		}
		return 1
	}
	x, y := strings.TrimPrefix(a, "-"), strings.TrimPrefix(b, "-")
	cmp := strings.Compare(x, y)
	if len(x) < len(y) {
		cmp = -1
	} else if len(x) > len(y) {
		cmp = 1
	}
	if negA {
		return -cmp
	}
	return cmp
}

func vffSKCIntegerBounds(name string) (min, max string, integer, supported bool) {
	integer, supported = true, true
	switch name {
	case "integer":
	case "nonPositiveInteger":
		max = "0"
	case "negativeInteger":
		max = "-1"
	case "long":
		min, max = "-9223372036854775808", "9223372036854775807"
	case "int":
		min, max = "-2147483648", "2147483647"
	case "short":
		min, max = "-32768", "32767"
	case "byte":
		min, max = "-128", "127"
	case "nonNegativeInteger":
		min = "0"
	case "unsignedLong":
		min, max = "0", "18446744073709551615"
	case "unsignedInt":
		min, max = "0", "4294967295"
	case "unsignedShort":
		min, max = "0", "65535"
	case "unsignedByte":
		min, max = "0", "255"
	case "positiveInteger":
		min = "1"
	default:
		integer, supported = false, false
	}
	return
}
