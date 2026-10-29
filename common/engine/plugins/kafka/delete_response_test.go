package kafka

import (
	"errors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"testing"
)

func TestDeleteResponseRequiresExactExplicitAcknowledgement(t *testing.T) {
	for _, responses := range []kadm.DeleteTopicResponses{nil, {}, {"other": {Topic: "other"}}, {"target": {Topic: "other"}}} {
		if ValidateTopicDeletionResponse("target", responses) == nil {
			t.Fatal("missing or mismatched topic response accepted")
		}
	}
	for _, responses := range []kadm.DeleteGroupResponses{nil, {}, {"other": {Group: "other"}}, {"target": {Group: "other"}}} {
		if ValidateGroupDeletionResponse("target", responses) == nil {
			t.Fatal("missing or mismatched group response accepted")
		}
	}
	for _, status := range []error{nil, kerr.UnknownTopicOrPartition, kerr.TopicAuthorizationFailed, kerr.RequestTimedOut} {
		got := ValidateTopicDeletionResponse("target", kadm.DeleteTopicResponses{"target": {Topic: "target", Err: status}})
		if status == nil || status == kerr.UnknownTopicOrPartition {
			if got != nil {
				t.Fatal(got)
			}
		} else if !errors.Is(got, status) {
			t.Fatal("failure not preserved")
		}
	}
	for _, status := range []error{nil, kerr.GroupIDNotFound, kerr.GroupAuthorizationFailed, kerr.RequestTimedOut} {
		got := ValidateGroupDeletionResponse("target", kadm.DeleteGroupResponses{"target": {Group: "target", Err: status}})
		if status == nil || status == kerr.GroupIDNotFound {
			if got != nil {
				t.Fatal(got)
			}
		} else if !errors.Is(got, status) {
			t.Fatal("failure not preserved")
		}
	}
}
