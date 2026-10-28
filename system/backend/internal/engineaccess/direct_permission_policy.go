package engineaccess

import (
	"errors"
	"sort"

	"github.com/addp/common/dataitem/authorizationfacts"
)

// A permission decision concerns new direct issuance only. It is not a Grant,
// a Catalog business approval, or a read-access decision.
type directPermissionDecision struct {
	State   string
	Source  string
	Version int64
}

type directPermissionSetting struct {
	Allowed bool
	Version int64
}

type catalogDeploymentDeclaration struct {
	Enabled bool
	Version int64
}

const (
	directPermissionAllowed           = "allowed"
	directPermissionDenied            = "denied"
	directPermissionUnknown           = "unknown"
	directPermissionExplicit          = "explicit"
	directPermissionDeployment        = "deployment_default"
	directPermissionDeploymentUnknown = "deployment_unknown"
)

var errDirectPermissionObservation = errors.New("invalid direct authorization permission observation")

// nil deployment means no persistent declaration. A failed read must be
// returned by the repository before calling this function, never encoded as nil.
// Runtime/heartbeat/URL/Ready values deliberately have no place in this input.
func resolveDirectPermission(setting *directPermissionSetting, deployment *catalogDeploymentDeclaration) (directPermissionDecision, error) {
	if setting != nil {
		if setting.Version <= 0 {
			return directPermissionDecision{}, errDirectPermissionObservation
		}
		state := directPermissionDenied
		if setting.Allowed {
			state = directPermissionAllowed
		}
		return directPermissionDecision{State: state, Source: directPermissionExplicit, Version: setting.Version}, nil
	}
	if deployment == nil {
		return directPermissionDecision{State: directPermissionUnknown, Source: directPermissionDeploymentUnknown}, nil
	}
	if deployment.Version <= 0 {
		return directPermissionDecision{}, errDirectPermissionObservation
	}
	state := directPermissionDenied
	if !deployment.Enabled {
		state = directPermissionAllowed
	}
	return directPermissionDecision{State: state, Source: directPermissionDeployment, Version: deployment.Version}, nil
}

// Build the identity predicate before Meta counts and paginates. Only explicit
// exceptions travel to Meta; the deployment default is never materialized as
// one System record per DataItem. Metadata content is not consulted here.
func directPermissionIdentityQuery(engineID uint, search, state string, page, size int,
	settings map[string]directPermissionSetting, deployment *catalogDeploymentDeclaration,
) (authorizationfacts.IdentityQuery, error) {
	query := authorizationfacts.IdentityQuery{EngineID: engineID, Search: search, Page: page, PageSize: size}
	if state != "" && state != directPermissionAllowed && state != directPermissionDenied && state != directPermissionUnknown {
		return query, errDirectPermissionObservation
	}
	defaultDecision, err := resolveDirectPermission(nil, deployment)
	if err != nil {
		return query, err
	}
	// Validate even an unfiltered observation. A malformed local fact must not
	// silently disappear from the supposedly complete effective-permission list.
	for fingerprint, setting := range settings {
		if !authorizationfacts.ValidItemFingerprint(fingerprint) || setting.Version <= 0 {
			return query, errDirectPermissionObservation
		}
	}
	if state != "" {
		matchesDefault := state == defaultDecision.State
		query.FingerprintFilter = "include"
		if matchesDefault {
			query.FingerprintFilter = "exclude"
		}
		query.Fingerprints = make([]string, 0)
		for fingerprint, setting := range settings {
			decision, err := resolveDirectPermission(&setting, deployment)
			if err != nil {
				return query, err
			}
			if (decision.State == state) != matchesDefault {
				query.Fingerprints = append(query.Fingerprints, fingerprint)
			}
		}
		sort.Strings(query.Fingerprints)
	}
	if err := query.Validate(); err != nil {
		return query, err
	}
	return query, nil
}
