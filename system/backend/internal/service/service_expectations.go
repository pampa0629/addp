package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
)

func parseServiceExpectations(raw []byte, at time.Time) *commonmodels.ServiceExpectationDocument {
	object, err := jsonmap.DecodeObjectStrict(raw, deploymentDiscoveryLimit, 5)
	if err != nil {
		return nil
	}
	exact := func(value map[string]any, names ...string) bool {
		if len(value) != len(names) {
			return false
		}
		for _, name := range names {
			if _, ok := value[name]; !ok {
				return false
			}
		}
		return true
	}
	if !exact(object, "schema", "deployment_id", "revision", "groups") {
		return nil
	}
	groups, ok := object["groups"].([]any)
	if !ok {
		return nil
	}
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok || !exact(group, "module_name", "role", "runtime_name", "expected_replicas", "desired_state", "changed_at") {
			return nil
		}
		if _, ok := group["runtime_name"].(string); !ok {
			return nil
		}
		changedAt, ok := group["changed_at"].(string)
		if !ok || !strings.HasSuffix(changedAt, "Z") {
			return nil
		}
	}
	var result commonmodels.ServiceExpectationDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Validate(at) != nil {
		return nil
	}
	return &result
}

func (s *ModuleRegistryService) readServiceExpectations(ctx context.Context) (*commonmodels.ServiceExpectationDocument, [32]byte, string) {
	raw, state := s.deploymentDiscovery.readDocument(ctx, "service-expectations.json")
	if state != "" {
		return nil, [32]byte{}, state
	}
	plan := parseServiceExpectations(raw, time.Now().UTC())
	if plan == nil {
		return nil, [32]byte{}, "invalid_intent"
	}
	return plan, sha256.Sum256(raw), ""
}
