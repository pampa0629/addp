package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestVFFSKCFilterRuleDeclarationsAndSourceBytes(t *testing.T) {
	raw := []byte("// comments may mention https://not-a-reference/a.png\r\n  Beam\r\n { Name Description }\r\nColumn\nEmpty {}\n// global blacklist\n{ href source }\n")
	budget := &vffSKCFilterBudget{limits: skcDocumentLimits().filters}
	got, err := readVFFSKCFilter(context.Background(), "Schemas/schema.xsd.filter", raw, budget)
	if err != nil || len(got.rules) != 4 || got.member != "Schemas/schema.xsd.filter" || got.sha256 != sha256.Sum256(raw) {
		t.Fatalf("filter declarations: %#v %v", got, err)
	}
	if got.rules[0].typeName != "Beam" || !got.rules[0].attributesSpecified || strings.Join(got.rules[0].attributes, ",") != "Name,Description" || got.rules[1].typeName != "Column" || got.rules[1].attributesSpecified || got.rules[2].typeName != "Empty" || !got.rules[2].attributesSpecified || len(got.rules[2].attributes) != 0 || got.rules[3].typeName != "" || strings.Join(got.rules[3].attributes, ",") != "href,source" {
		t.Fatal("whitelist, absent/empty attributes or global blacklist confused")
	}
	for _, rule := range got.rules {
		if rule.sha256 != sha256.Sum256(raw[rule.begin:rule.end]) {
			t.Fatal("exact rule source identity lost")
		}
	}
	if budget.bytes != len(raw) || budget.rules != 4 || budget.attributes != 4 || budget.tokens != 13 {
		t.Fatalf("actual cumulative use: %#v", budget)
	}
	for _, text := range []string{"", "// comment only\n", " \t"} {
		got, err := readVFFSKCFilter(context.Background(), "empty.filter", []byte(text), &vffSKCFilterBudget{limits: skcDocumentLimits().filters})
		if err != nil || len(got.rules) != 0 {
			t.Fatalf("explicit zero rules: %#v %v", got, err)
		}
	}
	// A whitelisted attribute can also appear in the global blacklist. The
	// official rules distinguish these scopes; do not invent a conflict.
	if _, err := readVFFSKCFilter(context.Background(), "scope.filter", []byte("Beam { Name }\n{ Name }"), &vffSKCFilterBudget{limits: skcDocumentLimits().filters}); err != nil {
		t.Fatal(err)
	}
}

func TestVFFSKCFilterRejectsUnknownOrAmbiguousSyntax(t *testing.T) {
	for _, text := range []string{"Beam { Name", "Beam { Name } }", "{ { Name } }", "Beam Beam", "Beam {} Beam { Name }", "{ Name } { Other }", "Beam { Name Name }", "Beam { ../image.png }", "Beam { https://invalid/a }", "Beam;", "Beam,Column", "Beam // inline comments not in this profile", "Beam { 'Name' }", "Beam { ns:Name }", "类型 { 属性 }", "\ufeffBeam", "Beam\x00", "Beam\vName", "\xff"} {
		got, err := readVFFSKCFilter(context.Background(), "bad.filter", []byte(text), &vffSKCFilterBudget{limits: skcDocumentLimits().filters})
		if err == nil || got != nil {
			t.Fatalf("unknown/ambiguous filter accepted: %q", text)
		}
	}
}

