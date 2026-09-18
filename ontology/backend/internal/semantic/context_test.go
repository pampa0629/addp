package semantic

import "testing"

func TestClassContextDefinitionsAndImmutableCopies(t *testing.T) {
	d := outdoor()
	d.Relations = []Relation{{ID: "observes", Name: "观察", From: "attendance", To: "activity"}}
	s := mustFreeze(t, d)
	c, err := s.ClassContext("attendance")
	if err != nil || c.Class.ID != "attendance" || len(c.Ancestors) != 1 || len(c.Properties) != 3 || len(c.Rules) != 2 || len(c.Relations) != 1 {
		t.Fatalf("context=%+v error=%v", c, err)
	}
	for _, rule := range c.Rules {
		if rule.ClassID != "attendance" {
			t.Fatal("ancestor rule silently inherited")
		}
	}
	c.Class.Parents[0] = "changed"
	c.Properties[0].Name = "changed"
	c.Rules[0].Inputs[0].PropertyID = "changed"
	c.Relations[0].From = "changed"
	classes := s.Classes()
	classes[1].Parents[0] = "changed"
	again, err := s.ClassContext("attendance")
	if err != nil || again.Class.Parents[0] != "activity" || again.Properties[0].Name == "changed" || again.Rules[0].Inputs[0].PropertyID == "changed" || again.Relations[0].From != "attendance" {
		t.Fatalf("mutated immutable snapshot: %+v %v", again, err)
	}
	if _, err := s.ClassContext("missing"); err == nil {
		t.Fatal("unknown class accepted")
	}
}
