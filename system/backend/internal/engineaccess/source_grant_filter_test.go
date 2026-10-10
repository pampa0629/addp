package engineaccess

import (
	"reflect"
	"strings"
	"testing"
)

func TestSourceGrantFilterValidation(t *testing.T) {
	for _, filter := range []SourceGrantFilter{{}, {TableSearch: "表_%", RecipientType: "user", RecipientID: 9007199254740993}, {RecipientType: "department"}, {RecipientType: "project_group", RecipientID: 33}, {TableSearch: strings.Repeat("表", 200)}} {
		if err := filter.Validate(); err != nil {
			t.Fatalf("valid filter rejected: %+v %v", filter, err)
		}
	}
	for _, filter := range []SourceGrantFilter{{RecipientType: "user", RecipientID: -1}, {RecipientID: 33}, {RecipientType: "unknown"}, {RecipientType: " user"}, {TableSearch: " a"}, {TableSearch: "a\n"}, {TableSearch: "a\x00b"}, {TableSearch: "\xff"}, {TableSearch: strings.Repeat("表", 201)}} {
		if err := filter.Validate(); err == nil {
			t.Fatalf("invalid filter accepted: %+v", filter)
		}
	}
}

func TestSourceGrantFilterBindsRecipientTypeAndID(t *testing.T) {
	for _, kind := range []string{"user", "department", "project_group"} {
		predicate, args := (SourceGrantFilter{TableSearch: "表_%", RecipientType: kind, RecipientID: 33}).predicate()
		if !strings.Contains(predicate, "recipient_type = ? AND recipient_id = ?") || !reflect.DeepEqual(args, []any{"表_%", kind, int64(33)}) {
			t.Fatalf("recipient identity lost: %s %+v", predicate, args)
		}
	}
	predicate, args := (SourceGrantFilter{RecipientType: "department"}).predicate()
	if strings.Contains(predicate, "recipient_id") || !reflect.DeepEqual(args, []any{"department"}) {
		t.Fatalf("type-only filter: %s %+v", predicate, args)
	}
}
