package deadletter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/addp/transfer/internal/models"
)

type PayloadAvailabilityIndex interface {
	ListAvailablePayloadReferences(ctx context.Context, afterIdentity string, limit int) ([]models.DeadLetterPayloadReference, error)
	MarkPayloadUnavailable(ctx context.Context, reference models.DeadLetterPayloadReference, checkedAt time.Time) (bool, error)
}

type PayloadAvailabilityProbe interface {
	Probe(ctx context.Context, references []models.DeadLetterPayloadReference) (map[string]bool, error)
}

type PayloadAvailabilityReconcilerConfig struct {
	Interval  time.Duration
	BatchSize int
	Timeout   time.Duration
}

type PayloadAvailabilityReconcileStats struct {
	Candidates           int
	ConfirmedAvailable   int
	ConfirmedUnavailable int
	UpdatedUnavailable   int
	StaleReferences      int
	Unresolved           int
}

// PayloadAvailabilityReconciler 低频核验当前 payload reference，并只以 CAS 收敛 unavailable。
type PayloadAvailabilityReconciler struct {
	index    PayloadAvailabilityIndex
	probe    PayloadAvailabilityProbe
	config   PayloadAvailabilityReconcilerConfig
	log      *slog.Logger
	cursorMu sync.Mutex
	afterID  string
}

func NewPayloadAvailabilityReconciler(
	index PayloadAvailabilityIndex,
	probe PayloadAvailabilityProbe,
	config PayloadAvailabilityReconcilerConfig,
	log *slog.Logger,
) (*PayloadAvailabilityReconciler, error) {
	if index == nil || probe == nil {
		return nil, fmt.Errorf("dead-letter availability index and probe are required")
	}
	if config.Interval <= 0 || config.Timeout <= 0 || config.BatchSize <= 0 || config.BatchSize > 1000 {
		return nil, fmt.Errorf("dead-letter availability interval, timeout, and batch size must be valid")
	}
	if log == nil {
		log = slog.Default()
	}
	return &PayloadAvailabilityReconciler{index: index, probe: probe, config: config, log: log}, nil
}

func (r *PayloadAvailabilityReconciler) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("dead-letter availability reconciler is not configured")
	}
	r.reconcileAndLog(ctx)
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.reconcileAndLog(ctx)
		}
	}
}

func (r *PayloadAvailabilityReconciler) reconcileAndLog(ctx context.Context) {
	stats, err := r.RunOnce(ctx)
	if err != nil {
		r.log.Warn("DLQ payload availability 核验未完整完成", "error", err, "candidates", stats.Candidates,
			"updated_unavailable", stats.UpdatedUnavailable, "unresolved", stats.Unresolved)
		return
	}
	if stats.Candidates > 0 {
		r.log.Info("DLQ payload availability 核验完成", "candidates", stats.Candidates,
			"confirmed_available", stats.ConfirmedAvailable, "confirmed_unavailable", stats.ConfirmedUnavailable,
			"updated_unavailable", stats.UpdatedUnavailable, "stale_references", stats.StaleReferences)
	}
}

func (r *PayloadAvailabilityReconciler) RunOnce(ctx context.Context) (PayloadAvailabilityReconcileStats, error) {
	var stats PayloadAvailabilityReconcileStats
	if r == nil {
		return stats, fmt.Errorf("dead-letter availability reconciler is not configured")
	}
	afterID := r.cursor()
	references, err := r.index.ListAvailablePayloadReferences(ctx, afterID, r.config.BatchSize)
	if err != nil {
		return stats, err
	}
	if len(references) == 0 && afterID != "" {
		r.setCursor("")
		references, err = r.index.ListAvailablePayloadReferences(ctx, "", r.config.BatchSize)
		if err != nil {
			return stats, err
		}
	}
	if len(references) == 0 {
		return stats, nil
	}
	r.setCursor(references[len(references)-1].Identity)
	stats.Candidates = len(references)

	probeCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	availability, probeErr := r.probe.Probe(probeCtx, references)
	cancel()
	checkedAt := time.Now().UTC()
	var updateErrors []error
	for _, reference := range references {
		available, confirmed := availability[reference.Identity]
		if !confirmed {
			stats.Unresolved++
			continue
		}
		if available {
			stats.ConfirmedAvailable++
			continue
		}
		stats.ConfirmedUnavailable++
		updated, err := r.index.MarkPayloadUnavailable(ctx, reference, checkedAt)
		if err != nil {
			updateErrors = append(updateErrors, err)
			continue
		}
		if updated {
			stats.UpdatedUnavailable++
		} else {
			stats.StaleReferences++
		}
	}
	return stats, errors.Join(append([]error{probeErr}, updateErrors...)...)
}

func (r *PayloadAvailabilityReconciler) cursor() string {
	r.cursorMu.Lock()
	defer r.cursorMu.Unlock()
	return r.afterID
}

func (r *PayloadAvailabilityReconciler) setCursor(identity string) {
	r.cursorMu.Lock()
	r.afterID = identity
	r.cursorMu.Unlock()
}
