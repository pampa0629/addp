package skp

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
)

type vffSKCValue struct {
	text  string
	qname xml.Name
	list  bool
	items int
}
type vffSKCFixedValueComparison struct {
	source, declaration vffSKCDeclarationWitness
	typeName            xml.Name
	useValue, baseValue vffSKCValue
	known               bool
	primitive           xml.Name
	itemPrimitive       xml.Name
	facets              []vffSKCDeclarationWitness
	itemFacets          []vffSKCDeclarationWitness
}

// Only explicit builtins: no user facets, floating-point coercion, or source
// rewriting. Each decoded byte is charged before any scalar work.
func (b *vffSKCBinder) scalar(n *vffXMLNode, typ xml.Name, raw string) (vffSKCValue, bool, error) {
	for range len(raw) {
		if err := b.tick(0); err != nil {
			return vffSKCValue{}, false, err
		}
	}
	if typ.Space != vffXSDNS {
		return vffSKCValue{}, false, nil
	}
	text := strings.Trim(raw, " \t\r\n")
	value := vffSKCValue{}
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
	case "ID", "IDREF", "NMTOKEN":
		if text == "" {
			return value, false, fmt.Errorf("empty XSD name value")
		}
		token := typ.Local == "NMTOKEN"
		for i := range len(text) {
			ch := text[i]
			if ch >= 128 {
				return value, false, nil
			}
			if !vffSKCFilterNameByte(ch, i == 0 && !token) && !(token && ch == ':') {
				return value, false, fmt.Errorf("invalid XSD %s name value", typ.Local)
			}
		}
		value.text = text
	case "anyURI":
		known, err := vffSKCURIReference(text)
		return vffSKCValue{text: text}, known, err
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

// Checks only URI references that need no XLink escaping. The parser's result
// is discarded so URI comparison retains the original lexical representation.
func vffSKCURIReference(text string) (bool, error) {
	const hex = "0123456789abcdefABCDEF"
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '%' {
			if i+2 >= len(text) || !strings.ContainsRune(hex, rune(text[i+1])) || !strings.ContainsRune(hex, rune(text[i+2])) {
				return false, fmt.Errorf("invalid XSD URI percent escape")
			}
			i += 2
			continue
		}
		if !vffSKCFilterNameByte(ch, false) && !strings.ContainsRune("~:/?#@!$&'()*+,;=", rune(ch)) {
			return false, nil
		}
	}
	if strings.Count(text, "#") > 1 {
		return false, fmt.Errorf("multiple XSD URI fragment separators")
	}
	parsed, err := url.Parse(text)
	return err == nil && (parsed.Scheme == "" || parsed.Opaque != "" || parsed.Host != "" || parsed.Path != ""), nil
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
