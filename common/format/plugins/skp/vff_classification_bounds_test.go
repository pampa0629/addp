package skp

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestVFFSKCRangeAndLengthConstraints(t *testing.T) {
	cases := []struct {
		name, primitive, base, derived, value string
		reject                                bool
	}{
		{"decimal exact", "decimal", `<x:minInclusive value="9007199254740993.0000000000000001"/>`, `<x:maxInclusive value="9007199254740993.0000000000000002"/>`, "9007199254740993.0000000000000001", false},
		{"decimal below", "decimal", `<x:minInclusive value="9007199254740993.0000000000000001"/>`, "", "9007199254740993", true},
		{"negative decimal", "decimal", `<x:minExclusive value="-1.01"/><x:maxInclusive value="-1.001"/>`, "", "-1.005", false},
		{"exclusive repeated", "int", `<x:minExclusive value="1"/>`, `<x:minExclusive value="01"/>`, "2", false},
		{"exclusive endpoint", "int", `<x:minExclusive value="1"/>`, "", "1", true},
		{"range widen", "int", `<x:maxInclusive value="10"/>`, `<x:maxInclusive value="11"/>`, "5", true},
		{"open to closed widen", "int", `<x:minExclusive value="1"/>`, `<x:minInclusive value="1"/>`, "2", true},
		{"conflicting same side", "int", "", `<x:minInclusive value="0"/><x:minExclusive value="1"/>`, "2", true},
		{"duplicate", "int", "", `<x:minInclusive value="0"/><x:minInclusive value="0"/>`, "2", true},
		{"reversed", "int", "", `<x:minInclusive value="10"/><x:maxInclusive value="1"/>`, "5", true},
		{"closed singleton", "int", "", `<x:minInclusive value="1"/><x:maxInclusive value="1"/>`, "1", false},
		{"empty mixed", "int", "", `<x:minExclusive value="1"/><x:maxInclusive value="1"/>`, "2", true},
		{"builtin endpoint", "byte", "", `<x:maxInclusive value="128"/>`, "1", true},
		{"enum and range", "int", `<x:enumeration value="1"/><x:enumeration value="2"/>`, `<x:maxInclusive value="1"/>`, "1", false},
		{"enum excluded", "int", `<x:enumeration value="1"/><x:enumeration value="2"/>`, `<x:maxInclusive value="1"/>`, "2", true},
		{"unicode codepoints", "string", "", `<x:length value="2"/>`, "中😀", false},
		{"combining not grapheme", "string", "", `<x:length value="1"/>`, "é", true},
		{"token normalized first", "token", `<x:minLength value="1"/>`, `<x:maxLength value="3"/>`, "  a   b  ", false},
		{"empty valid", "string", "", `<x:length value="0"/>`, "", false},
		{"length widen", "string", `<x:maxLength value="3"/>`, `<x:maxLength value="4"/>`, "ab", true},
		{"min length widen", "string", `<x:minLength value="2"/>`, `<x:minLength value="1"/>`, "ab", true},
		{"length change", "string", `<x:length value="2"/>`, `<x:length value="3"/>`, "abc", true},
		{"length too short", "string", `<x:minLength value="2"/>`, "", "a", true},
		{"huge min length", "string", `<x:minLength value="184467440737095516160000"/>`, "", "a", true},
		{"huge max length", "string", `<x:maxLength value="184467440737095516160000"/>`, "", "a", false},
		{"negative length", "string", "", `<x:length value="-1"/>`, "a", true},
		{"numeric length inapplicable", "int", "", `<x:length value="1"/>`, "1", true},
		{"string range inapplicable", "string", "", `<x:minInclusive value="a"/>`, "b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := skcEnumFiles(t, tc.derived, tc.base, tc.value, tc.value)
			f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `base="x:int"`, `base="x:`+tc.primitive+`"`, 1))
			got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
			if tc.reject {
				if err == nil || got != nil {
					t.Fatal("invalid facet/value accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			v := skcAttributeValue(t, f)
			if !v.fixedValues[0].known || len(v.fixedValues[0].facets) == 0 {
				t.Fatal("bounded value not proved or source lost")
			}
			for _, fact := range v.constraintValues {
				if !fact.known {
					t.Fatal("single/fallback constraint bypassed facets")
				}
			}
		})
	}
}

func TestVFFSKCBoundsUnknownNeverFallBack(t *testing.T) {
	for _, body := range []string{`<x:minInclusive value="1" fixed="true" opaque="unproved"/>`, `<x:pattern value=".*"/><x:maxInclusive value="3"/>`} {
		v := skcAttributeValue(t, skcEnumFiles(t, body, "", "2", "2"))
		if v.fixedValues[0].known {
			t.Fatal("unproved facet ignored")
		}
	}
	f := skcEnumFiles(t, `<x:length value="2"/><x:minLength value="1"/>`, "", "ab", "ab")
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `base="x:int"`, `base="x:string"`, 1))
	if v := skcAttributeValue(t, f); v.fixedValues[0].known {
		t.Fatal("mixed length semantics claimed proved")
	}
}

