package query

import "testing"

func TestSQLFailureCoordinatesUseUTF16AndProveSourceMapping(t *testing.T) {
	source := "  \nSELECT '😀中文', missing_field"
	bound := "SELECT '😀中文', missing_field"
	offset := len("SELECT '😀中文', ")
	context := NewErrorContext(bound, "column", "missing_field", offset).RemapSource(source, bound)
	if context.Position.Line != 2 || context.Position.Column != 16 || context.SourceDigest != SourceDigest(source) {
		t.Fatalf("context=%+v position=%+v", context, context.Position)
	}
	changed := context.RemapSource("SELECT :value, missing_field", bound)
	if changed.Position != nil || changed.Object == nil {
		t.Fatalf("rewrite has an unproven position: %+v", changed)
	}
	if PositionAtByte(source, 12) != nil {
		t.Fatal("accepted a byte inside a Unicode code point")
	}
}

func TestSQLFailureContextRejectsTextAndUnboundedCoordinates(t *testing.T) {
	for _, context := range []*ErrorContext{
		{Object: &ErrorObject{Kind: "message", Name: "private"}},
		{Object: &ErrorObject{Kind: "table", Name: "name\nprivate"}},
		{Object: &ErrorObject{Kind: "table", Name: "name\u202eprivate"}},
		{Position: &SourcePosition{Line: 0, Column: 1}},
		{Position: &SourcePosition{Line: 1, Column: 1000001}},
	} {
		if context.Valid() {
			t.Fatalf("accepted invalid context: %+v", context)
		}
	}
}
