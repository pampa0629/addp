// Package kafkacleanup verifies exact Kafka deletion outcomes. It never sends
// a delete or infers absence from an empty response or expired lease.
package kafkacleanup

import (
	"context"
	"fmt"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"time"
)

type topicObserver interface {
	ListTopicsWithInternal(context.Context, ...string) (kadm.TopicDetails, error)
}
type groupObserver interface {
	DescribeGroups(context.Context, ...string) (kadm.DescribedGroups, error)
}

func WaitTopicAbsent(ctx context.Context, admin topicObserver, topic string) error {
	return waitAbsent(ctx, func(ctx context.Context) (bool, error) {
		details, err := admin.ListTopicsWithInternal(ctx, topic)
		if err != nil {
			return false, err
		}
		detail, ok := details[topic]
		if !ok {
			return false, fmt.Errorf("Kafka topic observation omitted exact target")
		}
		if detail.Err == kerr.UnknownTopicOrPartition {
			return true, nil
		}
		return false, detail.Err
	})
}

func WaitGroupAbsent(ctx context.Context, admin groupObserver, group string) error {
	return waitAbsent(ctx, func(ctx context.Context) (bool, error) {
		details, err := admin.DescribeGroups(ctx, group)
		if err != nil {
			return false, err
		}
		detail, ok := details[group]
		if !ok {
			return false, fmt.Errorf("Kafka group observation omitted exact target")
		}
		if detail.Err == kerr.GroupIDNotFound {
			return true, nil
		}
		return false, detail.Err
	})
}

func waitAbsent(ctx context.Context, observe func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		absent, err := observe(ctx)
		if err != nil {
			return err
		}
		if absent {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