func TestVFFSKCBoundsPreserveExactWitnessAndRejectWithoutDefaults(t *testing.T) {
	f := skcEnumFiles(t, `<x:maxInclusive value="3"/>`, `<x:minInclusive value="1"/>`, "2", "2")
	v := skcAttributeValue(t, f)
	for _, w := range v.fixedValues[0].facets {
		if w.member != "Schemas/extra.xsd" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
			t.Fatal("bound witness lost exact source")
		}
	}
	f["Schemas/extra.xsd"] = []byte(strings.ReplaceAll(string(f["Schemas/extra.xsd"]), ` fixed="2"`, ""))
	f["Schemas/shared.xsd"] = []byte(strings.ReplaceAll(string(f["Schemas/shared.xsd"]), ` fixed="2"`, ""))
	f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `value="3"`, `value="0"`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("invalid range escaped without a constraint value")
	}
}

func TestVFFSKCExactDecimalComparisonAndBudget(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"-1.005", "-1.001", -1}, {"0.01", "0.001", 1}, {"1.01", "1.01", 0}, {"-100", "-99", -1}, {"0", "-0.001", 1}, {"100000000000000000000.01", "100000000000000000000.001", 1},
	} {
		b := &vffSKCBinder{ctx: context.Background(), budget: &vffSKCBindingBudget{remaining: 1000, depth: 8}}
		got, err := b.compareDecimal(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("%s versus %s: %d %v", tc.a, tc.b, got, err)
		}
	}
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		b := &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
		_, err := b.compareDecimal("1.00", "2.00")
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && err == nil {
			t.Fatal("comparison bypassed budget/cancel")
		}
	}
}

