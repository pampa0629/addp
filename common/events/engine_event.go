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
	ChannelEngineChanged = "engine:changed"
	ChannelEngineDeleted = "engine:deleted"

	// Event actions
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// EngineChangeEvent 引擎变更事件
type EngineChangeEvent struct {
	EngineID  uint      `json:"engine_id"`
	Action    string    `json:"action"` // create, update, delete
	Timestamp time.Time `json:"timestamp"`
}

// EngineEventPublisher 引擎事件发布器
type EngineEventPublisher struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewEngineEventPublisher 创建引擎事件发布器
func NewEngineEventPublisher(redisClient *redis.Client, logger *slog.Logger) *EngineEventPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &EngineEventPublisher{
		redis:  redisClient,
		logger: logger.With("component", "engine_event_publisher"),
	}
}

// PublishEngineChange 发布引擎变更事件
func (p *EngineEventPublisher) PublishEngineChange(ctx context.Context, engineID uint, action string) error {
	if p.redis == nil {
		p.logger.Warn("Redis client not configured, skipping event publish",
			"engine_id", engineID,
			"action", action)
		return nil // 不阻塞业务逻辑
	}

	event := EngineChangeEvent{
		EngineID:  engineID,
		Action:    action,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	channel := ChannelEngineChanged
	if action == ActionDelete {
		channel = ChannelEngineDeleted
	}

	if err := p.redis.Publish(ctx, channel, data).Err(); err != nil {
		p.logger.Error("failed to publish engine event",
			"engine_id", engineID,
			"action", action,
			"channel", channel,
			"error", err)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	p.logger.Info("engine event published",
		"engine_id", engineID,
		"action", action,
		"channel", channel)
	return nil
}

// EngineEventSubscriber 引擎事件订阅器
type EngineEventSubscriber struct {
	redis   *redis.Client
	logger  *slog.Logger
	handler EngineChangeHandler
	ctx     context.Context
	cancel  context.CancelFunc
}

// EngineChangeHandler 引擎变更处理函数
type EngineChangeHandler func(event EngineChangeEvent) error

// NewEngineEventSubscriber 创建引擎事件订阅器
func NewEngineEventSubscriber(
	redisClient *redis.Client,
	handler EngineChangeHandler,
	logger *slog.Logger,
) *EngineEventSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &EngineEventSubscriber{
		redis:   redisClient,
		logger:  logger.With("component", "engine_event_subscriber"),
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 启动订阅
func (s *EngineEventSubscriber) Start() error {
	if s.redis == nil {
		s.logger.Warn("Redis client not configured, event subscription disabled")
		return nil
	}

	if s.handler == nil {
		return fmt.Errorf("handler is required")
	}

	pubsub := s.redis.Subscribe(s.ctx, ChannelEngineChanged, ChannelEngineDeleted)
	defer pubsub.Close()

	s.logger.Info("engine event subscriber started",
		"channels", []string{ChannelEngineChanged, ChannelEngineDeleted})

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
			s.logger.Info("engine event subscriber stopped")
			return nil
		case msg := <-ch:
			s.handleMessage(msg)
		}
	}
}

// Stop 停止订阅
func (s *EngineEventSubscriber) Stop() {
	s.logger.Info("stopping engine event subscriber")
	s.cancel()
}

// handleMessage 处理单条消息
func (s *EngineEventSubscriber) handleMessage(msg *redis.Message) {
	var event EngineChangeEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		s.logger.Error("failed to unmarshal event",
			"channel", msg.Channel,
			"payload", msg.Payload,
			"error", err)
		return
	}

	s.logger.Debug("received engine event",
		"engine_id", event.EngineID,
		"action", event.Action,
		"channel", msg.Channel)

	if err := s.handler(event); err != nil {
		s.logger.Error("failed to handle engine event",
			"engine_id", event.EngineID,
			"action", event.Action,
			"error", err)
		// 不重试，避免阻塞后续消息
	}
}
