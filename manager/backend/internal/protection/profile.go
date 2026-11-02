package protection

import (
	"strings"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/dataprofile"
)

// ProtectProfile applies Manager's stable aggregate-result semantics. A
// suppressed component is removed as a whole so no metric can retain source
// values. Rules match the complete source schema, never the filtered result.
// This operation is idempotent and does not mutate its input. Callers must
// still validate the current projection and source read.
func ProtectProfile(profile *dataprofile.Profile, rules []dataprotection.Rule, sourceFields []datatype.FieldInfo) (*dataprofile.Profile, error) {
	if len(rules) == 0 {
		return profile, nil
	}
	if err := ValidateProfileRules(rules); err != nil {
		return nil, err
	}
	suppressedProfiles := make(map[string]struct{}, len(rules)*2)
	for _, rule := range rules {
		fingerprint, err := dataprotection.ComponentSchemaFingerprint(sourceFields, rule.Component)
		if err != nil || fingerprint != rule.Component.SchemaFingerprint {
			return nil, ErrRequired
		}
		suppressedProfiles[rule.Component.Key] = struct{}{}
		path := make([]string, 0, len(rule.Component.Path))
		for index, segment := range rule.Component.Path {
			path = append(path, segment.Name)
			if index < len(rule.Component.Path)-1 {
				suppressedProfiles[strings.Join(path, ".")] = struct{}{}
			}
		}
		if strings.Join(path, ".") != rule.Component.Key {
			return nil, ErrRequired
		}
	}
	if profile == nil {
		return nil, nil
	}

	protected := *profile
	protected.Fields = make([]dataprofile.FieldProfile, 0, len(profile.Fields))
	for _, field := range profile.Fields {
		if _, remove := suppressedProfiles[field.Name]; remove {
			continue
		}
		protected.Fields = append(protected.Fields, field)
	}
	protected.Observations = make([]dataprofile.Observation, 0, len(profile.Observations))
	for _, observation := range profile.Observations {
		if _, remove := suppressedProfiles[observation.Field]; remove {
			continue
		}
		protected.Observations = append(protected.Observations, observation)
	}
	protected.FieldCount = len(protected.Fields)
	return &protected, nil
}

// ValidateProfileRules rejects deny and every effect not implemented by the
// aggregate-result executor before source sampling or execution creation.
func ValidateProfileRules(rules []dataprotection.Rule) error {
	for _, rule := range rules {
		if rule.Action != ActionProfile || strings.TrimSpace(rule.Component.Key) == "" || rule.Decision.Effect != dataprotection.EffectSuppress {
			return ErrRequired
		}
	}
	return nil
}
