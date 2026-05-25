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

func TestGraphInfoAttributesUsesStandardShapeModel(t *testing.T) {
	t.Parallel()

	nodeCount := int64(12)
	relationshipCount := int64(7)
	directed := true
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
			Count:      &nodeCount,
		}},
		RelationshipShapes: []GraphRelationshipShapeInfo{{
			Type: "WORKS_FOR",
			Patterns: []GraphRelationshipPatternInfo{{
				From:  GraphEndpointInfo{ShapeName: "Person", Labels: []string{"Person"}},
				To:    GraphEndpointInfo{ShapeName: "Company", Labels: []string{"Company"}},
				Count: &relationshipCount,
			}},
			Count: &relationshipCount,
		}},
	}

	attrs := GraphInfoAttributes(info)
	if attrs["edge_count"] != nil || attrs["node_labels"] != nil || attrs["relationship_types"] != nil {
		t.Fatalf("legacy graph attrs should not be written: %#v", attrs)
	}
	if attrs["model"] != GraphModelPropertyGraph || attrs["directed"] != true || attrs["relationship_count"] != relationshipCount {
		t.Fatalf("graph standard attrs missing: %#v", attrs)
	}
	nodeShapes := attrs["node_shapes"].([]interface{})
	nodeShape := nodeShapes[0].(map[string]interface{})
	if nodeShape["kind"] != GraphNodeShapeKindLabel {
		t.Fatalf("node shape attrs = %#v", nodeShape)
	}
	relationshipShapes := attrs["relationship_shapes"].([]interface{})
	relationshipShape := relationshipShapes[0].(map[string]interface{})
	patterns := relationshipShape["patterns"].([]interface{})
	pattern := patterns[0].(map[string]interface{})
	from := pattern["from"].(map[string]interface{})
	if from["shape_name"] != "Person" {
		t.Fatalf("relationship pattern attrs = %#v", pattern)
	}
}

func TestGraphInfoFromAttributesRestoresAndNormalizesGraphFacts(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"type_info": map[string]interface{}{
			"graph": map[string]interface{}{
				"model":              " property_graph ",
				"node_count":         int64(12),
				"relationship_count": int64(7),
				"node_shapes": []interface{}{
					map[string]interface{}{
						"name":   " Person ",
						"kind":   " label ",
						"labels": []interface{}{" Person ", ""},
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
								"from":  map[string]interface{}{"shape_name": " Person ", "labels": []interface{}{"Person"}},
								"to":    map[string]interface{}{"shape_name": " Company ", "labels": []interface{}{"Company"}},
								"count": int64(7),
							},
						},
						"count": int64(7),
					},
				},
			},
		},
	}

	info := GraphInfoFromAttributes(attrs)
	if info == nil || info.Model != GraphModelPropertyGraph {
		t.Fatalf("graph info = %#v", info)
	}
	if info.NodeCount == nil || *info.NodeCount != 12 || info.RelationshipCount == nil || *info.RelationshipCount != 7 {
		t.Fatalf("graph counts = %#v / %#v", info.NodeCount, info.RelationshipCount)
	}
	nodeShape := info.NodeShapes[0]
	if nodeShape.Name != "Person" || nodeShape.Kind != GraphNodeShapeKindLabel || len(nodeShape.Labels) != 1 || nodeShape.Labels[0] != "Person" {
		t.Fatalf("node shape = %#v", nodeShape)
	}
	if nodeShape.Properties[0].Name != "name" || nodeShape.Properties[0].Type != FieldTypeString {
		t.Fatalf("node properties = %#v", nodeShape.Properties)
	}
	pattern := info.RelationshipShapes[0].Patterns[0]
	if pattern.From.ShapeName != "Person" || pattern.To.ShapeName != "Company" {
		t.Fatalf("relationship pattern = %#v", pattern)
	}
}
