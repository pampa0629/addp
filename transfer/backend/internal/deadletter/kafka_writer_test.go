package deadletter

import "testing"

func TestTopicNameUsesTransferOwnedNamespace(t *testing.T) {
	got, err := TopicName(7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if got != "__addp_dlq.7.11" {
		t.Fatalf("topic = %q", got)
	}
}

func TestSamePolicyIgnoresKafkaPolicyOrder(t *testing.T) {
	if !samePolicy("delete,compact", "compact", "delete") {
		t.Fatal("equivalent Kafka cleanup policies were not accepted")
	}
	if samePolicy("delete", "compact", "delete") {
		t.Fatal("incomplete Kafka cleanup policy was accepted")
	}
}
