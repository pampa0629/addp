package authorization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const OntologyRuleEvaluateTool = "ontology.rule.evaluate"

var ontologyRuleIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// OntologyRuleEvaluation identifies one published rule and one business object.
// System stores only its digest, never the object identifier.
type OntologyRuleEvaluation struct {
	OntologyID        string `json:"ontology_id"`
	Revision          uint64 `json:"revision"`
	Generation        string `json:"generation"`
	ActivationVersion uint64 `json:"activation_version"`
	RuleID            string `json:"rule_id"`
	ObjectID          string `json:"object_id"`
}

func (e OntologyRuleEvaluation) Digest() string {
	generation, err := uuid.Parse(e.Generation)
	if err != nil || generation == uuid.Nil || generation.String() != e.Generation ||
		!ontologyRuleIdentifier.MatchString(e.OntologyID) || !ontologyRuleIdentifier.MatchString(e.RuleID) ||
		e.Revision == 0 || e.ActivationVersion == 0 || e.ObjectID == "" || len(e.ObjectID) > 512 ||
		!utf8.ValidString(e.ObjectID) || strings.TrimSpace(e.ObjectID) != e.ObjectID || strings.ContainsRune(e.ObjectID, 0) {
		return ""
	}
	data, _ := json.Marshal(e)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// OntologyRuleCall is passed only between the authenticated owner services.
type OntologyRuleCall struct {
	AgentRunID  string                 `json:"agent_run_id"`
	ToolCallID  string                 `json:"tool_call_id"`
	PrincipalID string                 `json:"principal_id"`
	Evaluation  OntologyRuleEvaluation `json:"evaluation"`
}

// OntologyRuleFactRequest selects the private QueryService frozen by Ontology.
type OntologyRuleFactRequest struct {
	Call           OntologyRuleCall `json:"call"`
	ServiceID      uint64           `json:"service_id"`
	ServiceVersion string           `json:"service_version"`
	IDParameter    string           `json:"id_parameter"`
	Fields         []string         `json:"fields"`
}
