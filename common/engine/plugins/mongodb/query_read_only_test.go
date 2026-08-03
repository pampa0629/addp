package mongodb

import "testing"

func TestMongoAggregateHasWriteStage(t *testing.T) {
	readPipeline := []interface{}{map[string]interface{}{"$match": map[string]interface{}{"status": "paid"}}}
	if mongoAggregateHasWriteStage(readPipeline) {
		t.Fatal("read-only aggregate was classified as writing")
	}
	writePipeline := []interface{}{map[string]interface{}{"$merge": "summary"}}
	if !mongoAggregateHasWriteStage(writePipeline) {
		t.Fatal("$merge aggregate must be rejected in read-only mode")
	}
}
