package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
)

// MetadataRefreshRange describes a maximum source range. It is not an access
// verdict: the execution must recheck current qualification before source IO.
// A range is either an ordinary node or one complete DataItem boundary.
type MetadataRefreshRange struct {
	Node *SourceNodeScope            `json:"node,omitempty"`
	Item *authorizationfacts.Binding `json:"item,omitempty"`
}

const MaxMetadataRefreshRanges = 200
const MaxMetadataRefreshScopeBytes = 512 << 10

type MetadataRefreshQuery struct {
	Target plugin.EngineCatalogPath `json:"target"`
	Kind   string                   `json:"kind" enums:"node,item,engine"`
}

func (q MetadataRefreshQuery) Validate() error {
	if q.Kind == "engine" && authorizationfacts.ValidNodeIdentityTarget(q.Target) && len(q.Target.Segments) == 1 {
		return nil
	}
	if q.Kind == "node" && authorizationfacts.ValidNodeIdentityTarget(q.Target) && len(q.Target.Segments) > 1 {
		return nil
	}
	if q.Kind == "item" {
		if _, err := authorizationfacts.PrimaryCandidateFingerprint(q.Target); err == nil {
			return nil
		}
	}
	return errors.New("invalid metadata refresh query")
}

// MetadataRefreshScope is a request-scoped observation to freeze into a scan,
// not a token. Each source read must revalidate the original execution.
type MetadataRefreshScope struct {
	TenantID      int64                      `json:"tenant_id,string" swaggertype:"string"`
	EngineVersion string                     `json:"engine_version"`
	Query         MetadataRefreshQuery       `json:"query"`
	Operator      SharingFulfillmentOperator `json:"operator"`
	Ranges        []MetadataRefreshRange     `json:"ranges"`
	ObservedAt    time.Time                  `json:"observed_at"`
}

