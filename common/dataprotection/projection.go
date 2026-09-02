// Package dataprotection contains the stable, owner-neutral execution contract
// shared by Security and resource owners. It contains no Security persistence
// or policy-authoring logic.
package dataprotection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ProjectionSchemaV1 = "addp.protection_projection/v1"

	ProjectionStateEnrolling = "enrolling"
	ProjectionStateActive    = "active"

	EffectAllow    = "allow"
	EffectMask     = "mask"
	EffectSuppress = "suppress"
	EffectDeny     = "deny"

	AlgorithmKeepPrefixSuffixV1 = "addp.mask.keep_prefix_suffix/v1"
)

var revisionPattern = regexp.MustCompile(`^[0-9]{20}$`)

type ResourceReference struct {
	OwnerModule      string `json:"owner_module"`
	ResourceType     string `json:"resource_type"`
	ResourceIdentity string `json:"resource_identity"`
	ComponentKey     string `json:"component_key,omitempty"`
}

type PathSegment struct {
	Name      string `json:"name"`
	Container string `json:"container"`
}

type Component struct {
	Key               string        `json:"key"`
	Path              []PathSegment `json:"path"`
	ValueType         string        `json:"value_type"`
	SchemaFingerprint string        `json:"schema_fingerprint"`
}

type Decision struct {
	Effect             string         `json:"effect"`
	Algorithm          string         `json:"algorithm,omitempty"`
	Parameters         map[string]any `json:"parameters,omitempty"`
	InvalidValueEffect string         `json:"invalid_value_effect,omitempty"`
}

type Rule struct {
	Action    string    `json:"action"`
	Component Component `json:"component"`
	Decision  Decision  `json:"decision"`
}

type Projection struct {
	SchemaVersion      string            `json:"schema_version"`
	ProjectionID       string            `json:"projection_id"`
	Revision           string            `json:"revision"`
	ConsumerOwner      string            `json:"consumer_owner"`
	State              string            `json:"state"`
	Target             ResourceReference `json:"target"`
	SourceSnapshotHash string            `json:"source_snapshot_hash"`
	Rules              []Rule            `json:"rules"`
	ValidFrom          time.Time         `json:"valid_from"`
	ExpiresAt          time.Time         `json:"expires_at"`
	Checksum           string            `json:"checksum"`
}

func (p Projection) Validate(now time.Time) error {
	if p.SchemaVersion != ProjectionSchemaV1 {
		return errors.New("unsupported protection projection schema")
	}
	if strings.TrimSpace(p.ProjectionID) == "" || !revisionPattern.MatchString(p.Revision) {
		return errors.New("invalid protection projection identity")
	}
	if strings.TrimSpace(p.ConsumerOwner) == "" {
		return errors.New("protection projection consumer owner is required")
	}
	if p.State != ProjectionStateEnrolling && p.State != ProjectionStateActive {
		return errors.New("invalid protection projection state")
	}
	if err := p.Target.Validate(); err != nil {
		return err
	}
	if p.ValidFrom.IsZero() || p.ExpiresAt.IsZero() || !p.ExpiresAt.After(p.ValidFrom) {
		return errors.New("invalid protection projection validity interval")
	}
	if !now.IsZero() && (now.Before(p.ValidFrom) || !now.Before(p.ExpiresAt)) {
		return errors.New("protection projection is not currently valid")
	}
	if p.State == ProjectionStateEnrolling {
		if p.SourceSnapshotHash != "" || len(p.Rules) != 0 {
			return errors.New("enrolling projection must be a resource-level deny gate")
		}
	} else {
		if !strings.HasPrefix(p.SourceSnapshotHash, "sha256:") {
			return errors.New("invalid source snapshot hash")
		}
		if len(p.Rules) == 0 {
			return errors.New("active protection projection rules are required")
		}
	}
	seen := make(map[string]struct{}, len(p.Rules))
	for index, rule := range p.Rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("invalid protection rule %d: %w", index, err)
		}
		key := rule.Action + "\x00" + rule.Component.Key
		if _, exists := seen[key]; exists {
			return errors.New("duplicate protection rule")
		}
		seen[key] = struct{}{}
	}
	expected, err := p.CalculateChecksum()
	if err != nil {
		return err
	}
	if p.Checksum != expected {
		return errors.New("protection projection checksum mismatch")
	}
	return nil
}

func (r ResourceReference) Validate() error {
	if strings.TrimSpace(r.OwnerModule) == "" || strings.TrimSpace(r.ResourceType) == "" || strings.TrimSpace(r.ResourceIdentity) == "" {
		return errors.New("invalid professional resource reference")
	}
	return nil
}

func (r Rule) Validate() error {
	if strings.TrimSpace(r.Action) == "" || strings.TrimSpace(r.Component.Key) == "" || len(r.Component.Path) == 0 {
		return errors.New("rule action, component key and path are required")
	}
	if strings.TrimSpace(r.Component.ValueType) == "" || !strings.HasPrefix(r.Component.SchemaFingerprint, "sha256:") {
		return errors.New("component value type and schema fingerprint are required")
	}
	for _, segment := range r.Component.Path {
		if strings.TrimSpace(segment.Name) == "" || (segment.Container != "object" && segment.Container != "array" && segment.Container != "scalar") {
			return errors.New("invalid component path")
		}
	}
	if r.Component.Path[len(r.Component.Path)-1].Container != "scalar" {
		return errors.New("component path must end at a scalar")
	}
	if !validEffect(r.Decision.Effect) {
		return errors.New("invalid protection effect")
	}
	if r.Decision.Effect == EffectMask {
		if r.Decision.Algorithm != AlgorithmKeepPrefixSuffixV1 && r.Decision.Algorithm != AlgorithmPhoneOccurrencesV1 {
			return errors.New("unsupported masking algorithm")
		}
		if err := validateKeepPrefixSuffixParameters(r.Decision.Parameters); err != nil {
			return err
		}
	}
	if r.Decision.Effect != EffectMask && r.Decision.Algorithm != "" {
		return errors.New("algorithm is only valid for mask effect")
	}
	if r.Decision.InvalidValueEffect != "" {
		if (r.Decision.InvalidValueEffect != EffectSuppress && r.Decision.InvalidValueEffect != EffectDeny) || effectRank(r.Decision.InvalidValueEffect) < effectRank(r.Decision.Effect) {
			return errors.New("invalid value effect must be at least as strict as the primary effect")
		}
	}
	return nil
}

func (p Projection) CalculateChecksum() (string, error) {
	p.Checksum = ""
	payload, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal protection projection checksum payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (p *Projection) Seal() error {
	if p == nil {
		return errors.New("protection projection is nil")
	}
	checksum, err := p.CalculateChecksum()
	if err != nil {
		return err
	}
	p.Checksum = checksum
	return nil
}

func validEffect(effect string) bool {
	return effect == EffectAllow || effect == EffectMask || effect == EffectSuppress || effect == EffectDeny
}

func effectRank(effect string) int {
	switch effect {
	case EffectAllow:
		return 0
	case EffectMask:
		return 1
	case EffectSuppress:
		return 2
	case EffectDeny:
		return 3
	default:
		return -1
	}
}
