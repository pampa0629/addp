package runtimehealth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RoleExecutionWorker  = "execution_worker"
	RoleContinuousWorker = "continuous_worker"
	RoleDispatcher       = "dispatcher"
	DefaultInterval      = 10 * time.Second
	DefaultTTL           = 30 * time.Second
)

// Heartbeat is a public liveness fact for one ADDP application background
// runtime. It never grants execution, runtime-session, or delivery ownership.
type Heartbeat struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	InstanceID  string     `gorm:"size:128;uniqueIndex;not null" json:"instance_id"`
	Module      string     `gorm:"size:50;not null;index:idx_background_runtime_identity" json:"module"`
	Role        string     `gorm:"size:32;not null;index:idx_background_runtime_identity" json:"role"`
	RuntimeName string     `gorm:"size:100;not null;index:idx_background_runtime_identity" json:"runtime_name"`
	Capacity    int        `gorm:"not null" json:"capacity"`
	ActiveCount int        `gorm:"not null;default:0" json:"active_count"`
	StartedAt   time.Time  `gorm:"not null" json:"started_at"`
	HeartbeatAt time.Time  `gorm:"not null;index" json:"heartbeat_at"`
	ExpiresAt   time.Time  `gorm:"not null;index" json:"expires_at"`
	StoppedAt   *time.Time `gorm:"index" json:"stopped_at,omitempty"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

func (Heartbeat) TableName() string { return "common.background_runtime_heartbeats" }

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func EnsureStore(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("background runtime health database is required")
	}
	if db.Dialector.Name() == "postgres" {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(2026082001)).Error; err != nil {
				return fmt.Errorf("lock background runtime health migration: %w", err)
			}
			return ensureStore(tx)
		})
	}
	return ensureStore(db)
}

func ensureStore(db *gorm.DB) error {
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		return fmt.Errorf("ensure common schema for runtime health: %w", err)
	}
	if err := db.AutoMigrate(&Heartbeat{}); err != nil {
		return fmt.Errorf("migrate background runtime heartbeats: %w", err)
	}
	return nil
}

func (r *Repository) Publish(ctx context.Context, heartbeat *Heartbeat) error {
	if r == nil || r.db == nil || heartbeat == nil {
		return fmt.Errorf("background runtime heartbeat repository and value are required")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"module", "role", "runtime_name", "capacity", "active_count",
			"started_at", "heartbeat_at", "expires_at", "stopped_at", "updated_at",
		}),
	}).Create(heartbeat).Error
}

func (r *Repository) Stop(ctx context.Context, instanceID string, now time.Time) error {
	if r == nil || r.db == nil || instanceID == "" {
		return fmt.Errorf("background runtime heartbeat repository and instance ID are required")
	}
	now = now.UTC()
	result := r.db.WithContext(ctx).Model(&Heartbeat{}).
		Where("instance_id = ?", instanceID).
		Updates(map[string]interface{}{
			"active_count": 0, "heartbeat_at": now, "expires_at": now,
			"stopped_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("background runtime heartbeat %s was not found", instanceID)
	}
	return nil
}

func (r *Repository) ListSince(ctx context.Context, since time.Time) ([]Heartbeat, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("background runtime heartbeat repository is required")
	}
	var heartbeats []Heartbeat
	err := r.db.WithContext(ctx).
		Where("heartbeat_at >= ?", since.UTC()).
		Order("module ASC, role ASC, runtime_name ASC, heartbeat_at DESC, id DESC").
		Find(&heartbeats).Error
	return heartbeats, err
}

func (r *Repository) DeleteBefore(ctx context.Context, before time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("background runtime heartbeat repository is required")
	}
	return r.db.WithContext(ctx).
		Where("expires_at < ?", before.UTC()).
		Delete(&Heartbeat{}).Error
}

type ReporterConfig struct {
	InstanceID  string
	Module      string
	Role        string
	RuntimeName string
	Capacity    int
	Interval    time.Duration
	TTL         time.Duration
	Retention   time.Duration
	ActiveCount func() int
	Logger      *slog.Logger
}

type Reporter struct {
	repo      *Repository
	config    ReporterConfig
	startedAt time.Time
}

func NewReporter(repo *Repository, config ReporterConfig) (*Reporter, error) {
	if repo == nil || config.InstanceID == "" || config.Module == "" || config.RuntimeName == "" {
		return nil, fmt.Errorf("background runtime reporter identity is required")
	}
	switch config.Role {
	case RoleExecutionWorker, RoleContinuousWorker, RoleDispatcher:
	default:
		return nil, fmt.Errorf("unsupported background runtime role %q", config.Role)
	}
	if config.Capacity <= 0 || config.Interval <= 0 || config.TTL <= config.Interval {
		return nil, fmt.Errorf("background runtime reporter capacity, interval, and TTL are invalid")
	}
	if config.Retention <= 0 {
		config.Retention = 7 * 24 * time.Hour
	}
	if config.ActiveCount == nil {
		config.ActiveCount = func() int { return 0 }
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Reporter{repo: repo, config: config, startedAt: time.Now().UTC()}, nil
}

func (r *Reporter) Run(ctx context.Context) {
	if err := r.repo.DeleteBefore(ctx, time.Now().UTC().Add(-r.config.Retention)); err != nil {
		r.config.Logger.Warn("prune stale background runtime heartbeats failed", "error", err)
	}
	r.publish(ctx, time.Now().UTC())
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()
	defer r.markStopped()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.publish(ctx, now.UTC())
		}
	}
}

func (r *Reporter) publish(ctx context.Context, now time.Time) {
	active := r.config.ActiveCount()
	if active < 0 {
		active = 0
	}
	if active > r.config.Capacity {
		active = r.config.Capacity
	}
	heartbeat := &Heartbeat{
		InstanceID: r.config.InstanceID, Module: r.config.Module, Role: r.config.Role,
		RuntimeName: r.config.RuntimeName, Capacity: r.config.Capacity, ActiveCount: active,
		StartedAt: r.startedAt, HeartbeatAt: now, ExpiresAt: now.Add(r.config.TTL), UpdatedAt: now,
	}
	if err := r.repo.Publish(ctx, heartbeat); err != nil && ctx.Err() == nil {
		r.config.Logger.Warn("publish background runtime heartbeat failed", "error", err)
	}
}

func (r *Reporter) markStopped() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.repo.Stop(ctx, r.config.InstanceID, time.Now().UTC()); err != nil {
		r.config.Logger.Warn("mark background runtime stopped failed", "error", err)
	}
}
