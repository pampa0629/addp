package skp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

func vffSKCBoundFacet(name string) bool {
	switch name {
	case "minInclusive", "minExclusive", "maxInclusive", "maxExclusive", "length", "minLength", "maxLength":
		return true
	}
	return false
}

// Bounds are projected from the existing resolved type graph. Cached base
// profiles remain immutable; every original facet witness is retained.
func (c *vffSKCRestrictionChecker) addBounds(p, base *vffSKCRestrictionProfile, nodes []*vffXMLNode) (bool, error) {
	p.bounds = make(map[string]string, len(base.bounds)+len(nodes))
	for k, v := range base.bounds {
		if err := c.b.tick(0); err != nil {
			return false, err
		}
		p.bounds[k] = v
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
		typ := p.primitive
		if length {
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
		local[kind] = value.text
	}
	// Do not claim the more intricate inherited length/min/max combination rules.
	_, hasLength := p.bounds["length"]
	_, newLength := local["length"]
	if hasLength || newLength {
		for _, k := range []string{"minLength", "maxLength"} {
			_, a := p.bounds[k]
			_, b := local[k]
			if a || b {
				return false, nil
			}
		}
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
		if strings.Contains(kind, "Length") || kind == "length" {
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

func (c *vffSKCRestrictionChecker) checkBounds(p *vffSKCRestrictionProfile, value vffSKCScalar) error {
	length := ""
	for _, kind := range []string{"minInclusive", "minExclusive", "maxInclusive", "maxExclusive", "length", "minLength", "maxLength"} {
		bound, ok := p.bounds[kind]
		if !ok {
			continue
		}
		actual := value.text
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
		case "maxInclusive", "maxLength":
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
