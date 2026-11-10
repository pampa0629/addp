package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
)

const DevelopFileExecutionSchema = "develop.geopackage-file-execution/v1"

// The projection carries three separately bounded values (raw config, Intent,
// optional sealed Scope), plus a fixed envelope of canonical IDs and digests.
const MaxDevelopFileExecutionFactsBytes = 3*execution.MaxExecutionConfigBytes + 4<<10

type DevelopFileExecutionActor struct {
	PrincipalID          string `json:"principal_id"`
	TenantMembershipID   string `json:"tenant_membership_id"`
	AuthorizationVersion string `json:"authorization_version"`
}

// DevelopFilePublicationFacts describes the Owner's original operation. A
// missing preparation digest cannot authorize final publication.
type DevelopFilePublicationFacts struct {
	ApprovalID        string                           `json:"approval_id"`
	Request           plugin.NewFilePublicationRequest `json:"request"`
	PreparationDigest string                           `json:"preparation_digest,omitempty"`
}

// DevelopFileExecutionFacts is a private immutable Owner projection. Common
// execution lifecycle and lease facts remain authoritative in the shared store;
// this wire value contains no lease secret, connection or permission decision.
type DevelopFileExecutionFacts struct {
	SchemaVersion   string                           `json:"schema_version"`
	ExecutionID     string                           `json:"execution_id"`
	TenantID        string                           `json:"tenant_id"`
	Actor           DevelopFileExecutionActor        `json:"actor"`
	ConfigJSON      json.RawMessage                  `json:"config_json"`
	Intent          execution.DevelopFileIntent      `json:"intent"`
	AuthorizationID string                           `json:"authorization_id,omitempty"`
	BoundScope      *execution.DevelopBoundFileScope `json:"bound_scope,omitempty"`
	ScopeDigest     string                           `json:"scope_digest,omitempty"`
	AdmissionState  string                           `json:"admission_state"`
	Publication     *DevelopFilePublicationFacts     `json:"publication,omitempty"`
}

func (f *DevelopFileExecutionFacts) Validate() error {
	if f == nil || f.SchemaVersion != DevelopFileExecutionSchema || !canonicalExecutionUUID(f.ExecutionID) {
		return errors.New("invalid Develop file execution identity")
	}
	for _, id := range []string{f.TenantID, f.Actor.PrincipalID, f.Actor.TenantMembershipID, f.Actor.AuthorizationVersion} {
		if _, err := parseCanonicalPositiveID(id); err != nil {
			return errors.New("invalid Develop file execution actor")
		}
	}
	intent, err := execution.NewDevelopFileIntent(f.ConfigJSON, f.Intent.ExpectedAttempt, f.Intent.TimeoutSeconds, f.Intent.Sources, f.Intent.Target)
	if err != nil || !reflect.DeepEqual(intent, &f.Intent) {
		return errors.New("invalid frozen Develop file intent")
	}
	if f.AdmissionState != "pending" && f.AdmissionState != "ready" && f.AdmissionState != "rejected" {
		return errors.New("invalid Develop file admission state")
	}
	if f.AuthorizationID == "" {
		if f.BoundScope != nil || f.ScopeDigest != "" || (f.AdmissionState != "pending" && f.AdmissionState != "rejected") {
			return errors.New("unissued Develop file scope")
		}
	} else {
		if _, err := parseCanonicalPositiveID(f.AuthorizationID); err != nil || f.BoundScope.ValidateAgainstIntent(f.Intent) != nil {
			return errors.New("invalid bound Develop file execution")
		}
		digest, err := f.BoundScope.Digest()
		if err != nil || digest != f.ScopeDigest {
			return errors.New("invalid bound Develop file digest")
		}
	}
	if p := f.Publication; p != nil {
		digest, err := hex.DecodeString(p.PreparationDigest)
		if f.AdmissionState != "ready" || f.BoundScope == nil || f.BoundScope.Target == nil ||
			!canonicalExecutionUUID(p.ApprovalID) || p.Request.Validate() != nil || p.Request.SizeBytes <= 0 ||
			!reflect.DeepEqual(p.Request.Target, f.BoundScope.Target.Target) ||
			(p.PreparationDigest != "" && (err != nil || len(digest) != sha256.Size || strings.ToLower(p.PreparationDigest) != p.PreparationDigest)) {
			return errors.New("invalid Develop file publication binding")
		}
	}
	encoded, err := json.Marshal(f)
	if err != nil || len(encoded) > MaxDevelopFileExecutionFactsBytes {
		return errors.New("Develop file execution projection exceeds wire budget")
	}
	return nil
}

func (f *DevelopFileExecutionFacts) Clone() *DevelopFileExecutionFacts {
	if f == nil {
		return nil
	}
	v := *f
	v.ConfigJSON = bytes.Clone(f.ConfigJSON)
	v.Intent = *f.Intent.Clone()
	v.BoundScope = f.BoundScope.Clone()
	if f.Publication != nil {
		publication := *f.Publication
		publication.Request.Target.Segments = append([]plugin.EngineCatalogSegment(nil), publication.Request.Target.Segments...)
		v.Publication = &publication
	}
	return &v
}

// ReadFileExecution uses one strict current Tenant Service request. No User
// token, redirects, implicit reauthentication replay, or alternative endpoint.
func (c *DevelopClient) ReadFileExecution(ctx context.Context, tenant uint, executionID string) (*DevelopFileExecutionFacts, error) {
	if c == nil || tenant == 0 || !canonicalExecutionUUID(executionID) {
		return nil, errors.New("invalid Develop file execution read")
	}
	bound := c.WithTenantID(tenant)
	var raw json.RawMessage
	if err := bound.doStrictJSON(ctx, http.MethodGet, "/api/v1/develop/runtime/file-executions/"+executionID, &raw, MaxDevelopFileExecutionFactsBytes); err != nil {
		return nil, err
	}
	if _, err := jsonmap.DecodeObjectStrict(raw, MaxDevelopFileExecutionFactsBytes, 64); err != nil {
		return nil, errors.New("invalid Develop file execution response")
	}
	var facts DevelopFileExecutionFacts
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&facts) != nil || facts.Validate() != nil || facts.ExecutionID != executionID || facts.TenantID != strconv.FormatUint(uint64(tenant), 10) {
		return nil, errors.New("invalid Develop file execution response")
	}
	return facts.Clone(), nil
}