func TestVFFSKCFilterCumulativeBudgetsAndCancellation(t *testing.T) {
	raw := []byte("Beam { Name }\n{ Other }")
	full := vffSKCFilterLimits{bytes: len(raw), tokens: 7, rules: 2, attributes: 2}
	if _, err := readVFFSKCFilter(context.Background(), "control.filter", raw, &vffSKCFilterBudget{limits: full}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*vffSKCFilterLimits){"bytes": func(l *vffSKCFilterLimits) { l.bytes-- }, "tokens": func(l *vffSKCFilterLimits) { l.tokens-- }, "rules": func(l *vffSKCFilterLimits) { l.rules-- }, "attributes": func(l *vffSKCFilterLimits) { l.attributes-- }} {
		t.Run(name, func(t *testing.T) {
			l := full
			mutate(&l)
			if got, err := readVFFSKCFilter(context.Background(), "bad.filter", raw, &vffSKCFilterBudget{limits: l}); err == nil || got != nil {
				t.Fatal("filter boundary bypass")
			}
		})
	}
	budget := &vffSKCFilterBudget{limits: full}
	if _, err := readVFFSKCFilter(context.Background(), "one.filter", raw, budget); err != nil {
		t.Fatal(err)
	}
	if got, err := readVFFSKCFilter(context.Background(), "equal-but-distinct.filter", raw, budget); err == nil || got != nil {
		t.Fatal("same bytes at distinct filter identity not charged")
	}
	for _, budget := range []*vffSKCFilterBudget{nil, {}, {limits: vffSKCFilterLimits{bytes: 1}}} {
		if got, err := readVFFSKCFilter(context.Background(), "bad.filter", raw, budget); err == nil || got != nil {
			t.Fatal("unspecified filter budgets accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 4}
	long := append([]byte("//"), bytes.Repeat([]byte("x"), 10000)...)
	if got, err := readVFFSKCFilter(controlled, "long.filter", long, &vffSKCFilterBudget{limits: skcDocumentLimits().filters}); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-filter cancellation: %#v %v", got, err)
	}
}

func TestVFFClassificationFiltersIntegratedBudgetsAndCorruptCurrentMember(t *testing.T) {
	files := skcDocumentFiles(t)
	limits := skcDocumentLimits()
	for name, mutate := range map[string]func(*vffSKCFilterLimits){"bytes": func(l *vffSKCFilterLimits) { l.bytes = 1 }, "tokens": func(l *vffSKCFilterLimits) { l.tokens = 1 }, "rules unspecified": func(l *vffSKCFilterLimits) { l.rules = 0 }, "attributes unspecified": func(l *vffSKCFilterLimits) { l.attributes = 0 }} {
		t.Run(name, func(t *testing.T) {
			l := limits
			mutate(&l.filters)
			if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), l); err == nil || got != nil {
				t.Fatal("integrated filter budget bypass")
			}
		})
	}
	if _, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), limits); err != nil {
		t.Fatal(err)
	}
	files["Schemas/root.xsd.filter"] = []byte("Beam { source ../../other.png }")
	if got, err := readVFFClassificationDocuments(context.Background(), skcDocumentContainer(t, files), limits); err == nil || got != nil {
		t.Fatal("invalid current filter ignored or old filter facts reused")
	}
	files = skcDocumentFiles(t)
	raw := skcDocumentContainer(t, files).members["classifications/literal%20schema.skc"]
	c := vffClassificationControl(func(p map[string][]byte, c *vffContainer) {
		c.members["classifications/literal%20schema.skc"] = raw
		c.members["classifications/second.skc"] = bytes.Clone(raw)
		p["entries"] = append(vffRecord("8467", p["directory"], p["name"], p["path"]), vffRecord("8467", p["directory"], p["name"], vffRecord("8767", []byte("classifications/second.skc")))...)
	})
	if got, err := readVFFClassificationDocuments(context.Background(), c, limits); err != nil || len(got.documents) != 2 {
		t.Fatalf("two distinct package control: %v", err)
	}
	for name, mutate := range map[string]func(*vffSKCFilterLimits){
		"bytes across packages":      func(l *vffSKCFilterLimits) { l.bytes = 2*len(files["Schemas/root.xsd.filter"]) - 1 },
		"tokens across packages":     func(l *vffSKCFilterLimits) { l.tokens = 7 },
		"rules across packages":      func(l *vffSKCFilterLimits) { l.rules = 1 },
		"attributes across packages": func(l *vffSKCFilterLimits) { l.attributes = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := limits
			mutate(&copy.filters)
			if got, err := readVFFClassificationDocuments(context.Background(), c, copy); err == nil || got != nil {
				t.Fatal("second package filter limit ignored or partial result exposed")
			}
		})
	}
}
