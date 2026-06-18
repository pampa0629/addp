package data

import "testing"

func TestTableRefFromLocator(t *testing.T) {
	ref, err := tableRefFromLocator("addp://engine/9/path/public/sales?type=table&item_id=33")
	if err != nil {
		t.Fatalf("tableRefFromLocator() error = %v", err)
	}
	if ref.EngineID != 9 || ref.SchemaName != "public" || ref.TableName != "sales" || ref.ItemID != 33 {
		t.Fatalf("table ref = %+v", ref)
	}
}

func TestTableRefFromLocatorRejectsNonTableLocator(t *testing.T) {
	_, err := tableRefFromLocator("addp://engine/9/path/public?type=schema&node_id=33")
	if err == nil {
		t.Fatal("tableRefFromLocator() error = nil, want invalid locator error")
	}
	if !IsInvalidResourceLocatorError(err) {
		t.Fatalf("IsInvalidResourceLocatorError(%v) = false", err)
	}
}

func TestTableRefFromLocatorRequiresItemID(t *testing.T) {
	_, err := tableRefFromLocator("addp://engine/9/path/public/sales?type=table")
	if err == nil {
		t.Fatal("tableRefFromLocator() error = nil, want invalid locator error")
	}
	if !IsInvalidResourceLocatorError(err) {
		t.Fatalf("IsInvalidResourceLocatorError(%v) = false", err)
	}
}
