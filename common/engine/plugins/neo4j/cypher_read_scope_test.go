package neo4j

import (
	"errors"
	"reflect"
	"testing"
)

func TestLocalReadCypherGrammar(t *testing.T) {
	for _, query := range []string{
		"MATCH (n) RETURN n LIMIT 10",
		"MATCH (a)-[r]->(b) WHERE NOT type(r) IN ['RTREE_ROOT'] RETURN a,r,b LIMIT 10",
		"OPTIONAL MATCH (n:`CREATE`) WHERE n.name = 'DELETE x' RETURN n",
		"UNWIND [1,2] AS n WITH n WHERE n > 0 RETURN n UNION RETURN 3 AS n",
		"MATCH (n) RETURN n // CREATE (bad)\n",
		"RETURN {text: 'CALL remote()', value: 1}",
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := parseLocalReadCypher(query); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, query := range []string{
		"CREATE (n) RETURN n", "MATCH (n) SET n.x=1 RETURN n", "MATCH (n) DETACH DELETE n",
		"MERGE (n) RETURN n", "MATCH (n) REMOVE n.x RETURN n", "CALL db.labels()",
		"MATCH (n) CALL db.labels() YIELD label RETURN label", "CALL { MATCH (n) RETURN n } RETURN n",
		"LOAD CSV FROM 'https://example.invalid/data.csv' AS row RETURN row", "USE remote MATCH (n) RETURN n",
		"RETURN 1; RETURN 2", "RETURN 1 @", "RETURN '\u0000' @", "MATCH (n RETURN n", "",
		"RETURN `point.distance`(1,2)", "SHOW DATABASES", "CYPHER runtime=slotted MATCH (n) RETURN n",
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := parseLocalReadCypher(query); !errors.Is(err, errLocalReadCypher) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestCypherFunctionsComeFromAST(t *testing.T) {
	proof, err := parseLocalReadCypher("MATCH (n) RETURN type(n), custom.fetch(n), `apoc`.`load``er`(n), count(*)")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(proof.functions, []string{"apoc.load`er", "custom.fetch", "type"}) {
		t.Fatalf("functions=%v", proof.functions)
	}
}

func TestCypherProofUsesStatementTokenSpan(t *testing.T) {
	for _, q := range []string{" // heading\nMATCH (n) RETURN n; // tail", "MATCH (n) RETURN n // tail"} {
		proof, err := parseLocalReadCypher(q)
		if err != nil {
			t.Fatal(err)
		}
		if proof.statement != "MATCH (n) RETURN n" {
			t.Fatalf("statement=%q", proof.statement)
		}
	}
}

func TestPreparedCypherFreezesTypedParameterCollections(t *testing.T) {
	values := []string{"A"}
	nested := map[string][]int64{"items": {1}}
	copy := cloneCypherValue(map[string]interface{}{"values": values, "nested": nested}).(map[string]interface{})
	values[0] = "changed"
	nested["items"][0] = 2
	if copy["values"].([]string)[0] != "A" || copy["nested"].(map[string][]int64)["items"][0] != 1 {
		t.Fatal("typed parameters retained caller aliases")
	}
}
