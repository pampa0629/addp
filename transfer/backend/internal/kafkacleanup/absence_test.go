package kafkacleanup

import (
	"context"
	"errors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"testing"
	"time"
)

type topicObservation struct {
	values kadm.TopicDetails
	err    error
}

func (s topicObservation) ListTopicsWithInternal(context.Context, ...string) (kadm.TopicDetails, error) {
	return s.values, s.err
}

type groupObservation struct {
	values kadm.DescribedGroups
	err    error
}

func (s groupObservation) DescribeGroups(context.Context, ...string) (kadm.DescribedGroups, error) {
	return s.values, s.err
}
func TestExactAbsenceRequired(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values kadm.TopicDetails
		err    error
		pass   bool
	}{
		{name: "exact_missing", values: kadm.TopicDetails{"owned": {Err: kerr.UnknownTopicOrPartition}}, pass: true},
		{name: "omitted", values: kadm.TopicDetails{}},
		{name: "other_target", values: kadm.TopicDetails{"other": {Err: kerr.UnknownTopicOrPartition}}},
		{name: "permission", values: kadm.TopicDetails{"owned": {Err: kerr.TopicAuthorizationFailed}}},
		{name: "transport", err: errors.New("unavailable")},
		{name: "exists", values: kadm.TopicDetails{"owned": {}}},
	} {
		t.Run("topic_"+tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
			defer cancel()
			err := WaitTopicAbsent(ctx, topicObservation{tc.values, tc.err}, "owned")
			if (err == nil) != tc.pass {
				t.Fatalf("err=%v pass=%v", err, tc.pass)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		values kadm.DescribedGroups
		pass   bool
	}{
		{"exact_missing", kadm.DescribedGroups{"owned": {Group: "owned", Err: kerr.GroupIDNotFound}}, true},
		{"exact_dead_missing", kadm.DescribedGroups{"owned": {Group: "owned", State: "Dead"}}, true},
		{"omitted", kadm.DescribedGroups{}, false},
		{"other_target", kadm.DescribedGroups{"other": {Group: "other", Err: kerr.GroupIDNotFound}}, false},
		{"wrong_identity", kadm.DescribedGroups{"owned": {Group: "other", Err: kerr.GroupIDNotFound}}, false},
		{"omitted_identity", kadm.DescribedGroups{"owned": {Err: kerr.GroupIDNotFound}}, false},
		{"permission", kadm.DescribedGroups{"owned": {Group: "owned", State: "Dead", Err: kerr.GroupAuthorizationFailed}}, false},
		{"empty_is_not_absent", kadm.DescribedGroups{"owned": {Group: "owned", State: "Empty"}}, false},
		{"stable_is_not_absent", kadm.DescribedGroups{"owned": {Group: "owned", State: "Stable"}}, false},
		{"dead_protocol_type", kadm.DescribedGroups{"owned": {Group: "owned", State: "Dead", ProtocolType: "consumer"}}, false},
		{"dead_protocol", kadm.DescribedGroups{"owned": {Group: "owned", State: "Dead", Protocol: "range"}}, false},
		{"dead_members", kadm.DescribedGroups{"owned": {Group: "owned", State: "Dead", Members: []kadm.DescribedGroupMember{{MemberID: "live"}}}}, false},
		{"exists", kadm.DescribedGroups{"owned": {Group: "owned"}}, false},
	} {
		t.Run("group_"+tc.name, func(t *testing.T) {
			budget := time.Millisecond
			if tc.pass {
				budget = time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			err := WaitGroupAbsent(ctx, groupObservation{tc.values, nil}, "owned")
			if (err == nil) != tc.pass {
				t.Fatalf("err=%v pass=%v", err, tc.pass)
			}
		})
	}
}

func TestGroupAbsenceRejectsEmptyTarget(t *testing.T) {
	err := WaitGroupAbsent(t.Context(), groupObservation{values: kadm.DescribedGroups{"": {Err: kerr.GroupIDNotFound}}}, "")
	if err == nil {
		t.Fatal("empty group target was accepted as exact absence")
	}
}
func TestCancelledObservationDoesNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	err := waitAbsent(ctx, func(context.Context) (bool, error) { calls++; return true, nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
