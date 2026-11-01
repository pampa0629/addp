package federatedquery

import (
	_ "github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/models"
	"strings"
	"testing"
)

func TestRelationalCandidatesUseOwnerNamespaceNodesRatherThanFlatNames(t *testing.T) {
	tree := &models.MetadataTree{TopNodes: []models.MetaNode{{ID: 7, NodeType: "schema", Name: " 原.schema "}},
		Items: []models.MetaItem{{NodeID: 7, ItemType: "table", Name: " 订单.table ", FullName: "ambiguous.display.text"}}}
	tables := relationalTables("Business PostgreSQL", "postgresql", tree)
	if len(tables) != 1 || tables[0].Schema != " 原.schema " || tables[0].Table != " 订单.table " {
		t.Fatalf("owner facts were changed: %#v", tables)
	}
	tree.Items[0].NodeID = 999
	if len(relationalTables("Business PostgreSQL", "postgresql", tree)) != 0 {
		t.Fatal("missing namespace was guessed from full_name")
	}
}

func TestCandidatesUseCurrentSourceAndTableNames(t *testing.T) {
	candidates, err := Candidates([]Source{
		{EngineID: 7, EngineName: "Business PostgreSQL", Tables: []TableRef{{Schema: "public", Table: "farmland"}}},
		{EngineID: 8, EngineName: "Business MinIO", Tables: []TableRef{{Table: "orders-2026", PhysicalPath: "manager/orders-2026"}}},
	}, 10, `"`)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("Candidates() count = %d, want 2", len(candidates))
	}
	if candidates[0].EngineID != 7 || candidates[0].Query != "SELECT *\nFROM \"Business_PostgreSQL\".\"public\".\"farmland\"\nLIMIT 10" {
		t.Fatalf("unexpected relational candidate: %#v", candidates[0])
	}
	if candidates[1].EngineID != 8 || candidates[1].Query != "SELECT *\nFROM \"Business_MinIO\".\"orders_2026\"\nLIMIT 10" {
		t.Fatalf("unexpected object candidate: %#v", candidates[1])
	}
}

func TestCandidatesCanReturnUnboundedBaseQueries(t *testing.T) {
	candidates, err := Candidates([]Source{{
		EngineID: 7, EngineName: "Business PostgreSQL",
		Tables: []TableRef{{Schema: "public", Table: "farmland"}},
	}}, 0, "`")
	if err != nil || len(candidates) != 1 || candidates[0].Query != "SELECT *\nFROM `Business_PostgreSQL`.`public`.`farmland`" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}

func TestCandidatesPreservePhysicalNamesAndEscapeNativeQuote(t *testing.T) {
	for _, quote := range []string{"`", `"`} {
		name := " 原名 " + quote + "订单表 "
		candidates, err := Candidates([]Source{{EngineID: 7, EngineName: "Business PostgreSQL", Tables: []TableRef{{Schema: " 业务库 ", Table: name}}}}, 10, quote)
		if err != nil || len(candidates) != 1 {
			t.Fatalf("candidate error: %v", err)
		}
		want := quote + " 业务库 " + quote + "." + quote + " 原名 " + quote + quote + "订单表 " + quote
		if !strings.Contains(candidates[0].Query, want) {
			t.Fatalf("physical name changed: %q", candidates[0].Query)
		}
	}
	if _, err := Candidates(nil, 10, ""); err == nil {
		t.Fatal("missing grammar quote accepted")
	}
}
