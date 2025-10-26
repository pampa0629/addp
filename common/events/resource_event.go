package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Redis channels
	ChannelResourceChanged = "resource:changed"
	ChannelResourceDeleted = "resource:deleted"

	// Event actions
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// ResourceChangeEvent 资源变更事件
type ResourceChangeEvent struct {
	ResourceID uint      `json:"resource_id"`
	Action     string    `json:"action"` // create, update, delete
	Timestamp  time.Time `json:"timestamp"`
}

// ResourceEventPublisher 资源事件发布器
type ResourceEventPublisher struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewResourceEventPublisher 创建资源事件发布器
func NewResourceEventPublisher(redisClient *redis.Client, logger *slog.Logger) *ResourceEventPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResourceEventPublisher{
		redis:  redisClient,
		logger: logger.With("component", "resource_event_publisher"),
	}
}

// PublishResourceChange 发布资源变更事件
func (p *ResourceEventPublisher) PublishResourceChange(ctx context.Context, resourceID uint, action string) error {
	if p.redis == nil {
		p.logger.Warn("Redis client not configured, skipping event publish",
			"resource_id", resourceID,
			"action", action)
		return nil // 不阻塞业务逻辑
	}

	event := ResourceChangeEvent{
		ResourceID: resourceID,
		Action:     action,
		Timestamp:  time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	channel := ChannelResourceChanged
	if action == ActionDelete {
		channel = ChannelResourceDeleted
	}

	if err := p.redis.Publish(ctx, channel, data).Err(); err != nil {
		p.logger.Error("failed to publish resource event",
			"resource_id", resourceID,
			"action", action,
			"channel", channel,
			"error", err)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	p.logger.Info("resource event published",
		"resource_id", resourceID,
		"action", action,
		"channel", channel)
	return nil
}

// ResourceEventSubscriber 资源事件订阅器
type ResourceEventSubscriber struct {
	redis   *redis.Client
	logger  *slog.Logger
	handler ResourceChangeHandler
	ctx     context.Context
	cancel  context.CancelFunc
}

// ResourceChangeHandler 资源变更处理函数
type ResourceChangeHandler func(event ResourceChangeEvent) error

// NewResourceEventSubscriber 创建资源事件订阅器
func NewResourceEventSubscriber(
	redisClient *redis.Client,
	handler ResourceChangeHandler,
	logger *slog.Logger,
) *ResourceEventSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ResourceEventSubscriber{
		redis:   redisClient,
		logger:  logger.With("component", "resource_event_subscriber"),
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 启动订阅
func (s *ResourceEventSubscriber) Start() error {
	if s.redis == nil {
		s.logger.Warn("Redis client not configured, event subscription disabled")
		return nil
	}

	if s.handler == nil {
		return fmt.Errorf("handler is required")
	}

	pubsub := s.redis.Subscribe(s.ctx, ChannelResourceChanged, ChannelResourceDeleted)
	defer pubsub.Close()

	s.logger.Info("resource event subscriber started",
		"channels", []string{ChannelResourceChanged, ChannelResourceDeleted})

	// 等待订阅确认
	_, err := pubsub.Receive(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// 处理消息
	ch := pubsub.Channel()
	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("resource event subscriber stopped")
			return nil
		case msg := <-ch:
			s.handleMessage(msg)
		}
	}
}

// Stop 停止订阅
func (s *ResourceEventSubscriber) Stop() {
	s.logger.Info("stopping resource event subscriber")
	s.cancel()
}

// handleMessage 处理单条消息
func (s *ResourceEventSubscriber) handleMessage(msg *redis.Message) {
	var event ResourceChangeEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		s.logger.Error("failed to unmarshal event",
			"channel", msg.Channel,
			"payload", msg.Payload,
			"error", err)
		return
	}

	s.logger.Debug("received resource event",
		"resource_id", event.ResourceID,
		"action", event.Action,
		"channel", msg.Channel)

	if err := s.handler(event); err != nil {
		s.logger.Error("failed to handle resource event",
			"resource_id", event.ResourceID,
			"action", event.Action,
			"error", err)
		// 不重试，避免阻塞后续消息
	}
}
