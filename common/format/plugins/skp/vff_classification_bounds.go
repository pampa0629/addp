package skp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

func vffSKCBoundFacet(name string) bool {
	switch name {
	case "minInclusive", "minExclusive", "maxInclusive", "maxExclusive", "length", "minLength", "maxLength", "totalDigits", "fractionDigits":
		return true
	}
	return false
}

// Bounds are projected from the existing resolved type graph. Cached base
// profiles remain immutable; every original facet witness is retained.
func (c *vffSKCRestrictionChecker) addBounds(p, base *vffSKCRestrictionProfile, nodes []*vffXMLNode) (bool, error) {
	p.bounds = make(map[string]string, len(base.bounds)+len(nodes))
	p.fixed = make(map[string]string, len(base.fixed)+len(nodes))
	for k, v := range base.fixed {
		if err := c.b.tick(0); err != nil {
			return false, err
		}
		p.fixed[k] = v
	}
	for k, v := range base.bounds {
		if err := c.b.tick(0); err != nil {
			return false, err
		}
		p.bounds[k] = v
	}
	// integer and all its builtin descendants inherit fractionDigits fixed at 0.
	if _, _, _, integer := vffSKCIntegerBounds(p.primitive.Local); integer {
		p.bounds["fractionDigits"] = "0"
		p.fixed["fractionDigits"] = "0"
	}
	local := make(map[string]string, len(nodes))
	for _, n := range nodes {
		kind := n.name.Local
		if _, ok := local[kind]; ok {
			return false, fmt.Errorf("duplicate %s facet", kind)
		}
		raw, ok := n.attrs["value"]
		if !ok {
			return false, fmt.Errorf("%s requires a value", kind)
		}
		length := strings.Contains(kind, "Length") || kind == "length"
		digits := kind == "totalDigits" || kind == "fractionDigits"
		typ := p.primitive
		if digits {
			if _, _, _, ok := vffSKCIntegerBounds(typ.Local); !ok && typ.Local != "decimal" {
				return false, fmt.Errorf("digits facet not applicable to %s", typ.Local)
			}
			typ = xml.Name{Space: vffXSDNS, Local: "nonNegativeInteger"}
			if kind == "totalDigits" {
				typ.Local = "positiveInteger"
			}
		} else if length {
			switch typ.Local {
			case "string", "normalizedString", "token":
			case "QName":
				return false, nil // Deprecated QName length semantics remain unproved.
			default:
				return false, fmt.Errorf("length facet not applicable to %s", typ.Local)
			}
			typ = xml.Name{Space: vffXSDNS, Local: "nonNegativeInteger"}
		} else if _, _, _, ok := vffSKCIntegerBounds(typ.Local); !ok && typ.Local != "decimal" {
			return false, fmt.Errorf("range facet not applicable to %s", typ.Local)
		}
		value, known, err := c.b.scalar(n, typ, raw)
		if err != nil {
			return false, err
		}
		if !known {
			return false, nil
		}
		if locked, ok := p.fixed[kind]; ok {
			cmp, err := c.b.compareDecimal(value.text, locked)
			if err != nil {
				return false, err
			}
			if cmp != 0 {
				return false, fmt.Errorf("derived facet changes fixed %s", kind)
			}
		}
		if rawFixed, present := n.attrs["fixed"]; present {
			flag, _, err := c.b.scalar(n, xml.Name{Space: vffXSDNS, Local: "boolean"}, rawFixed)
			if err != nil {
				return false, err
			}
			if flag.text == "true" {
				p.fixed[kind] = value.text
			}
			// false (or absence) never clears an inherited fixed component.
		}
		local[kind] = value.text
	}
	if err := c.checkMixedLength(base, local); err != nil {
		return false, err
	}
	for _, pair := range [][2]string{{"minInclusive", "minExclusive"}, {"maxInclusive", "maxExclusive"}} {
		_, a := local[pair[0]]
		_, b := local[pair[1]]
		if a && b {
			return false, fmt.Errorf("conflicting range facets")
		}
	}
	// Validate endpoints against the base value space, including inherited enums.
	// XSD 1.0 permits repeating the identical exclusive endpoint of the base.
	for _, n := range nodes {
		kind := n.name.Local
		v := local[kind]
		if kind == "totalDigits" || kind == "fractionDigits" {
			if old, ok := p.bounds[kind]; ok {
				cmp, err := c.b.compareDecimal(v, old)
				if err != nil {
					return false, err
				}
				if cmp > 0 {
					return false, fmt.Errorf("digits restriction widens base")
				}
			}
		} else if strings.Contains(kind, "Length") || kind == "length" {
			if old, ok := base.bounds[kind]; ok {
				cmp, err := c.b.compareDecimal(v, old)
				if err != nil {
					return false, err
				}
				if kind == "length" && cmp != 0 || kind == "minLength" && cmp < 0 || kind == "maxLength" && cmp > 0 {
					return false, fmt.Errorf("length restriction widens base")
				}
			}
		} else {
			sameExclusive := false
			if strings.HasSuffix(kind, "Exclusive") {
				if old, ok := base.bounds[kind]; ok {
					var err error
					sameExclusive, err = c.b.equalScalar(vffSKCScalar{text: v}, vffSKCScalar{text: old})
					if err != nil {
						return false, err
					}
				}
			}
			if !sameExclusive {
				_, known, err := c.evaluate(base, n, n.attrs["value"])
				if err != nil {
					return false, err
				}
				if !known {
					return false, nil
				}
			}
			if strings.HasPrefix(kind, "min") {
				delete(p.bounds, "minInclusive")
				delete(p.bounds, "minExclusive")
			} else {
				delete(p.bounds, "maxInclusive")
				delete(p.bounds, "maxExclusive")
			}
		}
		p.bounds[kind] = v
		p.facets = append(p.facets, c.b.witness(n))
	}
	if total, ok := p.bounds["totalDigits"]; ok {
		if fraction, ok := p.bounds["fractionDigits"]; ok {
			cmp, err := c.b.compareDecimal(fraction, total)
			if err != nil {
				return false, err
			}
			if cmp > 0 {
				return false, fmt.Errorf("fractionDigits exceeds totalDigits")
			}
		}
	}
	for _, lo := range []string{"minInclusive", "minExclusive"} {
		for _, hi := range []string{"maxInclusive", "maxExclusive"} {
			a, aok := p.bounds[lo]
			b, bok := p.bounds[hi]
			if !aok || !bok {
				continue
			}
			cmp, err := c.b.compareDecimal(a, b)
			if err != nil {
				return false, err
			}
			// Both exclusive endpoints may coincide (an empty value space), per 4.3.8.
			mixed := strings.HasSuffix(lo, "Exclusive") != strings.HasSuffix(hi, "Exclusive")
			if cmp > 0 || cmp == 0 && mixed {
				return false, fmt.Errorf("conflicting lower and upper bounds")
			}
		}
	}
	if lo, ok := p.bounds["minLength"]; ok {
		if hi, ok := p.bounds["maxLength"]; ok {
			cmp, err := c.b.compareDecimal(lo, hi)
			if err != nil {
				return false, err
			}
			if cmp > 0 {
				return false, fmt.Errorf("minLength exceeds maxLength")
			}
		}
	}
	return true, nil
}

