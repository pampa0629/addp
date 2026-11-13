package capture

import (
	"github.com/addp/transfer/internal/models"
	"time"
)

// Health describes capture itself, independently of target consumption and source probes.
func (s *Supervisor) healthObservation(resource *models.CaptureResource, status *ConnectorStatus, now time.Time) models.JSONMap {
	value := models.CaptureHealthObservation{State: "unknown", SampledAt: now, ExpiresAt: now.Add(2 * s.config.MonitorInterval)}
	if status != nil {
		switch status.ConnectorState {
		case "FAILED", "PAUSED", "UNASSIGNED":
			value.State, value.Reason = "failed", "connector_not_running"
		case "RUNNING":
			for _, state := range status.TaskStates {
				if state == "FAILED" || state == "PAUSED" || state == "UNASSIGNED" {
					value.State, value.Reason = "failed", "task_not_running"
					break
				}
			}
			if value.State == "unknown" && connectorHealthy(status) {
				if resource.SourceType != models.CaptureSourcePostgreSQL {
					value.State = "healthy"
				} else {
					delay := models.NewCaptureSummary(resource).DelayObservation
					if delay != nil && delay.Status == "available" && delay.Connected != nil && !delay.SampledAt.After(now) && delay.ExpiresAt.After(now) {
						if *delay.Connected {
							value.State = "healthy"
						} else {
							value.State, value.Reason = "failed", "source_disconnected"
						}
					}
				}
			}
		}
	}
	if value.State == "failed" {
		since := now
		previous := models.NewCaptureSummary(resource).HealthObservation
		if previous != nil && previous.State == "failed" && previous.FailedSince != nil && !previous.SampledAt.After(now) && !previous.FailedSince.After(previous.SampledAt) && !previous.ExpiresAt.Before(now) {
			since = *previous.FailedSince
		}
		value.FailedSince = &since
	}
	return sourceObservationMap(&value)
}
