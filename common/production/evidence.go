// Package production defines producer facts. Facts are neither credentials nor
// Grant decisions; Meta owns DataItem identity and System owns authorization.
package production

import (
	"errors"
	"reflect"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

var ErrEvidence = errors.New("incomplete or inconsistent production evidence")

// Reference is stable across delivery attempts. Each independently committed
// result has its own ID; a task retry cannot manufacture a new creation origin.
type Reference struct {
	Owner       string `json:"owner"`
	ExecutionID string `json:"execution_id"`
	ResultID    string `json:"result_id"`
}

func canonicalUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (r Reference) Validate() error {
	if !ownerName.MatchString(r.Owner) || !canonicalUUID(r.ExecutionID) || !canonicalUUID(r.ResultID) {
		return ErrEvidence
	}
	return nil
}

type UserOrigin struct {
	PrincipalID          int64 `json:"principal_id,string"`
	MembershipID         int64 `json:"membership_id,string"`
	AuthorizationVersion int64 `json:"authorization_version,string"`
}

// SourceEvidence distinguishes a proven empty business read set from absent
// evidence. Private input proof remains the responsibility of its native owner.
type SourceEvidence struct {
	Kind    string               `json:"kind"`
	ReadSet *plugin.QueryReadSet `json:"read_set,omitempty"`
}

const (
	SourcesUnknown      = "unknown"
	SourcesDataItems    = "data_items"
	SourcesNone         = "source_free"
	SourcesPrivateInput = "private_input"
)

func (s SourceEvidence) Validate() error {
	if s.Kind != SourcesDataItems {
		if s.ReadSet != nil || (s.Kind != SourcesUnknown && s.Kind != SourcesNone && s.Kind != SourcesPrivateInput) {
			return ErrEvidence
		}
		return nil
	}
	if s.ReadSet == nil || len(s.ReadSet.Paths) == 0 || len(s.ReadSet.Paths) > 200 {
		return ErrEvidence
	}
	normalized, err := plugin.NewQueryReadSet(s.ReadSet.Paths...)
	if err != nil || !reflect.DeepEqual(normalized, s.ReadSet) {
		return ErrEvidence
	}
	return nil
}

type CommittedObject struct {
	Target  plugin.EngineCatalogPath `json:"target"`
	Created bool                     `json:"created"`
}

// Receipt does not include parent status: committed results survive parent
// failure/cancellation. Operation, attempt and committed objects come from the
// producer's persisted write path, never from a scan or a caller's target list.
type Receipt struct {
	Reference   Reference                `json:"reference"`
	TenantID    int64                    `json:"tenant_id,string"`
	Attempt     int                      `json:"attempt"`
	OperationID string                   `json:"operation_id"`
	CommittedAt time.Time                `json:"committed_at"`
	Origin      *UserOrigin              `json:"origin"`
	Primary     plugin.EngineCatalogPath `json:"primary"`
	Objects     []CommittedObject        `json:"objects"`
	Sources     SourceEvidence           `json:"sources"`
}

func (r Receipt) Validate() error {
	if r.Reference.Validate() != nil || r.TenantID <= 0 || r.Attempt <= 0 || !canonicalUUID(r.OperationID) ||
		r.CommittedAt.IsZero() || len(r.Objects) == 0 || len(r.Objects) > 200 || r.Sources.Validate() != nil {
		return ErrEvidence
	}
	if r.Origin != nil && (r.Origin.PrincipalID <= 0 || r.Origin.MembershipID <= 0 || r.Origin.AuthorizationVersion <= 0) {
		return ErrEvidence
	}
	paths := make([]plugin.EngineCatalogPath, len(r.Objects))
	primaryFound := false
	for i, object := range r.Objects {
		if object.Target.EngineID != r.Primary.EngineID {
			return ErrEvidence
		}
		paths[i] = object.Target
		primaryFound = primaryFound || reflect.DeepEqual(object.Target, r.Primary)
	}
	set, err := plugin.NewQueryReadSet(paths...)
	if err != nil || len(set.Paths) != len(paths) || !primaryFound {
		return ErrEvidence
	}
	return nil
}

// ProveNewBoundary compares all committed objects against Meta's complete
// binding. Members are completeness evidence, not additional ACL objects.
// Existing logical identity/history is checked independently by Meta/System.
func (r Receipt) ProveNewBoundary(binding authorizationfacts.Binding) error {
	if r.Validate() != nil {
		return ErrEvidence
	}
	primary, err := binding.PrimaryTarget()
	if err != nil || !reflect.DeepEqual(primary, r.Primary) {
		return ErrEvidence
	}
	var members []plugin.EngineCatalogPath
	switch {
	case binding.FileComposition != nil:
		for _, ref := range binding.FileComposition.Refs {
			members = append(members, ref.Target)
		}
	case binding.Scope != nil, binding.MetadataTarget != nil:
		// Whole and metadata-only bindings require their owner-specific exact
		// composition proof; never infer creation from a container/root alone.
		return ErrEvidence
	default:
		members = []plugin.EngineCatalogPath{binding.NativeTarget}
	}
	if len(members) != len(r.Objects) {
		return ErrEvidence
	}
	for _, member := range members {
		found := false
		for _, object := range r.Objects {
			if reflect.DeepEqual(member, object.Target) {
				if !object.Created {
					return ErrEvidence
				}
				found = true
				break
			}
		}
		if !found {
			return ErrEvidence
		}
	}
	return nil
}