func (s MetadataRefreshScope) Validate() error {
	version, err := strconv.ParseInt(s.EngineVersion, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != s.EngineVersion || s.TenantID <= 0 ||
		s.Operator.PrincipalID <= 0 || s.Operator.MembershipID <= 0 || s.Operator.AuthorizationVersion <= 0 ||
		s.ObservedAt.IsZero() || s.Query.Validate() != nil || len(s.Ranges) == 0 || len(s.Ranges) > MaxMetadataRefreshRanges {
		return errors.New("invalid metadata refresh observation")
	}
	for _, r := range s.Ranges {
		if err := r.Validate(s.Query.Target.EngineID); err != nil {
			return err
		}
	}
	if s.Query.Kind == "item" {
		if len(s.Ranges) != 1 || s.Ranges[0].Item == nil {
			return errors.New("exact refresh expanded into a node")
		}
		primary, _ := s.Ranges[0].Item.PrimaryTarget()
		if !reflect.DeepEqual(primary, s.Query.Target) {
			return errors.New("exact refresh target changed")
		}
	} else if s.Query.Kind == "engine" {
		if len(s.Ranges) != 1 || s.Ranges[0].Node == nil || !s.Ranges[0].Node.Recursive || !reflect.DeepEqual(s.Ranges[0].Node.Target, s.Query.Target) {
			return errors.New("invalid centralized engine scope")
		}
	} else {
		canonical, err := IntersectMetadataRefreshScope(s.Query.Target, s.Ranges)
		if err != nil || !reflect.DeepEqual(canonical, s.Ranges) {
			return errors.New("noncanonical metadata refresh range")
		}
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > MaxMetadataRefreshScopeBytes {
		return errors.New("metadata refresh observation exceeds budget")
	}
	return nil
}

func (s *MetadataRefreshScope) UnmarshalJSON(raw []byte) error {
	if _, err := jsonmap.DecodeObjectStrict(raw, MaxMetadataRefreshScopeBytes, 64); err != nil {
		return err
	}
	type plain MetadataRefreshScope
	var result plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || MetadataRefreshScope(result).Validate() != nil {
		return errors.New("invalid strict metadata refresh observation")
	}
	*s = MetadataRefreshScope(result)
	return nil
}

func (r MetadataRefreshRange) Validate(engineID uint) error {
	invalid := errors.New("invalid metadata refresh range")
	if (r.Node == nil) == (r.Item == nil) {
		return invalid
	}
	if r.Node != nil {
		if r.Node.Validate() != nil || r.Node.Target.EngineID != engineID {
			return invalid
		}
		return nil
	}
	path, err := r.Item.PrimaryTarget()
	if err != nil || path.EngineID != engineID {
		return invalid
	}
	return nil
}

// CoversBinding checks every explicit dependency, not just the primary file.
// A whole DataItem remains one directory-root boundary, not an ordinary node.
func (s SourceNodeScope) CoversBinding(binding authorizationfacts.Binding) bool {
	primary, err := binding.PrimaryTarget()
	if err != nil || !s.Covers(primary) {
		return false
	}
	if binding.FileComposition != nil {
		for _, ref := range binding.FileComposition.Refs {
			if !s.Covers(ref.Target) {
				return false
			}
		}
	}
	return true
}

func sameCatalogPrefix(parent, child plugin.EngineCatalogPath) bool {
	if parent.EngineID != child.EngineID || parent.Version != child.Version || len(parent.Segments) > len(child.Segments) {
		return false
	}
	for i, part := range parent.Segments {
		if child.Segments[i] != part {
			return false
		}
	}
	return true
}

// CoversNode admits the node's own enumeration only when a current node Grant
// covers it. A set of precise item Grants can never justify parent enumeration.
func (s SourceNodeScope) CoversNode(node SourceNodeScope) bool {
	return s.Validate() == nil && node.Validate() == nil && sameCatalogPrefix(s.Target, node.Target) &&
		(s.Recursive || (reflect.DeepEqual(s.Target, node.Target) && !node.Recursive))
}

// IntersectMetadataRefreshScope clips trusted live Grant ranges to a clicked
// ordinary node. No Provider/Meta enumeration is needed to discover this scope.
// An empty result is a valid denial; exceeding the existing scan-plan budget
// fails rather than silently returning a partial plan.
func IntersectMetadataRefreshScope(target plugin.EngineCatalogPath, grants []MetadataRefreshRange) ([]MetadataRefreshRange, error) {
	if !authorizationfacts.ValidNodeIdentityTarget(target) || len(target.Segments) == 1 {
		return nil, errors.New("metadata refresh requires an ordinary non-root node")
	}
	ranges := map[string]MetadataRefreshRange{}
	for _, grant := range grants {
		if err := grant.Validate(target.EngineID); err != nil {
			return nil, err
		}
		var clipped MetadataRefreshRange
		if grant.Node != nil {
			node := *grant.Node
			switch {
			case sameCatalogPrefix(target, node.Target):
				clipped.Node = &node
			case node.Recursive && sameCatalogPrefix(node.Target, target):
				node.Target = target
				clipped.Node = &node
			default:
				continue
			}
		} else if (SourceNodeScope{Target: target, Recursive: true}).CoversBinding(*grant.Item) {
			clipped.Item = grant.Item
		} else {
			continue
		}
		raw, err := json.Marshal(clipped)
		if err != nil {
			return nil, err
		}
		// Copy through the strict binding decoder to detach caller-owned slices.
		var copy MetadataRefreshRange
		if json.Unmarshal(raw, &copy) != nil {
			return nil, errors.New("invalid metadata refresh boundary")
		}
		ranges[string(raw)] = copy
		if len(ranges) > MaxMetadataRefreshRanges {
			return nil, errors.New("metadata refresh scope exceeds scan-plan budget")
		}
	}
	keys := make([]string, 0, len(ranges))
	for key := range ranges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare([]byte(keys[i]), []byte(keys[j])) < 0 })
	result := make([]MetadataRefreshRange, 0, len(keys))
	for _, key := range keys {
		result = append(result, ranges[key])
	}
	return result, nil
}

// MetadataRefreshScopeStillCovered never adds newly granted targets to a frozen
// execution. Revocation/expiry must be reflected in live before this call.
func MetadataRefreshScopeStillCovered(frozen, live []MetadataRefreshRange, engineID uint) bool {
	if len(frozen) == 0 || len(frozen) > MaxMetadataRefreshRanges || len(live) > MaxMetadataRefreshRanges {
		return false
	}
	for _, list := range [][]MetadataRefreshRange{frozen, live} {
		for _, r := range list {
			if r.Validate(engineID) != nil {
				return false
			}
		}
	}
	for _, needed := range frozen {
		covered := false
		for _, available := range live {
			if available.Node != nil {
				covered = (needed.Node != nil && available.Node.CoversNode(*needed.Node)) ||
					(needed.Item != nil && available.Node.CoversBinding(*needed.Item))
			} else {
				covered = needed.Item != nil && reflect.DeepEqual(needed.Item, available.Item)
			}
			if covered {
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
