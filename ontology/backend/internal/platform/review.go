package platform

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Review is release evidence, not an editable definition or a live capability
// observation. All references are frozen together with the semantic context.
type Review struct {
	Method   string          `json:"method"`
	Scope    string          `json:"scope"`
	Sources  []Source        `json:"sources"`
	Bindings []SourceBinding `json:"bindings"`
	Coverage []Coverage      `json:"coverage"`
}

type Source struct {
	ID     string            `json:"id"`
	Owner  string            `json:"owner"`
	Kind   string            `json:"kind"`
	Path   string            `json:"path"`
	Anchor string            `json:"anchor"`
	Commit string            `json:"commit"`
	SHA256 string            `json:"sha256"`
	Use    map[string]string `json:"use"`
}

type SourceBinding struct {
	Subject string   `json:"subject"`
	Sources []string `json:"sources"`
}

type Coverage struct {
	ID      string            `json:"id"`
	State   string            `json:"state"`
	Summary map[string]string `json:"summary"`
	Sources []string          `json:"sources"`
}

var contentHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var gitCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Review returns independent evidence from the stored release. It never opens
// repository files; only the static gate checks current source drift.
func (s *Snapshot) Review() (Review, error) {
	if s == nil || s.canonical == "" {
		return Review{}, fmt.Errorf("invalid_platform_snapshot")
	}
	var stored envelope
	if err := json.Unmarshal([]byte(s.canonical), &stored); err != nil {
		return Review{}, err
	}
	return stored.Review, nil
}

// ReviewSubjects is the exact member set requiring evidence in a release.
// Stable semantic identities, not array offsets or display labels, bind sources.
func ReviewSubjects(d Context) []string {
	result := []string{"operation"}
	for _, c := range d.Concepts {
		result = append(result, "concept/"+c.ID)
	}
	for _, r := range d.Relations {
		result = append(result, "relation/"+r.From+"/"+r.Kind+"/"+r.To)
	}
	for _, v := range d.Operation.InputsRequired {
		result = append(result, "condition/"+v)
	}
	for _, v := range d.Operation.Effects {
		result = append(result, "effect/"+v)
	}
	for _, v := range d.Operation.ExcludedEffects {
		result = append(result, "excluded_effect/"+v)
	}
	for _, r := range d.Requirements {
		result = append(result, "requirement/"+r.ID)
	}
	return result
}

func validateReview(d Context, r Review) error {
	if r.Method != "curated" || r.Scope != "selected_capability" || len(r.Sources) == 0 || len(r.Sources) > 32 ||
		len(r.Bindings) == 0 || len(r.Bindings) > 256 || len(r.Coverage) == 0 || len(r.Coverage) > 32 {
		return fmt.Errorf("invalid_platform_review")
	}
	sources := map[string]bool{}
	kinds := map[string]bool{}
	for _, source := range r.Sources {
		if !identifier.MatchString(source.ID) || sources[source.ID] || !skillName.MatchString(source.Owner) ||
			!validSourceKind(source.Kind) || !validSourcePath(source.Path) || !validText(source.Anchor) ||
			!gitCommit.MatchString(source.Commit) || !contentHash.MatchString(source.SHA256) || !bilingual(source.Use) {
			return fmt.Errorf("invalid_platform_source")
		}
		sources[source.ID], kinds[source.Kind] = true, true
	}
	for _, kind := range []string{"normative", "contract", "implementation", "test"} {
		if !kinds[kind] {
			return fmt.Errorf("incomplete_platform_source_kinds")
		}
	}
	validRefs := func(refs []string) bool {
		return uniqueIDs(refs, func(id string) bool { return sources[id] })
	}
	missing := map[string]bool{}
	for _, subject := range ReviewSubjects(d) {
		missing[subject] = true
	}
	for _, binding := range r.Bindings {
		if !missing[binding.Subject] || !validRefs(binding.Sources) {
			return fmt.Errorf("invalid_platform_source_binding")
		}
		delete(missing, binding.Subject)
	}
	if len(missing) != 0 {
		return fmt.Errorf("incomplete_platform_source_bindings")
	}
	coverage := map[string]bool{}
	modeled := 0
	for _, entry := range r.Coverage {
		if !qualifiedID(entry.ID) || coverage[entry.ID] || !bilingual(entry.Summary) || !validRefs(entry.Sources) {
			return fmt.Errorf("invalid_platform_coverage")
		}
		coverage[entry.ID] = true
		switch entry.State {
		case "modeled":
			if entry.ID != d.Capability {
				return fmt.Errorf("unbound_platform_coverage")
			}
			modeled++
		case "not_modeled":
			if entry.ID == d.Capability {
				return fmt.Errorf("conflicting_platform_coverage")
			}
		default:
			return fmt.Errorf("invalid_platform_coverage_state")
		}
	}
	if modeled != 1 {
		return fmt.Errorf("missing_platform_modeled_coverage")
	}
	return nil
}

func bilingual(value map[string]string) bool {
	return len(value) == 2 && validText(value["en"]) && validText(value["zh-cn"])
}

func validSourceKind(kind string) bool {
	return kind == "normative" || kind == "contract" || kind == "implementation" || kind == "test" || kind == "procedure"
}

func validSourcePath(value string) bool {
	return validText(value) && path.Clean(value) == value && !path.IsAbs(value) &&
		value != "." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00\r\n")
}
