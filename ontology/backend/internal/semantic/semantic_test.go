package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Beijing is a synthetic fixture context only. No assertion is made about the
// actual location of any business record, and no live database is required.
func outdoor() Definition {
	return Definition{
		Scope:   Scope{TenantID: 1, OntologyID: "beijing_outdoor", Revision: 1},
		Classes: []Class{{ID: "activity", Name: "活动观察"}, {ID: "attendance", Name: "含成员状态的活动观察", Parents: []string{"activity"}}},
		Properties: []Property{
			{ID: "activity_status", ClassID: "activity", Key: "status", Name: "活动状态", Kind: String},
			{ID: "activity_date_present", ClassID: "activity", Key: "date_present", Name: "有活动日期", Kind: Boolean},
			{ID: "member_status", ClassID: "attendance", Key: "member_status", Name: "成员状态", Kind: String,
				Enum: []string{"报名中", "领队", "领队组", "替补中", "占坑中", "浏览中"}},
		},
		Rules: []Rule{
			{ID: "valid_activity", ClassID: "activity", Expression: `date_present && status != '拟定中' && status != '已取消'`, Basis: "Outdoor 统计有效活动口径（合成夹具）",
				Inputs: []Input{{"status", "activity_status", AbsenceUnknown}, {"date_present", "activity_date_present", AbsenceFalse}}},
			{ID: "registered", ClassID: "attendance", Expression: `status in ['报名中','领队','领队组','替补中','占坑中']`, Basis: "有效活动成员的报名口径（前置活动筛选由调用方负责）", Inputs: []Input{{"status", "member_status", AbsenceUnknown}}},
			{ID: "participated", ClassID: "attendance", Expression: `status in ['报名中','领队','领队组']`, Basis: "有效活动成员的参加口径（前置活动筛选由调用方负责）", Inputs: []Input{{"status", "member_status", AbsenceUnknown}}},
		},
	}
}

