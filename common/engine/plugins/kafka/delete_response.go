package kafka

import (
	"errors"
	"fmt"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

// ValidateTopicDeletionResponse requires an explicit response for the exact
// requested topic. An acknowledgement alone does not prove all in-flight
// producers have stopped or the asynchronous physical deletion has finished.
func ValidateTopicDeletionResponse(topic string, responses kadm.DeleteTopicResponses) error {
	response, ok := responses[topic]
	if topic == "" || !ok || response.Topic != topic {
		return fmt.Errorf("Kafka topic deletion response is incomplete")
	}
	if errors.Is(response.Err, kerr.UnknownTopicOrPartition) {
		return nil
	}
	return response.Err
}
func ValidateGroupDeletionResponse(group string, responses kadm.DeleteGroupResponses) error {
	response, ok := responses[group]
	if group == "" || !ok || response.Group != group {
		return fmt.Errorf("Kafka group deletion response is incomplete")
	}
	if errors.Is(response.Err, kerr.GroupIDNotFound) {
		return nil
	}
	return response.Err
}
