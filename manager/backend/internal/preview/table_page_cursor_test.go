package preview

import (
	"errors"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/common/opaquetoken"
)

func TestTablePageTokenBindsScopeAndRequiresReadableUniqueKey(t *testing.T) {
	tenant := uint(3)
	req := PreviewRequest{Engine: &models.Engine{ID: 7, Version: 2}, ItemFingerprint: "sample", TenantID: &tenant}
	field := datatype.FieldInfo{Name: "uid", Type: datatype.FieldTypeBigInt, NativeType: "bigint"}
	facts := &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{field}, Native: map[string]interface{}{"relkind": "r", "relhassubclass": false}}, Constraints: []plugin.ConstraintFacts{{ConstraintType: plugin.ConstraintTypeUnique, Fields: []string{"uid"}}}}
	reader := TablePageRequest{Codec: opaquetoken.New([]byte("fixture table pages"), "manager.table-preview.v1"), Subject: dataprotection.SubjectReference{Type: "user", ID: "42"}}
	page, err := reader.resolve(&req, facts, 1, 20)
	if err != nil || page == nil {
		t.Fatal(page, err)
	}
	const largest = int64(9223372036854775807)
	reader.Cursor, err = page.next(map[string]interface{}{"uid": largest})
	if err != nil {
		t.Fatal(err)
	}
	token := reader.Cursor
	check := func(ok bool) {
		t.Helper()
		page, err := reader.resolve(&req, facts, 2, 20)
		if ok && (err != nil || page.after[0] != "9223372036854775807") || !ok && !errors.Is(err, ErrTablePageCursor) {
			t.Fatalf("accepted=%t page=%v err=%v", ok, page, err)
		}
	}
	check(true)
	reader.Cursor = "invalid"
	check(false)
	reader.Cursor = token
	tenant++
	check(false)
	tenant--
	req.Engine.Version++
	check(false)
	req.Engine.Version--
	req.ItemFingerprint = "different"
	check(false)
	req.ItemFingerprint = "sample"
	facts.Table.Native["relhassubclass"] = true
	check(false)
	facts.Table.Native["relhassubclass"] = false
	facts.Table.Fields[0].Nullable = true
	check(false)
	facts.Table.Fields[0].Nullable = false
	reader.Cursor = ""
	if _, err := reader.resolve(&req, facts, 10000, 20); !errors.Is(err, ErrTablePageCursor) {
		t.Fatalf("arbitrary jump accepted: %v", err)
	}
	reader.Codec = nil
	if _, err := reader.resolve(&req, facts, 1, 20); !errors.Is(err, ErrTablePageKeyUnavailable) {
		t.Fatalf("missing deployment key: %v", err)
	}
}
