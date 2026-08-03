package federatedquery

import "testing"

func TestCandidatesUseCurrentSourceAndTableNames(t *testing.T) {
	candidates := Candidates([]Source{
		{EngineID: 7, EngineName: "Business PostgreSQL", Tables: []TableRef{{Schema: "public", Table: "farmland"}}},
		{EngineID: 8, EngineName: "Business MinIO", Tables: []TableRef{{Table: "orders-2026"}}},
	}, 10)
	if len(candidates) != 2 {
		t.Fatalf("Candidates() count = %d, want 2", len(candidates))
	}
	if candidates[0].EngineID != 7 || candidates[0].Query != "SELECT *\nFROM Business_PostgreSQL.public.farmland\nLIMIT 10" {
		t.Fatalf("unexpected relational candidate: %#v", candidates[0])
	}
	if candidates[1].EngineID != 8 || candidates[1].Query != "SELECT *\nFROM Business_MinIO.orders_2026\nLIMIT 10" {
		t.Fatalf("unexpected object candidate: %#v", candidates[1])
	}
}

func TestCandidatesCanReturnUnboundedBaseQueries(t *testing.T) {
	candidates := Candidates([]Source{{
		EngineID: 7, EngineName: "Business PostgreSQL",
		Tables: []TableRef{{Schema: "public", Table: "farmland"}},
	}}, 0)
	if len(candidates) != 1 || candidates[0].Query != "SELECT *\nFROM Business_PostgreSQL.public.farmland" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}
