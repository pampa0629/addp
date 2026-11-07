package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/authorization"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

type ServiceFactReader struct {
	baseURL string
	client  http.Client
	tokens  ontologyFactServiceTokens
}

type ontologyFactServiceTokens interface {
	TenantServiceAccessToken(context.Context, uint) (string, error)
}

type ontologyRuleCallKey struct{}

func WithOntologyRuleCall(ctx context.Context, call authorization.OntologyRuleCall) context.Context {
	return context.WithValue(ctx, ontologyRuleCallKey{}, call)
}

func (r *ServiceFactReader) SetServiceTokens(tokens ontologyFactServiceTokens) { r.tokens = tokens }

var ErrOntologyFactPermission = errors.New("ontology fact permission denied")
var ErrOntologyFactUnavailable = errors.New("ontology fact transport unavailable")

func NewServiceFactReader(endpoint string) (*ServiceFactReader, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid Service owner URL")
	}
	return &ServiceFactReader{baseURL: strings.TrimSuffix(u.String(), "/"), client: http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type BusinessEvaluation struct {
	SemanticBinding
	ServiceID       uint64            `json:"service_id"`
	ServiceVersion  string            `json:"service_version"`
	ObservationDate string            `json:"observation_date,omitempty"`
	Decision        semantic.Decision `json:"decision"`
}

func (s *RevisionService) EvaluateBusiness(ctx context.Context, actor models.Actor, id, ruleID string, revision uint64, generation string, activation uint64, objectID, bearer string) (*BusinessEvaluation, error) {
	call, delegated := ctx.Value(ontologyRuleCallKey{}).(authorization.OntologyRuleCall)
	if objectID == "" || len(objectID) > 512 || strings.TrimSpace(objectID) != objectID || !utf8.ValidString(objectID) || (bearer == "") != delegated {
		return nil, repository.ErrInvalid
	}
	snapshot, active, err := s.activeSnapshot(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if active.Revision != revision || active.Generation != generation || active.ActivationVersion != activation {
		return nil, ErrActivationChanged
	}
	binding, ok := snapshot.RuleServiceBinding(ruleID)
	if !ok {
		return nil, repository.ErrNotFound
	}
	observationDate := ""
	if binding.TimeZone != "" {
		zone, err := time.LoadLocation(binding.TimeZone)
		if err != nil {
			return nil, repository.ErrIntegrity
		}
		observationDate = time.Now().In(zone).Format(time.DateOnly)
	}
	if delegated && call.Evaluation != (authorization.OntologyRuleEvaluation{OntologyID: id, RuleID: ruleID, Revision: revision,
		Generation: generation, ActivationVersion: activation, ObjectID: objectID}) {
		return nil, repository.ErrInvalid
	}
	facts, err := s.serviceFacts.read(ctx, actor.TenantID, binding, objectID, bearer, observationDate, call, delegated)
	if err != nil {
		return nil, err
	}
	decision := snapshot.EvaluateServiceFacts(ctx, ruleID, facts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, current, err := s.activeSnapshot(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if current != active {
		return nil, ErrActivationChanged
	}
	result := &BusinessEvaluation{SemanticBinding: active, ServiceID: binding.ServiceID, ServiceVersion: binding.ServiceVersion,
		ObservationDate: observationDate, Decision: decision}
	return result, boundSemanticResult(ctx, result)
}

func (r *ServiceFactReader) read(ctx context.Context, tenant uint64, binding semantic.ServiceBinding, objectID, bearer, observationDate string, call authorization.OntologyRuleCall, delegated bool) (map[string]semantic.Fact, error) {
	if r == nil {
		if delegated {
			return nil, ErrOntologyFactUnavailable
		}
		return nil, nil
	}
	fields := []string{binding.IDField}
	for _, input := range binding.Inputs {
		fields = append(fields, input.Field)
	}
	request := struct {
		Parameters             map[string]string `json:"parameters"`
		Select                 []string          `json:"select"`
		Page                   map[string]int    `json:"page"`
		RequireUntransformed   bool              `json:"require_untransformed"`
		ExpectedServiceID      uint64            `json:"expected_service_id"`
		ExpectedServiceVersion string            `json:"expected_service_version"`
	}{map[string]string{binding.IDParameter: objectID}, fields, map[string]int{"limit": 2}, true, binding.ServiceID, binding.ServiceVersion}
	var payload any = request
	path := "/api/query/" + url.PathEscape(binding.ServiceName) + "/query"
	if delegated {
		if r.tokens == nil {
			return nil, ErrOntologyFactUnavailable
		}
		credential, err := r.tokens.TenantServiceAccessToken(ctx, uint(tenant))
		if err != nil || credential == "" {
			return nil, ErrOntologyFactUnavailable
		}
		bearer = credential
		path = "/api/v1/service/runtime/ontology-facts/" + url.PathEscape(binding.ServiceName)
		payload = authorization.OntologyRuleFactRequest{Call: call, ServiceID: binding.ServiceID, ServiceVersion: binding.ServiceVersion,
			IDParameter: binding.IDParameter, Fields: fields}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrOntologyFactUnavailable
	}
	// A Service timeout is an unknown fact result. Leave the outer request
	// enough time for the authoritative activation recheck.
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(queryCtx, http.MethodPost, r.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, ErrOntologyFactUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(req)
	if err != nil {
		if delegated {
			return nil, ErrOntologyFactUnavailable
		}
		return nil, nil
	}
	defer response.Body.Close()
	if delegated && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized) {
		return nil, ErrOntologyFactPermission
	}
	if delegated && response.StatusCode != http.StatusOK {
		return nil, ErrOntologyFactUnavailable
	}
	format, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || format != "application/json" {
		return nil, nil
	}
	const maxResponse = 128 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return nil, nil
	}
	var result struct {
		FactStatus     string `json:"fact_status"`
		ServiceVersion string `json:"service_version"`
		Page           struct {
			Limit      int    `json:"limit"`
			HasMore    *bool  `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
		Data []map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || result.FactStatus != "verified" ||
		result.ServiceVersion != binding.ServiceVersion || result.Page.Limit < 1 || result.Page.Limit > 2 ||
		result.Page.HasMore == nil || *result.Page.HasMore || result.Page.NextCursor != "" || len(result.Data) != 1 {
		return nil, nil
	}
	row := result.Data[0]
	var returnedID string
	if json.Unmarshal(row[binding.IDField], &returnedID) != nil || returnedID != objectID {
		return nil, nil
	}
	facts := make(map[string]semantic.Fact, len(binding.Inputs))
	for _, input := range binding.Inputs {
		value, present := row[input.Field]
		if !present {
			return nil, nil
		}
		facts[input.Variable] = serviceFact(value, input.Conversion, observationDate)
	}
	return facts, nil
}

func serviceFact(raw json.RawMessage, conversion, observationDate string) semantic.Fact {
	if bytes.Equal(raw, []byte("null")) {
		return semantic.Fact{State: semantic.Absent}
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return semantic.Fact{State: semantic.Invalid}
	}
	if conversion == semantic.ServiceString {
		return semantic.Fact{State: semantic.Known, Value: value}
	}
	if len(value) == len(time.DateOnly)+len("T00:00:00Z") && strings.HasSuffix(value, "T00:00:00Z") {
		value = strings.TrimSuffix(value, "T00:00:00Z")
	}
	date, err := time.Parse(time.DateOnly, value)
	if err != nil || date.Format(time.DateOnly) != value || observationDate == "" {
		return semantic.Fact{State: semantic.Invalid}
	}
	return semantic.Fact{State: semantic.Known, Value: value <= observationDate}
}
