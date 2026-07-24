package deadletter

import (
	"context"
	"fmt"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaTopicCleanerConfig struct {
	ConnectionInfo engineplugin.ConnectionInfo
}

// KafkaTopicCleaner 使用 Infra Kafka admin principal 幂等删除 task-owned DLQ topic。
type KafkaTopicCleaner struct {
	client *kgo.Client
	admin  *kadm.Client
}

func NewKafkaTopicCleaner(config KafkaTopicCleanerConfig) (*KafkaTopicCleaner, error) {
	client, err := newKafkaClient(config.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	return &KafkaTopicCleaner{client: client, admin: kadm.NewClient(client)}, nil
}

func (c *KafkaTopicCleaner) DeleteTaskTopic(ctx context.Context, tenantID, taskID uint) error {
	if c == nil || c.client == nil || c.admin == nil {
		return fmt.Errorf("dead-letter Kafka topic cleaner is not configured")
	}
	topic, err := TopicName(tenantID, taskID)
	if err != nil {
		return err
	}
	responses, err := c.admin.DeleteTopics(ctx, topic)
	if err != nil {
		return fmt.Errorf("delete Infra Kafka dead-letter topic %q: %w", topic, err)
	}
	response, ok := responses[topic]
	if !ok || response.Err == nil || response.Err == kerr.UnknownTopicOrPartition {
		return nil
	}
	return fmt.Errorf("delete Infra Kafka dead-letter topic %q: %w", topic, response.Err)
}

func (c *KafkaTopicCleaner) Close() {
	if c != nil && c.client != nil {
		c.client.Close()
	}
}