func mustFreeze(t *testing.T, d Definition) *Snapshot {
	t.Helper()
	s, err := Freeze(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOutdoorEvidenceStates(t *testing.T) {
	d := outdoor()
	s := mustFreeze(t, d)
	tests := []struct {
		name       string
		status     Fact
		date       Fact
		want       Outcome
		unresolved int
	}{
		{"observed", Fact{Known, "已发布"}, Fact{Known, true}, Matched, 0},
		{"explicitly_absent_date", Fact{Known, "已发布"}, Fact{Absent, nil}, NotMatched, 0},
		{"unread_date", Fact{Known, "已发布"}, Fact{Unknown, nil}, Undetermined, 1},
		{"canceled_unread_date", Fact{Known, "已取消"}, Fact{Unknown, nil}, NotMatched, 1},
		{"unread_status", Fact{Unknown, nil}, Fact{Known, true}, Undetermined, 1},
		{"absent_status", Fact{Absent, nil}, Fact{Known, true}, Undetermined, 1},
		{"invalid_status", Fact{Invalid, nil}, Fact{Known, false}, Failed, 0},
		{"wrong_date_type", Fact{Known, "已取消"}, Fact{Known, "false"}, Failed, 0},
		{"unknown_with_value", Fact{Unknown, "已取消"}, Fact{Known, false}, Failed, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := s.Evaluate(context.Background(), d.Scope, "valid_activity", map[string]Fact{"status": tc.status, "date_present": tc.date})
			if r.Outcome != tc.want {
				t.Fatalf("got %+v, want %s", r, tc.want)
			}
			if tc.want != Failed && len(r.Unresolved) != tc.unresolved {
				t.Fatalf("unexpected gaps: %+v", r)
			}
			if r.Mode != "hypothetical" || r.Digest != s.Digest() || r.Basis == "" {
				t.Fatalf("missing explanation: %+v", r)
			}
		})
	}
	if r := s.Evaluate(context.Background(), d.Scope, "valid_activity", nil); r.Outcome != Undetermined || len(r.Unresolved) != 2 {
		t.Fatalf("omitted inputs: %+v", r)
	}
	for _, name := range []string{"registered", "participated"} {
		r := s.Evaluate(context.Background(), d.Scope, name, map[string]Fact{"status": {Known, "替补中"}})
		want := NotMatched
		if name == "registered" {
			want = Matched
		}
		if r.Outcome != want {
			t.Fatalf("%s: %+v", name, r)
		}
		if r := s.Evaluate(context.Background(), d.Scope, name, nil); r.Outcome != Undetermined {
			t.Fatal(r)
		}
	}
}

func TestFactAndIdentityGuards(t *testing.T) {
	d := outdoor()
	s := mustFreeze(t, d)
	for _, scope := range []Scope{{2, d.Scope.OntologyID, 1}, {1, "other", 1}, {1, d.Scope.OntologyID, 2}} {
		r := s.Evaluate(context.Background(), scope, "registered", nil)
		if r.Code != "scope_mismatch" || r.Digest != "" || r.Basis != "" {
			t.Fatalf("scope leak: %+v", r)
		}
	}
	for _, fact := range []Fact{{Known, 1}, {Known, nil}, {Known, "新状态"}, {Known, strings.Repeat("a", maxText+1)}, {Known, string([]byte{0xff})}, {"typo", nil}, {Absent, false}} {
		r := s.Evaluate(context.Background(), d.Scope, "registered", map[string]Fact{"status": fact})
		if r.Outcome != Failed {
			t.Fatalf("accepted invalid fact: %+v", r)
		}
	}
	if r := s.Evaluate(context.Background(), d.Scope, "registered", map[string]Fact{"typo": {Known, "报名中"}}); r.Code != "unexpected_input" {
		t.Fatal(r)
	}
	if r := s.Evaluate(context.Background(), d.Scope, "missing", nil); r.Code != "unknown_rule" {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := s.Evaluate(ctx, d.Scope, "registered", nil); r.Code != "canceled" || r.Outcome != Failed {
		t.Fatal(r)
	}
	tooMany := map[string]Fact{}
	for i := 0; i <= maxInputs; i++ {
		tooMany[fmt.Sprint(i)] = Fact{Unknown, nil}
	}
	if r := s.Evaluate(context.Background(), d.Scope, "registered", tooMany); r.Code != "input_limit" {
		t.Fatal(r)
	}
}

func TestCanonicalSnapshotIsolation(t *testing.T) {
	d := outdoor()
	before, _ := json.Marshal(d)
	s := mustFreeze(t, d)
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("Freeze changed caller input")
	}
	reordered := outdoor()
	slices.Reverse(reordered.Classes)
	slices.Reverse(reordered.Properties)
	slices.Reverse(reordered.Properties[0].Enum)
	slices.Reverse(reordered.Rules)
	for i := range reordered.Rules {
		slices.Reverse(reordered.Rules[i].Inputs)
	}
	reordered.Relations = []Relation{}
	if s.Digest() != mustFreeze(t, reordered).Digest() {
		t.Fatal("member order changed digest")
	}
	d.Rules[0].Inputs[0].PropertyID = "corrupted"
	d.Properties[2].Enum[0] = "corrupted"
	d.Classes[1].Parents[0] = "corrupted"
	bytes := s.CanonicalJSON()
	bytes[0] = '!'
	ancestors, err := s.Ancestors("attendance")
	if err != nil || !slices.Equal(ancestors, []string{"activity"}) {
		t.Fatalf("ancestors: %v %v", ancestors, err)
	}
	ancestors[0] = "corrupted"
	if a, _ := s.Ancestors("attendance"); a[0] != "activity" {
		t.Fatal("mutable ancestry")
	}
	if _, err := s.Ancestors("missing"); err == nil {
		t.Fatal("unknown class accepted")
	}
	if s.Digest() != mustFreeze(t, outdoor()).Digest() || !json.Valid(s.CanonicalJSON()) {
		t.Fatal("mutable snapshot")
	}
	r := s.Evaluate(context.Background(), outdoor().Scope, "registered", map[string]Fact{"status": {Known, "报名中"}})
	r.Inputs[0].PropertyID = "corrupted"
	if r := s.Evaluate(context.Background(), outdoor().Scope, "registered", map[string]Fact{"status": {Known, "报名中"}}); r.Outcome != Matched || r.Inputs[0].PropertyID != "member_status" {
		t.Fatal(r)
	}
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Scope.TenantID++ },
		func(d *Definition) { d.Scope.Revision++ },
		func(d *Definition) { d.Rules[0].Basis += " changed" },
		func(d *Definition) { d.Rules[0].Expression = "!(" + d.Rules[0].Expression + ")" },
	} {
		d := outdoor()
		mutate(&d)
		if s.Digest() == mustFreeze(t, d).Digest() {
			t.Fatal("semantic change did not change digest")
		}
	}
}

