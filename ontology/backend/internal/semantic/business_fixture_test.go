package semantic

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func businessFixture(t *testing.T) (*Snapshot, Scope) {
	t.Helper()
	data, err := os.ReadFile("testdata/outdoor_business.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition Definition
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	definition.Scope = Scope{TenantID: 42, OntologyID: "outdoor_business", Revision: 1}
	return mustFreeze(t, definition), definition.Scope
}

func TestOutdoorBusinessDefinitionSeparatesMembershipFromActivity(t *testing.T) {
	snapshot, _ := businessFixture(t)
	classes := snapshot.Classes()
	if len(classes) != 3 {
		t.Fatalf("classes = %v", classes)
	}
	for _, class := range classes {
		if len(class.Parents) != 0 {
			t.Fatalf("business types must not inherit from each other: %+v", class)
		}
	}
	for _, tc := range []struct {
		class      string
		properties []string
		rules      []string
	}{
		{"activity", []string{"activity_date_present", "activity_status"}, []string{"valid_activity"}},
		{"person", []string{}, []string{}},
		{"activity_membership", []string{"member_status"}, []string{"participated", "registered"}},
	} {
		ctx, err := snapshot.ClassContext(tc.class)
		if err != nil {
			t.Fatal(err)
		}
		properties, rules := []string{}, []string{}
		for _, property := range ctx.Properties {
			properties = append(properties, property.ID)
		}
		for _, rule := range ctx.Rules {
			rules = append(rules, rule.ID)
		}
		if len(ctx.Ancestors) != 0 || !slices.Equal(properties, tc.properties) || !slices.Equal(rules, tc.rules) {
			t.Fatalf("unexpected context for %s: %+v", tc.class, ctx)
		}
	}
	membership, err := snapshot.ClassContext("activity_membership")
	if err != nil {
		t.Fatal(err)
	}
	if len(membership.Relations) != 2 {
		t.Fatalf("membership endpoints = %+v", membership.Relations)
	}
	for i, target := range []string{"activity", "person"} {
		relation := membership.Relations[i]
		if relation.From != "activity_membership" || relation.To != target || relation.Transitive || relation.InverseID != "" {
			t.Fatalf("unexpected relation semantics: %+v", relation)
		}
	}
}

func TestOutdoorBusinessMembershipStateMatrix(t *testing.T) {
	snapshot, scope := businessFixture(t)
	for _, tc := range []struct {
		state                    string
		registered, participated Outcome
	}{
		{"报名中", Matched, Matched},
		{"领队", Matched, Matched},
		{"领队组", Matched, Matched},
		{"替补中", Matched, NotMatched},
		{"占坑中", Matched, NotMatched},
		{"浏览中", NotMatched, NotMatched},
	} {
		t.Run(tc.state, func(t *testing.T) {
			for rule, want := range map[string]Outcome{"registered": tc.registered, "participated": tc.participated} {
				result := snapshot.Evaluate(context.Background(), scope, rule, map[string]Fact{"status": {Known, tc.state}})
				if result.Outcome != want || result.Mode != "hypothetical" || result.Digest != snapshot.Digest() || result.Basis == "" {
					t.Fatalf("%s: %+v, want %s", rule, result, want)
				}
			}
		})
	}
	for _, rule := range []string{"registered", "participated"} {
		for _, facts := range []map[string]Fact{nil, {"status": {Unknown, nil}}, {"status": {Absent, nil}}} {
			result := snapshot.Evaluate(context.Background(), scope, rule, facts)
			if result.Outcome != Undetermined || !slices.Equal(result.Unresolved, []string{"status"}) {
				t.Fatalf("%s converted missing evidence to a business result: %+v", rule, result)
			}
		}
		for _, fact := range []Fact{{Known, "新状态"}, {Known, true}, {Invalid, nil}, {Unknown, "报名中"}} {
			result := snapshot.Evaluate(context.Background(), scope, rule, map[string]Fact{"status": fact})
			if result.Outcome != Failed {
				t.Fatalf("%s accepted invalid evidence: %+v", rule, result)
			}
		}
	}
}

func TestOutdoorBusinessValidActivityEvidence(t *testing.T) {
	snapshot, scope := businessFixture(t)
	for _, tc := range []struct {
		name   string
		status Fact
		date   Fact
		want   Outcome
	}{
		{"published", Fact{Known, "已发布"}, Fact{Known, true}, Matched},
		{"historic_registration_closed", Fact{Known, "报名截止"}, Fact{Known, true}, Matched},
		{"formed", Fact{Known, "已成行"}, Fact{Known, true}, Matched},
		{"ended", Fact{Known, "已结束"}, Fact{Known, true}, Matched},
		{"other_stored_status", Fact{Known, "其他存储状态"}, Fact{Known, true}, Matched},
		{"draft", Fact{Known, "拟定中"}, Fact{Known, true}, NotMatched},
		{"canceled", Fact{Known, "已取消"}, Fact{Known, true}, NotMatched},
		{"observed_no_date", Fact{Known, "已发布"}, Fact{Known, false}, NotMatched},
		{"confirmed_absence", Fact{Known, "已发布"}, Fact{Absent, nil}, NotMatched},
		{"unread_date", Fact{Known, "已发布"}, Fact{Unknown, nil}, Undetermined},
		{"unread_status", Fact{Unknown, nil}, Fact{Known, true}, Undetermined},
		{"missing_status", Fact{Absent, nil}, Fact{Known, true}, Undetermined},
		{"canceled_unread_date", Fact{Known, "已取消"}, Fact{Unknown, nil}, NotMatched},
		{"malformed_date", Fact{Known, "已发布"}, Fact{Known, "true"}, Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := snapshot.Evaluate(context.Background(), scope, "valid_activity", map[string]Fact{"status": tc.status, "date_present": tc.date})
			if result.Outcome != tc.want || result.Mode != "hypothetical" {
				t.Fatalf("got %+v, want %s", result, tc.want)
			}
		})
	}
	// There is no runtime join or cross-class rule input in this definition-only slice.
	result := snapshot.Evaluate(context.Background(), scope, "registered", map[string]Fact{
		"status": {Known, "报名中"}, "date_present": {Known, true},
	})
	if result.Code != "unexpected_input" {
		t.Fatalf("membership rule silently accepted an activity fact: %+v", result)
	}
}
