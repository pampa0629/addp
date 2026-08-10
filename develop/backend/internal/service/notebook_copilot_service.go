package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

var (
	ErrNotebookCopilotInvalidRequest = errors.New("notebook copilot request is invalid")
	ErrNotebookCopilotForbidden      = errors.New("notebook copilot session is forbidden")
	ErrNotebookCopilotNoCandidates   = errors.New("notebook copilot found no authorized data candidates")
)

const (
	notebookCopilotCatalogPageSize = 1000
	notebookCopilotMaxLeaves       = 5000
	notebookCopilotMaxCandidates   = 320
)

type NotebookCopilotSelection struct {
	CandidateID string                         `json:"candidate_id"`
	Role        string                         `json:"role"`
	EngineID    uint                           `json:"engine_id"`
	Path        commonClient.EngineCatalogPath `json:"path"`
}

type NotebookCopilotRequest struct {
	Query      string                     `json:"query"`
	Kernel     string                     `json:"kernel"`
	Selections []NotebookCopilotSelection `json:"selections,omitempty"`
}

type NotebookCopilotResponse struct {
	Status              string                     `json:"status"`
	Candidates          []NotebookCopilotCandidate `json:"candidates,omitempty"`
	Code                string                     `json:"code,omitempty"`
	Explanation         string                     `json:"explanation,omitempty"`
	Warnings            []string                   `json:"warnings,omitempty"`
	ClarificationReason string                     `json:"clarification_reason,omitempty"`
	Message             string                     `json:"message,omitempty"`
}

type NotebookCopilotCandidate struct {
	CandidateID          string                         `json:"candidate_id"`
	Role                 string                         `json:"role"`
	EngineID             uint                           `json:"engine_id"`
	EngineName           string                         `json:"engine_name"`
	EngineType           string                         `json:"engine_type"`
	Name                 string                         `json:"name"`
	Term                 string                         `json:"term"`
	Kind                 string                         `json:"kind"`
	Path                 commonClient.EngineCatalogPath `json:"path"`
	PathNames            []string                       `json:"path_names"`
	Recommended          bool                           `json:"recommended,omitempty"`
	RecommendationReason string                         `json:"recommendation_reason,omitempty"`
}

type notebookCopilotIntent struct {
	Role          string   `json:"role"`
	SearchQueries []string `json:"search_queries"`
}

type notebookCopilotRemoteResponse struct {
	Status              string                     `json:"status"`
	Intents             []notebookCopilotIntent    `json:"intents"`
	Candidates          []NotebookCopilotCandidate `json:"candidates"`
	Code                string                     `json:"code"`
	Explanation         string                     `json:"explanation"`
	Warnings            []string                   `json:"warnings"`
	ClarificationReason string                     `json:"clarification_reason"`
	Message             string                     `json:"message"`
}

type NotebookCopilotService struct {
	sessions *NotebookSessionService
	baseURL  string
	client   *http.Client
}

func NewNotebookCopilotService(sessions *NotebookSessionService, baseURL string, client *http.Client) *NotebookCopilotService {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &NotebookCopilotService{sessions: sessions, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: client}
}

func (s *NotebookCopilotService) Generate(
	ctx context.Context, userToken, sessionID, secret string, tenantID, userID uint, request NotebookCopilotRequest,
) (*NotebookCopilotResponse, error) {
	request.Query = strings.TrimSpace(request.Query)
	request.Kernel = strings.TrimSpace(request.Kernel)
	if s == nil || s.sessions == nil || s.baseURL == "" || request.Query == "" || request.Kernel != "python3" {
		return nil, ErrNotebookCopilotInvalidRequest
	}
	session, err := s.sessions.Resolve(sessionID, secret)
	if err != nil {
		return nil, err
	}
	if session.TenantID != tenantID || session.UserID != userID {
		return nil, ErrNotebookCopilotForbidden
	}
	descriptors, err := s.sessions.listDataEngineDescriptorsForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	if len(request.Selections) == 0 {
		return s.discover(ctx, userToken, session, descriptors, request)
	}
	return s.generateCode(ctx, userToken, session, descriptors, request)
}