// With length present, each coexisting min/max must equal a limit from an
// ancestor without length (XSD 1.0 4.3.1.4). A checked base can only narrow its
// bounds until length appears; from then on these limits cannot change. Thus
// its effective limit already proves that ancestor, without a second graph.
func (c *vffSKCRestrictionChecker) checkMixedLength(base *vffSKCRestrictionProfile, local map[string]string) error {
	length, hasLength := local["length"]
	if !hasLength {
		length, hasLength = base.bounds["length"]
	}
	if !hasLength {
		return nil
	}
	for _, kind := range []string{"minLength", "maxLength"} {
		limit, present := local[kind]
		if !present {
			limit, present = base.bounds[kind]
		}
		if !present {
			continue
		}
		ancestor, ok := base.bounds[kind]
		if !ok {
			return fmt.Errorf("%s with length requires an inherited limit", kind)
		}
		cmp, err := c.b.compareDecimal(limit, ancestor)
		if err != nil {
			return err
		}
		if cmp != 0 {
			return fmt.Errorf("%s with length changes its inherited limit", kind)
		}
		cmp, err = c.b.compareDecimal(limit, length)
		if err != nil {
			return err
		}
		if kind == "minLength" && cmp > 0 || kind == "maxLength" && cmp < 0 {
			return fmt.Errorf("length conflicts with inherited %s", kind)
		}
	}
	return nil
}

