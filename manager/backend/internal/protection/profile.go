package protection

import (
	"strings"

	"github.com/addp/common/dataprotection"
	"github.com/addp/manager/internal/dataprofile"
)

// ProtectProfile applies Manager's stable aggregate-result semantics. A
// suppressed component is removed as a whole so no metric can retain source
// values. The input profile is not mutated.
func ProtectProfile(profile *dataprofile.Profile, rules []dataprotection.Rule) (*dataprofile.Profile, error) {
	if len(rules) == 0 {
		return profile, nil
	}
	if err := ValidateProfileRules(rules); err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, nil
	}
	suppressedComponents := make(map[string]struct{}, len(rules))
	suppressedProfiles := make(map[string]struct{}, len(rules)*2)
	for _, rule := range rules {
		suppressedComponents[rule.Component.Key] = struct{}{}
		suppressedProfiles[rule.Component.Key] = struct{}{}
		path := make([]string, 0, len(rule.Component.Path))
		for index, segment := range rule.Component.Path {
			path = append(path, segment.Name)
			if index < len(rule.Component.Path)-1 {
				suppressedProfiles[strings.Join(path, ".")] = struct{}{}
			}
		}
	}

	protected := *profile
	protected.Fields = make([]dataprofile.FieldProfile, 0, len(profile.Fields))
	matched := make(map[string]struct{}, len(suppressedComponents))
	for _, field := range profile.Fields {
		if _, isComponent := suppressedComponents[field.Name]; isComponent {
			matched[field.Name] = struct{}{}
		}
		if _, remove := suppressedProfiles[field.Name]; remove {
			continue
		}
		protected.Fields = append(protected.Fields, field)
	}
	if len(matched) != len(suppressedComponents) {
		return nil, ErrRequired
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
