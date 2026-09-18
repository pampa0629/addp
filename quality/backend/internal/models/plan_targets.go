package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/addp/common/resourcetree"
)

// PlanTableBinding names a required input and its optional default resource.
type PlanTableBinding struct {
	Alias     string   `json:"alias"`
	Locator   string   `json:"locator"`
	RecordKey []string `json:"record_key,omitempty"`
}

type PlanRunRequest struct {
	TableBindings map[string]string `json:"table_bindings"`
}

var targetAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateTableBindings validates definitions; requireTargets is true for execution snapshots.
func ValidateTableBindings(bindings []PlanTableBinding, requireTargets bool) error {
	if len(bindings) == 0 || len(bindings) > 100 {
		return fmt.Errorf("table_bindings must contain between 1 and 100 items")
	}
	aliases, targets := map[string]bool{}, map[string]bool{}
	var engineID uint
	for _, b := range bindings {
		if len(b.RecordKey) > 8 {
			return fmt.Errorf("record_key may contain at most 8 fields")
		}
		keys := map[string]bool{}
		for _, key := range b.RecordKey {
			if key == "" || len(key) > 200 || strings.TrimSpace(key) != key || keys[key] {
				return fmt.Errorf("invalid or duplicate record_key field")
			}
			keys[key] = true
		}
		if !targetAliasPattern.MatchString(b.Alias) || aliases[b.Alias] {
			return fmt.Errorf("invalid or duplicate table alias %q", b.Alias)
		}
		aliases[b.Alias] = true
		if b.Locator == "" && !requireTargets {
			continue
		}
		locator, err := resourcetree.ParseURI(b.Locator)
		if err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeTable || locator.ToURI() != b.Locator {
			return fmt.Errorf("invalid or missing table target for %q", b.Alias)
		}
		if engineID != 0 && engineID != locator.EngineID {
			return fmt.Errorf("table bindings must use one engine")
		}
		engineID = locator.EngineID
		identity := strings.SplitN(b.Locator, "?", 2)[0]
		if targets[identity] {
			return fmt.Errorf("physical table binding is duplicated")
		}
		targets[identity] = true
	}
	return nil
}

// ResolvePlanTargets never mutates the saved defaults. Call under the plan row lock.
func ResolvePlanTargets(raw json.RawMessage, request PlanRunRequest) ([]PlanTableBinding, string, error) {
	var bindings []PlanTableBinding
	if err := json.Unmarshal(raw, &bindings); err != nil {
		return nil, "", err
	}
	known := map[string]bool{}
	for i := range bindings {
		known[bindings[i].Alias] = true
		if value, ok := request.TableBindings[bindings[i].Alias]; ok {
			if value == "" {
				return nil, "", fmt.Errorf("explicit table target %q cannot be empty", bindings[i].Alias)
			}
			bindings[i].Locator = value
		}
	}
	for alias := range request.TableBindings {
		if !known[alias] {
			return nil, "", fmt.Errorf("unknown table alias %q", alias)
		}
	}
	key, err := PlanTargetKey(bindings)
	return bindings, key, err
}

// PlanTargetKey ignores Meta cache hints, but includes every alias and reference table.
func PlanTargetKey(bindings []PlanTableBinding) (string, error) {
	if err := ValidateTableBindings(bindings, true); err != nil {
		return "", err
	}
	identities := make([]string, 0, len(bindings))
	for _, b := range bindings {
		identities = append(identities, b.Alias+"="+strings.SplitN(b.Locator, "?", 2)[0])
	}
	sort.Strings(identities)
	digest := sha256.Sum256([]byte(strings.Join(identities, "\n")))
	return hex.EncodeToString(digest[:]), nil
}