func (c *vffSKCRestrictionChecker) checkBounds(p *vffSKCRestrictionProfile, value vffSKCScalar) error {
	length := ""
	for _, kind := range []string{"minInclusive", "minExclusive", "maxInclusive", "maxExclusive", "length", "minLength", "maxLength", "totalDigits", "fractionDigits"} {
		bound, ok := p.bounds[kind]
		if !ok {
			continue
		}
		actual := value.text
		if kind == "totalDigits" || kind == "fractionDigits" {
			// Canonical decimal from scalar(): no sign/leading whole zeroes or
			// trailing fractional zeroes. Count the minimum coefficient AND scale,
			// per XSD 1.0 4.3.11: i*10^-n, |i| < 10^t and 0 <= n <= t.
			for range len(value.text) {
				if err := c.b.tick(0); err != nil {
					return err
				}
			}
			whole, fraction, _ := strings.Cut(strings.TrimPrefix(value.text, "-"), ".")
			count := len(fraction)
			if kind == "totalDigits" {
				coefficient := len(whole) + len(fraction)
				if whole == "0" {
					coefficient = len(strings.TrimLeft(fraction, "0"))
				}
				count = max(1, count, coefficient)
			}
			actual = strconv.Itoa(count)
		}
		isLength := strings.Contains(kind, "Length") || kind == "length"
		if isLength {
			if length == "" {
				count := 0
				for range value.text {
					if err := c.b.tick(0); err != nil {
						return err
					}
					count++
				}
				length = strconv.Itoa(count)
			}
			actual = length
		}
		cmp, err := c.b.compareDecimal(actual, bound)
		if err != nil {
			return err
		}
		invalid := false
		switch kind {
		case "minInclusive", "minLength":
			invalid = cmp < 0
		case "minExclusive":
			invalid = cmp <= 0
		case "maxInclusive", "maxLength", "totalDigits", "fractionDigits":
			invalid = cmp > 0
		case "maxExclusive":
			invalid = cmp >= 0
		case "length":
			invalid = cmp != 0
		}
		if invalid {
			return fmt.Errorf("attribute constraint violates %s facet", kind)
		}
	}
	return nil
}

// Inputs are canonical exact decimals from scalar(), never exponents or floats.
// Charge all comparison bytes even when an early digit would determine order.
func (b *vffSKCBinder) compareDecimal(a, d string) (int, error) {
	for _, s := range []string{a, d} {
		for range len(s) {
			if err := b.tick(0); err != nil {
				return 0, err
			}
		}
	}
	negA, negD := strings.HasPrefix(a, "-"), strings.HasPrefix(d, "-")
	if negA != negD {
		if negA {
			return -1, nil
		}
		return 1, nil
	}
	a = strings.TrimPrefix(a, "-")
	d = strings.TrimPrefix(d, "-")
	aw, af, _ := strings.Cut(a, ".")
	dw, df, _ := strings.Cut(d, ".")
	cmp := 0
	if len(aw) < len(dw) {
		cmp = -1
	} else if len(aw) > len(dw) {
		cmp = 1
	} else {
		cmp = strings.Compare(aw, dw)
	}
	if cmp == 0 {
		for i := 0; i < max(len(af), len(df)); i++ {
			x, y := byte('0'), byte('0')
			if i < len(af) {
				x = af[i]
			}
			if i < len(df) {
				y = df[i]
			}
			if x < y {
				cmp = -1
				break
			}
			if x > y {
				cmp = 1
				break
			}
		}
	}
	if negA {
		cmp = -cmp
	}
	return cmp, nil
}