func (s *NotebookCopilotService) discover(
	ctx context.Context, userToken string, session *NotebookSession, descriptors []commonModels.EngineRuntimeDescriptor, request NotebookCopilotRequest,
) (*NotebookCopilotResponse, error) {
	var intentResponse notebookCopilotRemoteResponse
	if err := s.callCopilot(ctx, userToken, map[string]interface{}{
		"query": request.Query, "kernel": request.Kernel, "candidates": []interface{}{}, "resources": []interface{}{},
	}, &intentResponse); err != nil {
		return nil, err
	}
	if intentResponse.Status != "intents_ready" || len(intentResponse.Intents) == 0 {
		return &NotebookCopilotResponse{
			Status: "need_clarification", ClarificationReason: intentResponse.ClarificationReason, Message: intentResponse.Message,
		}, nil
	}
	candidates, missingRoles, err := s.findCandidates(ctx, session, descriptors, intentResponse.Intents)
	if err != nil {
		return nil, err
	}
	if len(missingRoles) > 0 {
		missingSet := make(map[string]struct{}, len(missingRoles))
		missingIntents := make([]notebookCopilotIntent, 0, len(missingRoles))
		for _, role := range missingRoles {
			missingSet[role] = struct{}{}
			for _, intent := range intentResponse.Intents {
				if strings.TrimSpace(intent.Role) == role {
					missingIntents = append(missingIntents, intent)
					break
				}
			}
		}
		var expansion notebookCopilotRemoteResponse
		if err := s.callCopilot(ctx, userToken, map[string]interface{}{
			"query": request.Query, "kernel": request.Kernel,
			"candidates": []interface{}{}, "resources": []interface{}{},
			"missing_intents": missingIntents,
		}, &expansion); err != nil {
			return nil, err
		}
		if expansion.Status == "intents_ready" {
			mergedIntents := mergeNotebookIntentQueries(intentResponse.Intents, expansion.Intents, missingSet)
			candidates, missingRoles, err = s.findCandidates(ctx, session, descriptors, mergedIntents)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(missingRoles) > 0 {
		return &NotebookCopilotResponse{
			Status: "need_clarification", ClarificationReason: "data_source_not_found",
			Message: "当前 Notebook Session 未找到以下输入数据：" + strings.Join(missingRoles, "、"),
		}, nil
	}
	if len(candidates) == 0 {
		return &NotebookCopilotResponse{
			Status: "need_clarification", ClarificationReason: "data_source_not_found",
			Message: "当前 Notebook Session 可访问的数据中未找到匹配资源",
		}, nil
	}
	var recommendation notebookCopilotRemoteResponse
	if err := s.callCopilot(ctx, userToken, map[string]interface{}{
		"query": request.Query, "kernel": request.Kernel, "candidates": candidates, "resources": []interface{}{},
	}, &recommendation); err != nil {
		return nil, err
	}
	if recommendation.Status != "candidates_ready" {
		return nil, fmt.Errorf("Copilot returned unexpected candidate status %q", recommendation.Status)
	}
	return &NotebookCopilotResponse{
		Status: "need_confirmation", Candidates: recommendation.Candidates,
		ClarificationReason: "data_source_confirmation_required", Message: "请确认 Notebook 使用的数据源",
	}, nil
}

func mergeNotebookIntentQueries(
	base, expanded []notebookCopilotIntent, allowedRoles map[string]struct{},
) []notebookCopilotIntent {
	result := make([]notebookCopilotIntent, 0, len(base))
	indexByRole := make(map[string]int, len(base))
	for _, intent := range base {
		role := strings.TrimSpace(intent.Role)
		if role == "" {
			continue
		}
		intent.Role = role
		intent.SearchQueries = uniqueNotebookSearchQueries(intent.SearchQueries)
		indexByRole[role] = len(result)
		result = append(result, intent)
	}
	for _, intent := range expanded {
		role := strings.TrimSpace(intent.Role)
		if role == "" {
			continue
		}
		if _, allowed := allowedRoles[role]; !allowed {
			continue
		}
		index, exists := indexByRole[role]
		if !exists {
			intent.Role = role
			intent.SearchQueries = uniqueNotebookSearchQueries(intent.SearchQueries)
			indexByRole[role] = len(result)
			result = append(result, intent)
			continue
		}
		result[index].SearchQueries = uniqueNotebookSearchQueries(append(
			result[index].SearchQueries, intent.SearchQueries...,
		))
	}
	return result
}

func uniqueNotebookSearchQueries(queries []string) []string {
	result := make([]string, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		value := strings.TrimSpace(query)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *NotebookCopilotService) generateCode(
	ctx context.Context, userToken string, session *NotebookSession, descriptors []commonModels.EngineRuntimeDescriptor, request NotebookCopilotRequest,
) (*NotebookCopilotResponse, error) {
	descriptorByID := make(map[uint]commonModels.EngineRuntimeDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		descriptorByID[descriptor.ID] = descriptor
	}
	resources := make([]map[string]interface{}, 0, len(request.Selections))
	seenRoles := make(map[string]struct{}, len(request.Selections))
	for _, selection := range request.Selections {
		selection.Role = strings.TrimSpace(selection.Role)
		descriptor, ok := descriptorByID[selection.EngineID]
		if selection.Role == "" || !ok || selection.Path.EngineID != selection.EngineID ||
			selection.CandidateID != notebookCandidateID(selection.EngineID, selection.Path) {
			return nil, ErrNotebookCopilotInvalidRequest
		}
		if _, exists := seenRoles[selection.Role]; exists {
			return nil, ErrNotebookCopilotInvalidRequest
		}
		seenRoles[selection.Role] = struct{}{}
		entry, err := s.findExactLeaf(ctx, session, selection.EngineID, selection.Path)
		if err != nil {
			return nil, err
		}
		facts, err := s.sessions.describeCatalogFactsForSession(ctx, session, selection.EngineID, selection.Path)
		if err != nil {
			return nil, err
		}
		resource := map[string]interface{}{
			"candidate_id": selection.CandidateID, "role": selection.Role,
			"engine_id": selection.EngineID, "engine_name": descriptor.Name, "engine_type": descriptor.EngineType,
			"name": entry.Name, "term": entry.Term, "kind": entry.Kind,
			"path": selection.Path, "path_names": notebookPathNames(selection.Path), "path_segments": selection.Path.Segments,
			"fields": []interface{}{},
		}
		if table := plugin.CatalogFactsTableInfo(facts); table != nil {
			resource["fields"] = table.Fields
		}
		if spatial := plugin.CatalogFactsSpatialInfo(facts); spatial != nil {
			resource["geometry_column"] = spatial.PrimaryGeometryName()
			resource["geometry_type"] = spatial.PrimaryGeometryType()
			resource["crs"] = spatial.PrimaryCRSRef()
		}
		resources = append(resources, resource)
	}
	var generated notebookCopilotRemoteResponse
	if err := s.callCopilot(ctx, userToken, map[string]interface{}{
		"query": request.Query, "kernel": request.Kernel, "candidates": []interface{}{}, "resources": resources,
	}, &generated); err != nil {
		return nil, err
	}
	if generated.Status != "success" || strings.TrimSpace(generated.Code) == "" {
		return nil, fmt.Errorf("Copilot returned unexpected generation status %q", generated.Status)
	}
	return &NotebookCopilotResponse{
		Status: "success", Code: generated.Code, Explanation: generated.Explanation, Warnings: generated.Warnings,
	}, nil
}

func (s *NotebookCopilotService) findCandidates(
	ctx context.Context, session *NotebookSession, descriptors []commonModels.EngineRuntimeDescriptor, intents []notebookCopilotIntent,
) ([]NotebookCopilotCandidate, []string, error) {
	roleOrder := make([]string, 0, len(intents))
	seenRoles := make(map[string]struct{}, len(intents))
	for _, intent := range intents {
		role := strings.TrimSpace(intent.Role)
		if role == "" {
			continue
		}
		if _, exists := seenRoles[role]; !exists {
			seenRoles[role] = struct{}{}
			roleOrder = append(roleOrder, role)
		}
	}
	if len(roleOrder) == 0 {
		return nil, nil, nil
	}
	roleCandidates := make(map[string][]NotebookCopilotCandidate, len(roleOrder))
	baseLimit := notebookCopilotMaxCandidates / len(roleOrder)
	extra := notebookCopilotMaxCandidates % len(roleOrder)
	roleLimits := make(map[string]int, len(roleOrder))
	for index, role := range roleOrder {
		roleLimits[role] = baseLimit
		if index < extra {
			roleLimits[role]++
		}
	}
	for _, descriptor := range descriptors {
		leaves, err := s.sessions.listCatalogLeavesForSession(ctx, session, descriptor.ID, notebookCopilotMaxLeaves)
		if err != nil {
			return nil, nil, err
		}
		for _, intent := range intents {
			role := strings.TrimSpace(intent.Role)
			if role == "" || len(roleCandidates[role]) >= roleLimits[role] {
				continue
			}
			for _, leaf := range leaves {
				if !notebookCandidateMatches(leaf, descriptor, intent.SearchQueries) {
					continue
				}
				roleCandidates[role] = append(roleCandidates[role], NotebookCopilotCandidate{
					CandidateID: notebookCandidateID(descriptor.ID, leaf.Path), Role: role,
					EngineID: descriptor.ID, EngineName: descriptor.Name, EngineType: descriptor.EngineType,
					Name: leaf.Name, Term: leaf.Term, Kind: leaf.Kind, Path: leaf.Path, PathNames: notebookPathNames(leaf.Path),
				})
				if len(roleCandidates[role]) >= roleLimits[role] {
					break
				}
			}
		}
	}
	candidates := make([]NotebookCopilotCandidate, 0, notebookCopilotMaxCandidates)
	missingRoles := make([]string, 0)
	for _, role := range roleOrder {
		if len(roleCandidates[role]) == 0 {
			missingRoles = append(missingRoles, role)
			continue
		}
		candidates = append(candidates, roleCandidates[role]...)
	}
	return candidates, missingRoles, nil
}

func (s *NotebookCopilotService) findExactLeaf(
	ctx context.Context, session *NotebookSession, engineID uint, target commonClient.EngineCatalogPath,
) (*commonClient.EngineCatalogEntry, error) {
	leaves, err := s.sessions.listCatalogLeavesForSession(ctx, session, engineID, notebookCopilotMaxLeaves)
	if err != nil {
		return nil, err
	}
	targetID := notebookCandidateID(engineID, target)
	for index := range leaves {
		if notebookCandidateID(engineID, leaves[index].Path) == targetID {
			return &leaves[index], nil
		}
	}
	return nil, ErrNotebookCopilotForbidden
}

func (s *NotebookCopilotService) callCopilot(ctx context.Context, token string, payload interface{}, destination interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/copilot/notebook/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("call Copilot notebook generator: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var detail map[string]interface{}
		_ = json.NewDecoder(response.Body).Decode(&detail)
		return fmt.Errorf("Copilot notebook generator returned HTTP %d: %v", response.StatusCode, detail)
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

func notebookCandidateMatches(entry commonClient.EngineCatalogEntry, descriptor commonModels.EngineRuntimeDescriptor, queries []string) bool {
	haystacks := []string{entry.Name, descriptor.Name, descriptor.EngineType}
	for _, segment := range entry.Path.Segments {
		haystacks = append(haystacks, segment.Name)
	}
	for _, query := range queries {
		needle := normalizeNotebookSearchText(query)
		if needle == "" {
			continue
		}
		for _, haystack := range haystacks {
			value := normalizeNotebookSearchText(haystack)
			if value == "" {
				continue
			}
			if value == needle || strings.Contains(value, needle) || strings.Contains(needle, value) {
				return true
			}
		}
	}
	return false
}

func normalizeNotebookSearchText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func notebookCandidateID(engineID uint, path commonClient.EngineCatalogPath) string {
	payload, _ := json.Marshal(struct {
		EngineID uint                           `json:"engine_id"`
		Path     commonClient.EngineCatalogPath `json:"path"`
	}{EngineID: engineID, Path: path})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func notebookPathNames(path commonClient.EngineCatalogPath) []string {
	result := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		if name := strings.TrimSpace(segment.Name); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func (s *NotebookSessionService) listDataEngineDescriptorsForSession(
	ctx context.Context, session *NotebookSession,
) ([]commonModels.EngineRuntimeDescriptor, error) {
	if session == nil {
		return nil, ErrNotebookSessionNotFound
	}
	return s.catalog.ListEngineDescriptors(ctx, session.TenantID, session.SessionAuthorizationID, session.ID)
}

func (s *NotebookSessionService) listCatalogLeavesForSession(
	ctx context.Context, session *NotebookSession, engineID uint, maxLeaves int,
) ([]commonClient.EngineCatalogEntry, error) {
	if session == nil || engineID == 0 || maxLeaves <= 0 {
		return nil, ErrNotebookCopilotInvalidRequest
	}
	queue := []commonClient.EngineCatalogPath{{Version: plugin.CatalogPathVersion, EngineID: engineID, Segments: []commonClient.EngineCatalogSegment{}}}
	leaves := make([]commonClient.EngineCatalogEntry, 0)
	for len(queue) > 0 && len(leaves) < maxLeaves {
		parent := queue[0]
		queue = queue[1:]
		for offset := 0; ; offset += notebookCopilotCatalogPageSize {
			nodes, err := s.catalog.ListChildren(ctx, session.TenantID, session.SessionAuthorizationID, commonClient.NotebookCatalogChildrenRequest{
				SessionID: session.ID, EngineID: engineID, Path: parent,
				Options: commonClient.EngineCatalogListOptions{Limit: notebookCopilotCatalogPageSize, Offset: offset},
			})
			if err != nil {
				return nil, err
			}
			for _, node := range nodes {
				switch node.Role {
				case "branch":
					queue = append(queue, node.Path)
				case "leaf":
					leaves = append(leaves, node)
					if len(leaves) >= maxLeaves {
						return leaves, nil
					}
				}
			}
			if len(nodes) < notebookCopilotCatalogPageSize {
				break
			}
		}
	}
	return leaves, nil
}

func (s *NotebookSessionService) describeCatalogFactsForSession(
	ctx context.Context, session *NotebookSession, engineID uint, path commonClient.EngineCatalogPath,
) (*plugin.CatalogFacts, error) {
	executionID := uuid.NewString()
	executionCtx, cancel, err := s.registerNotebookExecution(ctx, session.ID, executionID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.unregisterNotebookExecution(session.ID, executionID)
	expiresIn := int64(time.Until(session.ExpiresAt).Seconds())
	if expiresIn <= 0 {
		return nil, ErrNotebookSessionNotFound
	}
	access, err := s.catalog.DeriveExecutionEngineAccess(executionCtx, session.TenantID, session.SessionAuthorizationID,
		commonClient.NotebookExecutionEngineAccessRequest{
			SessionID: session.ID, ExecutionID: executionID, EngineID: engineID, ExpiresIn: expiresIn,
		})
	if err != nil {
		return nil, err
	}
	if access.Engine == nil || access.Engine.ID != engineID {
		return nil, ErrNotebookCopilotForbidden
	}
	return plugin.DescribeCatalogFacts(executionCtx, &plugin.Engine{
		ID: access.Engine.ID, EngineType: access.Engine.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(access.Engine.ConnectionInfo),
	}, notebookPluginCatalogPath(path), plugin.CatalogFactsOptions{IncludeSpatialFacts: true})
}
