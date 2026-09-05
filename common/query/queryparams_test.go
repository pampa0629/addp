package query

import (
	"reflect"
	"testing"
)

func TestBindSQLUsesDriverPlaceholdersAndIgnoresQuotedText(t *testing.T) {
	query := `SELECT ':ignored', payload::jsonb FROM members WHERE status = :status AND nickname = :nickname OR alias = :nickname -- :comment`
	parameters := map[string]interface{}{"status": "领队", "nickname": "PiPi"}

	bound, args, err := BindSQL(query, parameters, SQLPlaceholderDollar)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT ':ignored', payload::jsonb FROM members WHERE status = $1 AND nickname = $2 OR alias = $3 -- :comment`
	if bound != want {
		t.Fatalf("bound SQL = %q, want %q", bound, want)
	}
	if !reflect.DeepEqual(args, []interface{}{"领队", "PiPi", "PiPi"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestSQLPlaceholderStyleForEngine(t *testing.T) {
	tests := []struct {
		engineType string
		want       SQLPlaceholderStyle
	}{
		{engineType: "postgresql", want: SQLPlaceholderDollar},
		{engineType: "PostGIS", want: SQLPlaceholderDollar},
		{engineType: "oracle", want: SQLPlaceholderColon},
		{engineType: "mysql", want: SQLPlaceholderQuestion},
	}
	for _, tt := range tests {
		if got := SQLPlaceholderStyleForDialect(tt.engineType); got != tt.want {
			t.Fatalf("SQLPlaceholderStyleForDialect(%q) = %q, want %q", tt.engineType, got, tt.want)
		}
	}
}

func TestBindSQLUsesOracleColonPlaceholders(t *testing.T) {
	bound, args, err := BindSQL(
		`SELECT * FROM members WHERE status = :status AND score > :score`,
		map[string]interface{}{"status": "active", "score": 10},
		SQLPlaceholderColon,
	)
	if err != nil {
		t.Fatal(err)
	}
	if bound != `SELECT * FROM members WHERE status = :1 AND score > :2` {
		t.Fatalf("bound SQL = %q", bound)
	}
	if !reflect.DeepEqual(args, []interface{}{"active", 10}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestBindMQLReplacesStructuralParameterNodes(t *testing.T) {
	query := `{"find":"Outdoors","filter":{"Members":{"$elemMatch":{"entryInfo.status":{"$param":"status"},"userInfo.nickName":{"$param":"nickname"}}}}}`
	bound, err := BindMQL(query, map[string]interface{}{"status": "领队", "nickname": "PiPi"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"filter":{"Members":{"$elemMatch":{"entryInfo.status":"领队","userInfo.nickName":"PiPi"}}},"find":"Outdoors"}`
	if bound != want {
		t.Fatalf("bound MQL = %s, want %s", bound, want)
	}
}

func TestReferencesRejectsMissingAndUnusedParameters(t *testing.T) {
	if err := ValidateCypher(`MATCH (m) WHERE m.name = $name RETURN m`, map[string]interface{}{}); err == nil {
		t.Fatal("expected missing Cypher parameter to fail")
	}
	if _, _, err := BindSQL(`SELECT * FROM t WHERE id = :id`, map[string]interface{}{"id": 1, "unused": true}, SQLPlaceholderQuestion); err == nil {
		t.Fatal("expected unused SQL parameter to fail")
	}
}

func TestReferencesIgnoreCypherStringsAndComments(t *testing.T) {
	names, err := CypherReferences(`MATCH (n) WHERE n.text = '$ignored' AND n.name = $name // $comment`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"name"}) {
		t.Fatalf("names = %#v", names)
	}
}

func TestBindMQLRejectsTrailingJSONValue(t *testing.T) {
	_, err := BindMQL(`{"find":"members"} {"find":"other"}`, nil)
	if err == nil {
		t.Fatal("expected trailing MQL JSON value to fail")
	}
}
