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
	// Redis channel for scan completed events
	ChannelScanCompleted = "meta:scan_completed"

	// Scan types
	ScanTypeDatabase      = "database"
	ScanTypeObjectStorage = "object_storage"
)

// ScanCompletedEvent 扫描完成事件
type ScanCompletedEvent struct {
	EngineID          uint      `json:"engine_id"`
	TenantID          uint      `json:"tenant_id"`
	ScanType          string    `json:"scan_type"`           // database or object_storage
	ScannedNodes      []string  `json:"scanned_nodes"`       // schemas or prefixes that were scanned
	ScannedItemsCount int       `json:"scanned_items_count"` // total number of items scanned
	Timestamp         time.Time `json:"timestamp"`
}

// ScanEventPublisher 扫描事件发布器
type ScanEventPublisher struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewScanEventPublisher 创建扫描事件发布器
func NewScanEventPublisher(redisClient *redis.Client, logger *slog.Logger) *ScanEventPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScanEventPublisher{
		redis:  redisClient,
		logger: logger.With("component", "scan_event_publisher"),
	}
}

// PublishScanCompleted 发布扫描完成事件
func (p *ScanEventPublisher) PublishScanCompleted(ctx context.Context, event ScanCompletedEvent) error {
	if p.redis == nil {
		p.logger.Warn("Redis client not configured, skipping scan event publish",
			"engine_id", event.EngineID,
			"scan_type", event.ScanType)
		return nil // 不阻塞业务逻辑
	}

	// 设置时间戳
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal scan event: %w", err)
	}

	if err := p.redis.Publish(ctx, ChannelScanCompleted, data).Err(); err != nil {
		p.logger.Error("failed to publish scan completed event",
			"engine_id", event.EngineID,
			"tenant_id", event.TenantID,
			"scan_type", event.ScanType,
			"error", err)
		return fmt.Errorf("failed to publish scan event: %w", err)
	}

	p.logger.Info("scan completed event published",
		"engine_id", event.EngineID,
		"tenant_id", event.TenantID,
		"scan_type", event.ScanType,
		"items_count", event.ScannedItemsCount)
	return nil
}

// ScanEventSubscriber 扫描事件订阅器
type ScanEventSubscriber struct {
	redis   *redis.Client
	logger  *slog.Logger
	handler ScanCompletedHandler
	ctx     context.Context
	cancel  context.CancelFunc
}

// ScanCompletedHandler 扫描完成处理函数
type ScanCompletedHandler func(event ScanCompletedEvent) error

// NewScanEventSubscriber 创建扫描事件订阅器
func NewScanEventSubscriber(redisClient *redis.Client, handler ScanCompletedHandler, logger *slog.Logger) *ScanEventSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScanEventSubscriber{
		redis:   redisClient,
		logger:  logger.With("component", "scan_event_subscriber"),
		handler: handler,
	}
}

// Start 启动订阅（阻塞）
func (s *ScanEventSubscriber) Start() {
	if s.redis == nil {
		s.logger.Warn("Redis client not configured, scan event subscription disabled")
		return
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	pubsub := s.redis.Subscribe(s.ctx, ChannelScanCompleted)
	defer pubsub.Close()

	s.logger.Info("scan event subscriber started", "channel", ChannelScanCompleted)

	// 接收消息
	ch := pubsub.Channel()
	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("scan event subscriber stopped")
			return
		case msg, ok := <-ch:
			if !ok {
				s.logger.Warn("channel closed, resubscribing...")
				// 重新订阅
				time.Sleep(time.Second)
				pubsub = s.redis.Subscribe(s.ctx, ChannelScanCompleted)
				ch = pubsub.Channel()
				continue
			}

			var event ScanCompletedEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				s.logger.Error("failed to unmarshal scan event",
					"payload", msg.Payload,
					"error", err)
				continue
			}

			// 调用处理器
			if err := s.handler(event); err != nil {
				s.logger.Error("failed to handle scan event",
					"engine_id", event.EngineID,
					"tenant_id", event.TenantID,
					"scan_type", event.ScanType,
					"error", err)
			}
		}
	}
}

// Stop 停止订阅
func (s *ScanEventSubscriber) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
