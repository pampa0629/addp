package execution

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/google/uuid"
)

// InternalTaskScope is an immutable owner operation, never an Engine scope.
// Only the explicitly registered semantic projection operation is supported.
type InternalTaskScope struct {
	TaskType   string `json:"task_type"`
	ResourceID string `json:"resource_id"`
	Revision   string `json:"revision"`
	Digest     string `json:"digest"`
	Generation string `json:"generation"`
}

var internalResourcePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var internalDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s InternalTaskScope) Validate(audience string) error {
	revision, err := strconv.ParseInt(s.Revision, 10, 64)
	generation, uuidErr := uuid.Parse(s.Generation)
	if audience != AudienceOntology || s.TaskType != "semantic_projection" ||
		!internalResourcePattern.MatchString(s.ResourceID) || !internalDigestPattern.MatchString(s.Digest) ||
		err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != s.Revision ||
		uuidErr != nil || generation == uuid.Nil || generation.String() != s.Generation {
		return fmt.Errorf("invalid internal task scope")
	}
	return nil
}
