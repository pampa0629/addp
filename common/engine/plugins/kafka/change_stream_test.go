package kafka

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestAdjustKafkaFetchOffsetsUsesTransferCommittedPosition(t *testing.T) {
	assigned := map[string]map[int32]kgo.Offset{
		"orders": {
			0: kgo.NewOffset().AtStart(),
			1: kgo.NewOffset().AtStart(),
		},
	}
	adjusted, err := adjustKafkaFetchOffsets("orders", map[string]plugin.ChangeStreamPosition{
		"0": kafkaOffsetPosition("0", 73093),
	}, plugin.ChangeStreamInitialLatest, assigned)
	if err != nil {
		t.Fatal(err)
	}
	if got := adjusted["orders"][0].EpochOffset().Offset; got != 73093 {
		t.Fatalf("partition 0 offset = %d, want committed 73093", got)
	}
	if got := adjusted["orders"][1].EpochOffset().Offset; got != -1 {
		t.Fatalf("partition 1 offset = %d, want latest sentinel -1", got)
	}
}

func TestAdjustKafkaFetchOffsetsPreservesUnrelatedTopics(t *testing.T) {
	assigned := map[string]map[int32]kgo.Offset{
		"orders": {0: kgo.NewOffset().AtEnd()},
		"other":  {0: kgo.NewOffset().At(42)},
	}
	adjusted, err := adjustKafkaFetchOffsets("orders", nil, plugin.ChangeStreamInitialEarliest, assigned)
	if err != nil {
		t.Fatal(err)
	}
	if got := adjusted["orders"][0].EpochOffset().Offset; got != -2 {
		t.Fatalf("orders offset = %d, want earliest sentinel -2", got)
	}
	if got := adjusted["other"][0].EpochOffset().Offset; got != 42 {
		t.Fatalf("other topic offset = %d, want unchanged 42", got)
	}
}
