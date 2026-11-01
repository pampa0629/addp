package federatedquery

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestSourceDiscoveryPreservesQuotedReferencesAndIgnoresStringsCommentsAndAmbiguity(t *testing.T) {
	candidates := []plugin.FederatedQuerySource{
		{ID: 11, Name: "Business PostgreSQL", EngineType: "postgresql", LifecycleState: "active"},
		{ID: 12, Name: "Business MySQL", EngineType: "mysql", LifecycleState: "active"},
		{ID: 13, Name: "Private", EngineType: "postgresql", LifecycleState: "disabled"},
	}
	query := "SELECT 'Private.schema.table', `订单.字段` FROM `Business_PostgreSQL`.`public`.`订单 表` p JOIN \"Business_MySQL\".db.customers c ON p.id=c.id -- ignored.db.table\n/* Private.db.table /* nested.db.table */ */"
	if ids := ReferencedSourceEngineIDs(query, candidates, []string{"postgresql", "mysql"}); !reflect.DeepEqual(ids, []uint{11, 12}) {
		t.Fatalf("sources = %v", ids)
	}
	for _, query := range []string{"SELECT 'Business_PostgreSQL.public.orders'", "SELECT 1 /* Business_PostgreSQL.public.orders */", "SELECT 1 -- Business_MySQL.db.orders", "SELECT 'unterminated"} {
		if ids := ReferencedSourceEngineIDs(query, candidates, []string{"postgresql", "mysql"}); len(ids) != 0 {
			t.Fatalf("false source binding: %q -> %v", query, ids)
		}
	}
	candidates = append(candidates, plugin.FederatedQuerySource{ID: 14, Name: "Business-PostgreSQL", EngineType: "postgresql", LifecycleState: "active"})
	if ids := ReferencedSourceEngineIDs(query, candidates, []string{"postgresql", "mysql"}); len(ids) != 0 {
		t.Fatalf("ambiguous alias admitted: %v", ids)
	}
}
