package service

import (
	"context"
	"sort"
	"time"

	commonRuntimeHealth "github.com/addp/common/runtimehealth"
)

const runtimeHealthObservationWindow = 24 * time.Hour

type runtimeHeartbeatLister interface {
	ListSince(context.Context, time.Time) ([]commonRuntimeHealth.Heartbeat, error)
}

type RuntimeHealthService struct {
	repo runtimeHeartbeatLister
	now  func() time.Time
}

func NewRuntimeHealthService(repo runtimeHeartbeatLister) *RuntimeHealthService {
	return &RuntimeHealthService{repo: repo, now: time.Now}
}

type RuntimeHealthInstance struct {
	InstanceID     string     `json:"instance_id"`
	Status         string     `json:"status"`
	Capacity       int        `json:"capacity"`
	ActiveCount    int        `json:"active_count"`
	StartedAt      time.Time  `json:"started_at"`
	HeartbeatAt    time.Time  `json:"heartbeat_at"`
	HeartbeatAgeMs int64      `json:"heartbeat_age_ms"`
	ExpiresAt      time.Time  `json:"expires_at"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`
}

type RuntimeHealthSummary struct {
	Module           string                  `json:"module"`
	Role             string                  `json:"role"`
	RuntimeName      string                  `json:"runtime_name"`
	Status           string                  `json:"status"`
	HealthyInstances int                     `json:"healthy_instances"`
	KnownInstances   int                     `json:"known_instances"`
	Capacity         int                     `json:"capacity"`
	ActiveCount      int                     `json:"active_count"`
	LastHeartbeatAt  time.Time               `json:"last_heartbeat_at"`
	ExpiresAt        time.Time               `json:"expires_at"`
	Instances        []RuntimeHealthInstance `json:"instances"`
}

func (s *RuntimeHealthService) ListHealth(ctx context.Context) ([]RuntimeHealthSummary, error) {
	now := s.now().UTC()
	heartbeats, err := s.repo.ListSince(ctx, now.Add(-runtimeHealthObservationWindow))
	if err != nil {
		return nil, err
	}
	return aggregateRuntimeHealth(heartbeats, now), nil
}

func aggregateRuntimeHealth(heartbeats []commonRuntimeHealth.Heartbeat, now time.Time) []RuntimeHealthSummary {
	type groupKey struct{ module, role, runtimeName string }
	groups := map[groupKey]*RuntimeHealthSummary{}
	for _, heartbeat := range heartbeats {
		key := groupKey{heartbeat.Module, heartbeat.Role, heartbeat.RuntimeName}
		summary := groups[key]
		if summary == nil {
			summary = &RuntimeHealthSummary{
				Module: heartbeat.Module, Role: heartbeat.Role, RuntimeName: heartbeat.RuntimeName,
				Status: "stopped", Instances: []RuntimeHealthInstance{},
			}
			groups[key] = summary
		}
		status := runtimeInstanceStatus(heartbeat, now)
		age := now.Sub(heartbeat.HeartbeatAt.UTC()).Milliseconds()
		if age < 0 {
			age = 0
		}
		summary.Instances = append(summary.Instances, RuntimeHealthInstance{
			InstanceID: heartbeat.InstanceID, Status: status, Capacity: heartbeat.Capacity,
			ActiveCount: heartbeat.ActiveCount, StartedAt: heartbeat.StartedAt,
			HeartbeatAt: heartbeat.HeartbeatAt, HeartbeatAgeMs: age,
			ExpiresAt: heartbeat.ExpiresAt, StoppedAt: heartbeat.StoppedAt,
		})
		summary.KnownInstances++
		if heartbeat.HeartbeatAt.After(summary.LastHeartbeatAt) {
			summary.LastHeartbeatAt = heartbeat.HeartbeatAt
			summary.ExpiresAt = heartbeat.ExpiresAt
		}
		if status == "up" {
			summary.HealthyInstances++
			summary.Capacity += heartbeat.Capacity
			summary.ActiveCount += heartbeat.ActiveCount
			summary.Status = "up"
		} else if status == "down" && summary.Status != "up" {
			summary.Status = "down"
		}
	}

	result := make([]RuntimeHealthSummary, 0, len(groups))
	for _, summary := range groups {
		sort.Slice(summary.Instances, func(i, j int) bool {
			return summary.Instances[i].HeartbeatAt.After(summary.Instances[j].HeartbeatAt)
		})
		result = append(result, *summary)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Module != result[j].Module {
			return result[i].Module < result[j].Module
		}
		if result[i].Role != result[j].Role {
			return result[i].Role < result[j].Role
		}
		return result[i].RuntimeName < result[j].RuntimeName
	})
	return result
}

func runtimeInstanceStatus(heartbeat commonRuntimeHealth.Heartbeat, now time.Time) string {
	if heartbeat.StoppedAt != nil {
		return "stopped"
	}
	if heartbeat.ExpiresAt.Before(now) {
		return "down"
	}
	return "up"
}
