package datatype

import "testing"

func TestGraphInfoCloneDeepCopiesShapes(t *testing.T) {
	t.Parallel()

	nodeCount := int64(12)
	relationshipCount := int64(7)
	directed := true
	nodeShapeCount := int64(10)
	patternCount := int64(5)
	info := &GraphInfo{
		Model:             GraphModelPropertyGraph,
		Directed:          &directed,
		NodeCount:         &nodeCount,
		RelationshipCount: &relationshipCount,
		NodeShapes: []GraphNodeShapeInfo{{
			Name:       "Person",
			Kind:       GraphNodeShapeKindLabel,
			Labels:     []string{"Person"},
			Properties: []FieldInfo{{Name: "name", Type: FieldTypeString}},
			Count:      &nodeShapeCount,
		}},
		RelationshipShapes: []GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []GraphRelationshipPatternInfo{{
				From:  GraphEndpointInfo{ShapeName: "Person", Labels: []string{"Person"}},
				To:    GraphEndpointInfo{ShapeName: "Company", Labels: []string{"Company"}},
				Count: &patternCount,
			}},
			Count: &relationshipCount,
		}},
	}

	cloned := info.Clone()
	cloned.NodeShapes[0].Labels[0] = "Changed"
	cloned.NodeShapes[0].Properties[0].Name = "changed"
	cloned.RelationshipShapes[0].Patterns[0].From.Labels[0] = "Changed"
	*cloned.NodeShapes[0].Count = 99

	if info.NodeShapes[0].Labels[0] != "Person" || info.NodeShapes[0].Properties[0].Name != "name" {
		t.Fatalf("node shape was mutated: %#v", info.NodeShapes[0])
	}
	if info.RelationshipShapes[0].Patterns[0].From.Labels[0] != "Person" {
		t.Fatalf("relationship pattern was mutated: %#v", info.RelationshipShapes[0].Patterns[0])
	}
	if *info.NodeShapes[0].Count != 10 {
		t.Fatalf("node shape count was mutated: %#v", info.NodeShapes[0].Count)
	}
}

func TestGraphInfoPayloadUsesStandardShapeModel(t *testing.T) {
	t.Parallel()

	nodeCount := int64(12)
	relationshipCount := int64(7)
	directed := true
	info := &GraphInfo{
		Model:             " " + GraphModelPropertyGraph + " ",
		Directed:          &directed,
		NodeCount:         &nodeCount,
		RelationshipCount: &relationshipCount,
		NodeShapes: []GraphNodeShapeInfo{{
			Kind:       " " + GraphNodeShapeKindLabelSet + " ",
			Labels:     []string{" Person ", "Employee", "Person"},
			Properties: []FieldInfo{{Name: "name", Type: FieldTypeString}},
			Count:      &nodeCount,
		}},
		RelationshipShapes: []GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []GraphRelationshipPatternInfo{{
				From:  GraphEndpointInfo{Labels: []string{"Person", "Employee"}},
				To:    GraphEndpointInfo{ShapeName: "Company", Labels: []string{"Company"}},
				Count: &relationshipCount,
			}},
			Count: &relationshipCount,
		}},
	}

	payload := GraphInfoPayload(info)
	if payload["edge_count"] != nil || payload["node_labels"] != nil || payload["relationship_types"] != nil {
		t.Fatalf("legacy graph payload should not be written: %#v", payload)
	}
	if payload["model"] != GraphModelPropertyGraph || payload["directed"] != true || payload["relationship_count"] != relationshipCount {
		t.Fatalf("graph standard payload missing: %#v", payload)
	}
	nodeShapes := payload["node_shapes"].([]interface{})
	nodeShape := nodeShapes[0].(map[string]interface{})
	if nodeShape["kind"] != GraphNodeShapeKindLabelSet || nodeShape["name"] != "Employee+Person" {
		t.Fatalf("node shape payload = %#v", nodeShape)
	}
	labels := nodeShape["labels"].([]interface{})
	if len(labels) != 2 || labels[0] != "Employee" || labels[1] != "Person" {
		t.Fatalf("node shape labels = %#v", labels)
	}
	relationshipShapes := payload["relationship_shapes"].([]interface{})
	relationshipShape := relationshipShapes[0].(map[string]interface{})
	patterns := relationshipShape["patterns"].([]interface{})
	pattern := patterns[0].(map[string]interface{})
	from := pattern["from"].(map[string]interface{})
	if from["shape_name"] != "Employee+Person" {
		t.Fatalf("relationship pattern payload = %#v", pattern)
	}
}

func TestGraphInfoFromPayloadRestoresAndNormalizesGraphFacts(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"model":              " property_graph ",
		"node_count":         int64(12),
		"relationship_count": int64(7),
		"node_shapes": []interface{}{
			map[string]interface{}{
				"kind":   " label_set ",
				"labels": []interface{}{" Person ", "Employee", "Person", ""},
				"properties": []interface{}{
					map[string]interface{}{"name": " name ", "type": "string"},
				},
				"count": int64(12),
			},
		},
		"relationship_shapes": []interface{}{
			map[string]interface{}{
				"type": " WORKS_FOR ",
				"patterns": []interface{}{
					map[string]interface{}{
						"from":  map[string]interface{}{"labels": []interface{}{"Person", "Employee"}},
						"to":    map[string]interface{}{"shape_name": " Company ", "labels": []interface{}{"Company"}},
						"count": int64(7),
					},
				},
				"count": int64(7),
			},
		},
	}

	info := GraphInfoFromPayload(payload)
	if info == nil || info.Model != GraphModelPropertyGraph {
		t.Fatalf("graph info = %#v", info)
	}
	if info.NodeCount == nil || *info.NodeCount != 12 || info.RelationshipCount == nil || *info.RelationshipCount != 7 {
		t.Fatalf("graph counts = %#v / %#v", info.NodeCount, info.RelationshipCount)
	}
	nodeShape := info.NodeShapes[0]
	if nodeShape.Name != "Employee+Person" || nodeShape.Kind != GraphNodeShapeKindLabelSet || len(nodeShape.Labels) != 2 || nodeShape.Labels[0] != "Employee" || nodeShape.Labels[1] != "Person" {
		t.Fatalf("node shape = %#v", nodeShape)
	}
	if nodeShape.Properties[0].Name != "name" || nodeShape.Properties[0].Type != FieldTypeString {
		t.Fatalf("node properties = %#v", nodeShape.Properties)
	}
	pattern := info.RelationshipShapes[0].Patterns[0]
	if pattern.From.ShapeName != "Employee+Person" || pattern.To.ShapeName != "Company" {
		t.Fatalf("relationship pattern = %#v", pattern)
	}
}
