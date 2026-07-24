package deadletter

import (
	"testing"

	"github.com/addp/transfer/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestResolveFetchedPayloadReferencesConfirmsExactRecordAndCompactedHoles(t *testing.T) {
	references := []models.DeadLetterPayloadReference{
		{Identity: "identity-10", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 10},
		{Identity: "identity-11", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 11},
		{Identity: "identity-12", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 12},
		{Identity: "identity-20", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 20},
	}
	results := map[string]bool{}
	remaining := resolveFetchedPayloadReferences(results, references, kgo.FetchTopicPartition{
		Topic: "__addp_dlq.7.11",
		FetchPartition: kgo.FetchPartition{Partition: 0, HighWatermark: 21, Records: []*kgo.Record{
			{Offset: 10, Key: []byte("identity-10"), Value: []byte("payload"), Headers: []kgo.RecordHeader{{Key: "addp-schema", Value: []byte(EnvelopeSchemaV1)}}},
			{Offset: 12, Key: []byte("wrong-identity"), Value: []byte("payload"), Headers: []kgo.RecordHeader{{Key: "addp-schema", Value: []byte(EnvelopeSchemaV1)}}},
		}},
	})
	if !results["identity-10"] {
		t.Fatal("exact payload record was not confirmed available")
	}
	if results["identity-11"] || results["identity-12"] {
		t.Fatalf("compacted or mismatched payload records were marked available: %#v", results)
	}
	if len(remaining) != 1 || remaining[0].Identity != "identity-20" {
		t.Fatalf("remaining references = %#v", remaining)
	}
}

func TestResolveFetchedPayloadReferencesConfirmsEmptyCompactedRangeUnavailable(t *testing.T) {
	results := map[string]bool{}
	remaining := resolveFetchedPayloadReferences(results, []models.DeadLetterPayloadReference{
		{Identity: "identity-10", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 10},
	}, kgo.FetchTopicPartition{Topic: "__addp_dlq.7.11", FetchPartition: kgo.FetchPartition{
		Partition: 0, LogStartOffset: 5, HighWatermark: 20,
	}})
	if len(remaining) != 0 || results["identity-10"] {
		t.Fatalf("empty compacted range result=%#v remaining=%#v", results, remaining)
	}
}