func TestRejectInvalidDefinitions(t *testing.T) {
	tests := map[string]func(*Definition){
		"zero_scope":             func(d *Definition) { d.Scope.Revision = 0 },
		"invalid_id":             func(d *Definition) { d.Classes[0].ID = "a b" },
		"no_classes":             func(d *Definition) { d.Classes = nil },
		"duplicate_member":       func(d *Definition) { d.Properties[0].ID = "activity" },
		"cycle":                  func(d *Definition) { d.Classes[0].Parents = []string{"attendance"} },
		"self_cycle":             func(d *Definition) { d.Classes[0].Parents = []string{"activity"} },
		"missing_parent":         func(d *Definition) { d.Classes[1].Parents = []string{"missing"} },
		"duplicate_parent":       func(d *Definition) { d.Classes[1].Parents = []string{"activity", "activity"} },
		"property_shadow":        func(d *Definition) { d.Properties[2].Key = "status" },
		"missing_property_class": func(d *Definition) { d.Properties[0].ClassID = "missing" },
		"unsupported_type":       func(d *Definition) { d.Properties[0].Kind = "dyn" },
		"enum_on_bool":           func(d *Definition) { d.Properties[1].Enum = []string{"false"} },
		"duplicate_enum":         func(d *Definition) { d.Properties[2].Enum = []string{"报名中", "报名中"} },
		"unknown_binding":        func(d *Definition) { d.Rules[0].Inputs[0].PropertyID = "missing" },
		"wrong_binding_class":    func(d *Definition) { d.Rules[0].Inputs[0].PropertyID = "member_status" },
		"duplicate_variable":     func(d *Definition) { d.Rules[0].Inputs[1].Variable = "status" },
		"duplicate_binding":      func(d *Definition) { d.Rules[0].Inputs[1].PropertyID = "activity_status" },
		"unknown_absence":        func(d *Definition) { d.Rules[0].Inputs[0].OnAbsent = "" },
		"false_on_string":        func(d *Definition) { d.Rules[0].Inputs[0].OnAbsent = AbsenceFalse },
		"unknown_rule_class":     func(d *Definition) { d.Rules[0].ClassID = "missing" },
		"no_basis":               func(d *Definition) { d.Rules[0].Basis = "" },
		"bad_unicode":            func(d *Definition) { d.Classes[0].Name = string([]byte{0xff}) },
		"expression_size":        func(d *Definition) { d.Rules[0].Expression = strings.Repeat("a", maxText+1) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			d := outdoor()
			mutate(&d)
			if _, err := Freeze(d); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}

func TestInheritanceAndRelations(t *testing.T) {
	d := Definition{Scope: outdoor().Scope, Classes: []Class{{ID: "root", Name: "根"}, {ID: "left", Name: "左", Parents: []string{"root"}}, {ID: "right", Name: "右", Parents: []string{"root"}}, {ID: "leaf", Name: "叶", Parents: []string{"left", "right"}}},
		Properties: []Property{{ID: "flag", ClassID: "root", Key: "flag", Name: "标识", Kind: Boolean}},
		Relations:  []Relation{{ID: "within", Name: "位于", From: "root", To: "root", InverseID: "contains", Transitive: true}, {ID: "contains", Name: "包含", From: "root", To: "root", InverseID: "within", Transitive: true}}}
	s := mustFreeze(t, d)
	if a, _ := s.Ancestors("leaf"); !slices.Equal(a, []string{"left", "right", "root"}) {
		t.Fatal(a)
	}
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Relations[0].From = "missing" },
		func(d *Definition) { d.Relations[0].To = "leaf" },
		func(d *Definition) { d.Relations[0].InverseID = "missing" },
		func(d *Definition) { d.Relations[1].InverseID = "" },
		func(d *Definition) { d.Relations[1].Transitive = false },
	} {
		copy := d
		copy.Relations = slices.Clone(d.Relations)
		mutate(&copy)
		if _, err := Freeze(copy); err == nil {
			t.Fatal("bad relation accepted")
		}
	}
	d.Classes = nil
	d.Properties = nil
	d.Relations = nil
	for i := 0; i < 34; i++ {
		c := Class{ID: fmt.Sprintf("c%d", i), Name: "类型"}
		if i > 0 {
			c.Parents = []string{fmt.Sprintf("c%d", i-1)}
		}
		d.Classes = append(d.Classes, c)
	}
	if _, err := Freeze(d); err == nil {
		t.Fatal("deep memoized ancestry accepted")
	}
	slices.Reverse(d.Classes)
	if _, err := Freeze(d); err == nil {
		t.Fatal("deep recursive ancestry accepted")
	}
}

