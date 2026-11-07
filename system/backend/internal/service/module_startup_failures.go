package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/addp/common/jsonmap"
	"github.com/addp/system/internal/models"
)

const startupResultsFile = "service-startup-results.json"

var startupAttemptPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var startupModulePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)

type backendStartupResult struct {
	Attempt   string    `json:"attempt"`
	StartedAt time.Time `json:"started_at"`
	FailedAt  time.Time `json:"failed_at"`
	Reason    string    `json:"reason"`
}

type backendStartupResults struct {
	Schema       string                          `json:"schema"`
	DeploymentID string                          `json:"deployment_id"`
	Modules      map[string]backendStartupResult `json:"modules"`
}

func parseBackendStartupResults(raw []byte, deploymentID string, now time.Time) *backendStartupResults {
	if _, err := jsonmap.DecodeObjectStrict(raw, deploymentDiscoveryLimit, 4); err != nil {
		return nil
	}
	var result backendStartupResults
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Schema != "addp.service-startup-results/v1" ||
		result.DeploymentID != deploymentID || result.Modules == nil || len(result.Modules) > 1000 {
		return nil
	}
	for module, row := range result.Modules {
		if !startupModulePattern.MatchString(module) || !startupAttemptPattern.MatchString(row.Attempt) ||
			row.StartedAt.IsZero() || row.StartedAt.After(now) {
			return nil
		}
		switch row.Reason {
		case "":
			if !row.FailedAt.IsZero() {
				return nil
			}
		case "build_failed", "process_exited", "readiness_timeout":
			if row.FailedAt.Before(row.StartedAt) || row.FailedAt.After(now) {
				return nil
			}
		default:
			return nil
		}
	}
	return &result
}

func (s *ModuleRegistryService) ListModuleStartupFailures(ctx context.Context) ([]models.ModuleStartupFailure, error) {
	failures := []models.ModuleStartupFailure{}
	plan, digest, _ := s.readServiceExpectations(ctx)
	if plan == nil {
		return failures, nil
	}
	raw, state := s.deploymentDiscovery.readDocument(ctx, startupResultsFile)
	if state != "" {
		return failures, nil
	}
	results := parseBackendStartupResults(raw, plan.DeploymentID, time.Now().UTC())
	if results == nil {
		return failures, nil
	}
	activeFailures := map[string]backendStartupResult{}
	for _, group := range plan.Groups {
		row := results.Modules[group.ModuleName]
		if group.Role == "backend" && group.DesiredState == "running" && row.Reason != "" &&
			!row.StartedAt.Before(group.ChangedAt) {
			activeFailures[group.ModuleName] = row
		}
	}
	if len(activeFailures) == 0 {
		return failures, nil
	}
	definitions, err := s.repo.ListModules()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, module := range definitions {
		row, exists := activeFailures[module.ModuleName]
		if exists && (!module.Enabled || backendStartupFailureSuperseded(module.RuntimeInstances, row.FailedAt, now)) {
			delete(activeFailures, module.ModuleName)
		}
	}
	currentPlan, planState := s.deploymentDiscovery.readDocument(ctx, "service-expectations.json")
	currentResults, resultsState := s.deploymentDiscovery.readDocument(ctx, startupResultsFile)
	if planState != "" || resultsState != "" || sha256.Sum256(currentPlan) != digest || !bytes.Equal(currentResults, raw) {
		return failures, nil
	}
	for module, row := range activeFailures {
		failures = append(failures, models.ModuleStartupFailure{ModuleName: module, FailedAt: row.FailedAt, Reason: row.Reason})
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].ModuleName < failures[j].ModuleName })
	return failures, nil
}

func backendStartupFailureSuperseded(instances []models.ModuleRuntimeInstance, failedAt, now time.Time) bool {
	for _, instance := range instances {
		if instance.Role == models.ModuleRuntimeRoleBackend &&
			(instance.RegisteredAt.After(failedAt) ||
				instance.Status == models.ModuleRuntimeStatusUp && instance.LeaseExpiresAt.After(now)) {
			return true
		}
	}
	return false
}