func TestVFFSKCDigitConstraints(t *testing.T) {
	cases := []struct {
		name, primitive, base, derived, value string
		reject                                bool
	}{
		{"canonical zeros", "decimal", "", `<x:totalDigits value="3"/><x:fractionDigits value="2"/>`, "+0001.23000", false},
		{"integer trailing zeros count", "decimal", "", `<x:totalDigits value="2"/>`, "100", true},
		{"integer boundary", "integer", "", `<x:totalDigits value="3"/>`, "-100", false},
		{"zero", "decimal", "", `<x:totalDigits value="1"/><x:fractionDigits value="0"/>`, "-000.000", false},
		{"scale fits", "decimal", "", `<x:totalDigits value="3"/>`, "0.00100", false},
		{"scale exceeds total", "decimal", "", `<x:totalDigits value="2"/>`, "0.001", true},
		{"coefficient exceeds total", "decimal", "", `<x:totalDigits value="3"/>`, "1.234", true},
		{"fraction exceeds", "decimal", "", `<x:fractionDigits value="2"/>`, "-0.001", true},
		{"fraction no rounding", "decimal", "", `<x:fractionDigits value="16"/>`, "9007199254740993.0000000000000001", false},
		{"precision exceeds", "decimal", "", `<x:totalDigits value="31"/>`, "9007199254740993.0000000000000001", true},
		{"huge declaration", "decimal", "", `<x:totalDigits value="184467440737095516160000"/><x:fractionDigits value="184467440737095516160000"/>`, "0.00001", false},
		{"narrow inherited", "decimal", `<x:totalDigits value="4"/><x:fractionDigits value="3"/>`, `<x:totalDigits value="3"/><x:fractionDigits value="2"/>`, "1.23", false},
		{"total widen", "decimal", `<x:totalDigits value="3"/>`, `<x:totalDigits value="4"/>`, "1", true},
		{"fraction widen", "decimal", `<x:fractionDigits value="2"/>`, `<x:fractionDigits value="3"/>`, "1", true},
		{"inherited total conflict", "decimal", `<x:totalDigits value="2"/>`, `<x:fractionDigits value="3"/>`, "1", true},
		{"inherited fraction conflict", "decimal", `<x:fractionDigits value="3"/>`, `<x:totalDigits value="2"/>`, "1", true},
		{"integer fraction fixed", "int", "", `<x:fractionDigits value="1"/>`, "1", true},
		{"integer fraction zero", "unsignedLong", "", `<x:fractionDigits value="0"/><x:totalDigits value="20"/>`, "18446744073709551615", false},
		{"enum and digits", "decimal", `<x:enumeration value="1.23"/>`, `<x:totalDigits value="3"/>`, "1.2300", false},
		{"enum violates inherited digits", "decimal", `<x:totalDigits value="2"/>`, `<x:enumeration value="1.23"/>`, "1", true},
		{"range violates inherited digits", "decimal", `<x:totalDigits value="2"/>`, `<x:maxInclusive value="123"/>`, "1", true},
		{"bounds retained", "decimal", `<x:minInclusive value="1"/><x:maxExclusive value="2"/>`, `<x:totalDigits value="3"/>`, "1.23", false},
		{"bounds not bypassed", "decimal", `<x:maxInclusive value="1"/>`, `<x:totalDigits value="3"/>`, "1.23", true},
		{"missing value", "decimal", "", `<x:totalDigits/>`, "1", true},
		{"zero total", "decimal", "", `<x:totalDigits value="0"/>`, "0", true},
		{"negative fraction", "decimal", "", `<x:fractionDigits value="-1"/>`, "1", true},
		{"nonintegral total", "decimal", "", `<x:totalDigits value="2.0"/>`, "1", true},
		{"exponent value", "decimal", "", `<x:totalDigits value="3"/>`, "1e0", true},
		{"duplicate total", "decimal", "", `<x:totalDigits value="2"/><x:totalDigits value="2"/>`, "1", true},
		{"duplicate fraction", "decimal", "", `<x:fractionDigits value="2"/><x:fractionDigits value="2"/>`, "1", true},
		{"string inapplicable", "string", "", `<x:totalDigits value="3"/>`, "abc", true},
		{"boolean inapplicable", "boolean", "", `<x:fractionDigits value="0"/>`, "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := skcEnumFiles(t, tc.derived, tc.base, tc.value, tc.value)
			f["Schemas/extra.xsd"] = []byte(strings.Replace(string(f["Schemas/extra.xsd"]), `base="x:int"`, `base="x:`+tc.primitive+`"`, 1))
			got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits())
			if tc.reject {
				if err == nil || got != nil {
					t.Fatal("invalid digit declaration/value accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			v := skcAttributeValue(t, f)
			if !v.fixedValues[0].known || len(v.fixedValues[0].facets) == 0 {
				t.Fatal("digits not proved or witness lost")
			}
			for _, w := range v.fixedValues[0].facets {
				if w.member != "Schemas/extra.xsd" || w.sha256 != sha256.Sum256(f[w.member][w.begin:w.end]) {
					t.Fatal("digits lost exact declaration witness")
				}
			}
			for _, fact := range v.constraintValues {
				if !fact.known {
					t.Fatal("single constraint failed to follow digits profile")
				}
			}
		})
	}
}

func TestVFFSKCDigitsNoConstraintsAndUnknownFacets(t *testing.T) {
	for _, facets := range []string{`<x:totalDigits value="0"/>`, `<x:fractionDigits value="-1"/>`, `<x:totalDigits value="2"/><x:fractionDigits value="3"/>`} {
		f := skcEnumFiles(t, facets, "", "1", "1")
		for _, name := range []string{"Schemas/extra.xsd", "Schemas/shared.xsd"} {
			f[name] = []byte(strings.ReplaceAll(string(f[name]), ` fixed="1"`, ""))
		}
		if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
			t.Fatal("invalid digits escaped without default/fixed")
		}
	}
	for _, facets := range []string{`<x:totalDigits value="2" fixed="false" opaque="unproved"/>`, `<x:totalDigits value="2"/><x:pattern value=".*"/>`, `<x:totalDigits value="2"/><x:whiteSpace value="collapse"/>`} {
		v := skcAttributeValue(t, skcEnumFiles(t, facets, "", "1", "1"))
		if v.fixedValues[0].known {
			t.Fatal("unproved facet dropped by digits profile")
		}
		for _, fact := range v.constraintValues {
			if fact.known {
				t.Fatal("single constraint bypassed unproved facet")
			}
		}
	}
}

func TestVFFSKCDigitsSingleDefaultAndBudget(t *testing.T) {
	f := skcSimpleContentFiles(t, `<x:attribute name="A" default="1.2300"><x:simpleType><x:restriction base="x:decimal"><x:totalDigits value="3"/><x:fractionDigits value="2"/></x:restriction></x:simpleType></x:attribute>`, "")
	v := skcAttributeValue(t, f)
	if len(v.fixedValues) != 0 || len(v.constraintValues) != 2 {
		t.Fatal("single default did not retain declaration/use facts")
	}
	for _, fact := range v.constraintValues {
		if !fact.known || fact.value.text != "1.23" || len(fact.facets) != 2 {
			t.Fatal("single default did not consume digits")
		}
	}
	f["Schemas/shared.xsd"] = []byte(strings.Replace(string(f["Schemas/shared.xsd"]), `default="1.2300"`, `default="1.234"`, 1))
	if got, err := readVFFClassificationBindings(context.Background(), skcGraphContainer(t, f), skcBindingLimits()); err == nil || got != nil {
		t.Fatal("single default bypassed digit constraints")
	}
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			stop()
		}
		b := &vffSKCBinder{ctx: ctx, budget: &vffSKCBindingBudget{remaining: 1, depth: 8}}
		c := &vffSKCRestrictionChecker{b: b}
		err := c.checkBounds(&vffSKCRestrictionProfile{bounds: map[string]string{"totalDigits": "1000"}}, vffSKCScalar{text: strings.Repeat("1", 100)})
		stop()
		if cancel && !errors.Is(err, context.Canceled) || !cancel && err == nil {
			t.Fatal("digit counting bypassed budget/cancel")
		}
	}
}