func TestRestrictedLanguage(t *testing.T) {
	for _, expression := range []string{
		`size(status) > 1`, `status.matches('.*')`, `dyn(status) == true`, `status == string(1)`,
		`timestamp('2026-01-01T00:00:00Z') == timestamp('2026-01-01T00:00:00Z')`,
		`[1,2].exists(x, x > 1)`, `status == ('a' + 'b')`, `status == {'x':'a'}['x']`,
		`status == ['a'][0]`, `status == null`, `true ? status == 'a' : false`,
		`status in [true]`, `status in ['a','a']`, `status in []`, `status in [status]`,
		`status == missing`, `status`, `true`, `1 == 1`, `status ==`,
		`status == '报名钟'`, `'报名钟' != status`, `status in ['报名中','报名钟']`,
	} {
		t.Run(expression, func(t *testing.T) {
			d := outdoor()
			d.Rules = d.Rules[1:2]
			d.Rules[0].Expression = expression
			if _, err := Freeze(d); err == nil {
				t.Fatal("unsupported expression accepted")
			}
		})
	}
	for _, expression := range []string{`status == '报名中'`, `!(status != '报名中')`, `status in ['报名中','替补中']`, `status == '报名中' || status == '领队'`} {
		d := outdoor()
		d.Rules = d.Rules[1:2]
		d.Rules[0].Expression = expression
		s := mustFreeze(t, d)
		if r := s.Evaluate(context.Background(), d.Scope, "registered", map[string]Fact{"status": {Known, "报名中"}}); r.Outcome != Matched {
			t.Fatal(r)
		}
	}
}

func TestRuleCostLimit(t *testing.T) {
	d := outdoor()
	d.Properties[2].Enum = nil
	d.Rules = d.Rules[1:2]
	d.Rules[0].Expression = strings.TrimSuffix(strings.Repeat("status == status && ", 10), " && ")
	s := mustFreeze(t, d)
	r := s.Evaluate(context.Background(), d.Scope, "registered", map[string]Fact{"status": {Known, strings.Repeat("a", maxText)}})
	if r.Outcome != Failed || r.Code != "cost_limit" {
		t.Fatalf("cost limit not enforced: %+v", r)
	}
}

func TestStructuralBudgets(t *testing.T) {
	for name, mutate := range map[string]func(*Definition){
		"classes":    func(d *Definition) { d.Classes = make([]Class, 65) },
		"properties": func(d *Definition) { d.Properties = make([]Property, 257) },
		"relations":  func(d *Definition) { d.Relations = make([]Relation, 129) },
		"rules":      func(d *Definition) { d.Rules = make([]Rule, 65) },
		"inputs":     func(d *Definition) { d.Rules[0].Inputs = make([]Input, maxInputs+1) },
		"parents":    func(d *Definition) { d.Classes[0].Parents = make([]string, 9) },
		"enum":       func(d *Definition) { d.Properties[2].Enum = make([]string, 65) },
		"ast": func(d *Definition) {
			d.Rules = d.Rules[1:2]
			d.Rules[0].Expression = strings.TrimSuffix(strings.Repeat("status == status && ", 80), " && ")
		},
		"parser_depth": func(d *Definition) { d.Rules[0].Expression = strings.Repeat("!", 40) + "date_present" },
	} {
		t.Run(name, func(t *testing.T) {
			d := outdoor()
			mutate(&d)
			if _, err := Freeze(d); err == nil {
				t.Fatal("budget not enforced")
			}
		})
	}
}

func TestDisjunctionAndRawValueRedaction(t *testing.T) {
	d := outdoor()
	d.Rules = d.Rules[:1]
	d.Rules[0].Expression = `date_present || status == '已发布'`
	s := mustFreeze(t, d)
	r := s.Evaluate(context.Background(), d.Scope, "valid_activity", map[string]Fact{"date_present": {Known, true}})
	if r.Outcome != Matched || !slices.Equal(r.Unresolved, []string{"status"}) {
		t.Fatal(r)
	}
	secret := "synthetic-sensitive-business-value"
	r = s.Evaluate(context.Background(), d.Scope, "valid_activity", map[string]Fact{"status": {Known, secret}, "date_present": {Known, true}})
	encoded, _ := json.Marshal(r)
	if r.Outcome != Matched || strings.Contains(string(encoded), secret) {
		t.Fatalf("raw value leaked: %s", encoded)
	}
}

func TestConcurrentEvaluation(t *testing.T) {
	d := outdoor()
	s := mustFreeze(t, d)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				if r := s.Evaluate(context.Background(), d.Scope, "registered", map[string]Fact{"status": {Known, "报名中"}}); r.Outcome != Matched {
					t.Error(r)
				}
			}
		}()
	}
	wg.Wait()
}

func TestCompilerIdentityMatchesDependency(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), strings.ReplaceAll(CompilerVersion, "@", " ")) {
		t.Fatal("compiler identity drifted from locked dependency")
	}
}

func ExampleSnapshot_Evaluate() {
	d := outdoor()
	s, _ := Freeze(d)
	for _, date := range []Fact{{Known, true}, {Absent, nil}, {Unknown, nil}} {
		r := s.Evaluate(context.Background(), d.Scope, "valid_activity", map[string]Fact{"status": {Known, "已发布"}, "date_present": date})
		fmt.Println(date.State, r.Outcome, r.Mode)
	}
	// Output:
	// known matched hypothetical
	// absent not_matched hypothetical
	// unknown unknown hypothetical
}
