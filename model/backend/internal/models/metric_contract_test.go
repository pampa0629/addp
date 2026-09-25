package models

import (
	"encoding/json"
	"testing"
)

func TestMetricContractJSONKeepsPublishedOperationsStable(t *testing.T) {
	old := MetricContract{
		Operation: "count_distinct", Subject: MetricFieldReference{FieldID: 1},
		SubjectRelationID: 10, Distinct: MetricFieldReference{FieldID: 2},
		Time: MetricFieldReference{FieldID: 6, RelationID: 11},
	}
	encoded, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"operation":"count_distinct","subject":{"field_id":1,"relation_id":0},"subject_relation_id":10,"distinct":{"field_id":2,"relation_id":0},"time":{"field_id":6,"relation_id":11},"filters":null}`
	if string(encoded) != want {
		t.Fatalf("published contract JSON changed: %s", encoded)
	}
	group := MetricFieldReference{FieldID: 21}
	measure := MetricFieldReference{FieldID: 22}
	fresh := MetricContract{Operation: "sum_decimal_by_group", Group: &group, Measure: &measure}
	encoded, err = json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"operation":"sum_decimal_by_group","group":{"field_id":21,"relation_id":0},"measure":{"field_id":22,"relation_id":0}}`
	if string(encoded) != want {
		t.Fatalf("new contract contains unrelated fields: %s", encoded)
	}
	var decoded MetricContract
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Group == nil || decoded.Measure == nil || decoded.Group.FieldID != 21 || decoded.Measure.FieldID != 22 {
		t.Fatalf("new contract round trip: %+v %v", decoded, err)
	}
}
