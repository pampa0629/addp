package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type StandardCollectionService struct {
	repo      *repository.StandardCollectionRepository
	refs      *repository.TenantReferenceRepository
	directory *commonclient.SystemServiceClient
}

func NewStandardCollectionService(repo *repository.StandardCollectionRepository, refs *repository.TenantReferenceRepository, directory *commonclient.SystemServiceClient) *StandardCollectionService {
	return &StandardCollectionService{repo: repo, refs: refs, directory: directory}
}

func (s *StandardCollectionService) Create(ctx context.Context, tenantID, userID int64, req *models.CreateStandardCollectionRequest) (*models.StandardCollectionAggregate, error) {
	code, name, description, summary := strings.TrimSpace(req.Code), strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), strings.TrimSpace(req.ChangeSummary)
	if code == "" || name == "" || description == "" || summary == "" {
		return nil, ErrInvalidStandardCollection
	}
	exists, err := s.repo.ExistsByCode(code, tenantID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	members, err := s.refs.ResolveCollectionMembers(tenantID, req.Members)
	if err != nil {
		return nil, err
	}
	collection := &models.StandardCollection{TenantID: tenantID, Code: code, CreatedBy: userID}
	revision := &models.StandardCollectionRevision{Name: name, Description: description, ChangeSummary: summary, CreatedBy: userID}
	if err := s.repo.Create(collection, revision, members); err != nil {
		return nil, err
	}
	return s.Get(ctx, collection.ID, tenantID, userID)
}

func (s *StandardCollectionService) Get(ctx context.Context, id, tenantID, userID int64) (*models.StandardCollectionAggregate, error) {
	aggregate, err := s.repo.GetAggregate(id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.enrichAggregate(ctx, tenantID, aggregate); err != nil {
		return nil, err
	}
	return aggregate, nil
}

func (s *StandardCollectionService) List(ctx context.Context, tenantID, userID int64, keyword, status string, page, pageSize int) ([]models.StandardCollectionAggregate, int64, error) {
	if status != "" && status != models.RevisionStatusDraft && status != models.RevisionStatusInReview && status != models.RevisionStatusPublished {
		return nil, 0, ErrInvalidStandardCollection
	}
	items, total, err := s.repo.List(tenantID, userID, keyword, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for index := range items {
		if err := s.enrichAggregate(ctx, tenantID, &items[index]); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (s *StandardCollectionService) ListRevisions(ctx context.Context, id, tenantID int64) ([]models.StandardCollectionRevision, error) {
	revisions, err := s.repo.ListRevisions(id, tenantID)
	if err != nil {
		return nil, err
	}
	for index := range revisions {
		if err := s.enrichMembers(tenantID, &revisions[index]); err != nil {
			return nil, err
		}
	}
	return revisions, nil
}

func (s *StandardCollectionService) ListEvents(ctx context.Context, id, tenantID int64, page, pageSize int) ([]models.StandardCollectionEvent, int64, error) {
	events, total, err := s.repo.ListEvents(id, tenantID, page, pageSize)
	if err != nil || len(events) == 0 {
		return events, total, err
	}
	actorIDs := make([]int64, 0, len(events))
	for _, event := range events {
		actorIDs = append(actorIDs, event.ActorID)
	}
	resolved, err := s.directory.WithTenantID(uint(tenantID)).ResolveStandardGovernanceUsers(ctx, uniquePrincipalIDs(actorIDs))
	if err != nil {
		return nil, 0, ErrStandardGovernanceDirectoryUnavailable
	}
	byID := make(map[int64]commonclient.StandardGovernanceUser, len(resolved))
	for _, item := range resolved {
		byID[item.ID] = item
	}
	for index := range events {
		actor := byID[events[index].ActorID]
		events[index].ActorName, events[index].ActorCode = actor.Name, actor.Code
		events[index].Referenceable = actor.Found && actor.Referenceable
	}
	return events, total, nil
}

func (s *StandardCollectionService) CreateRevision(ctx context.Context, id, tenantID, userID int64, req *models.CreateStandardCollectionRevisionRequest) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentOwner, models.CollectionAssignmentMaintainer); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ChangeSummary) == "" {
		return nil, ErrInvalidStandardCollection
	}
	if err := s.repo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary)); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) UpdateRevision(ctx context.Context, id, revisionID, tenantID, userID int64, req *models.UpdateStandardCollectionRevisionRequest) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentOwner, models.CollectionAssignmentMaintainer); err != nil {
		return nil, err
	}
	name, description, summary := strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), strings.TrimSpace(req.ChangeSummary)
	if name == "" || description == "" || summary == "" {
		return nil, ErrInvalidStandardCollection
	}
	members, err := s.refs.ResolveCollectionMembers(tenantID, req.Members)
	if err != nil {
		return nil, err
	}
	revision := &models.StandardCollectionRevision{Name: name, Description: description, ChangeSummary: summary}
	if err := s.repo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision, members); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) Submit(ctx context.Context, id, revisionID, tenantID, userID, version int64) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentOwner, models.CollectionAssignmentMaintainer); err != nil {
		return nil, err
	}
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(revision.Name) == "" || strings.TrimSpace(revision.Description) == "" || len(revision.Members) == 0 {
		return nil, ErrInvalidStandardCollection
	}
	reviewers, err := s.repo.CountReviewersExcept(id, userID)
	if err != nil {
		return nil, err
	}
	if reviewers == 0 {
		return nil, ErrStandardCollectionReviewerRequired
	}
	if err := s.repo.Transition(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) Return(ctx context.Context, id, revisionID, tenantID, userID, version int64) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentReviewer); err != nil {
		return nil, err
	}
	if err := s.repo.Transition(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) Publish(ctx context.Context, id, revisionID, tenantID, userID, version int64) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentReviewer); err != nil {
		return nil, err
	}
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.SubmittedBy == nil || *revision.SubmittedBy == userID {
		return nil, ErrStandardCollectionSelfApproval
	}
	if err := s.repo.Publish(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) ReplaceAssignments(ctx context.Context, id, tenantID, userID int64, req *models.ReplaceStandardCollectionAssignmentsRequest) (*models.StandardCollectionAggregate, error) {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentOwner); err != nil {
		return nil, err
	}
	seen, principals, hasOwner := make(map[string]struct{}, len(req.Assignments)), make([]int64, 0, len(req.Assignments)), false
	assignments := make([]models.StandardCollectionAssignment, 0, len(req.Assignments))
	for _, input := range req.Assignments {
		if input.PrincipalID <= 0 || (input.Role != models.CollectionAssignmentOwner && input.Role != models.CollectionAssignmentMaintainer && input.Role != models.CollectionAssignmentReviewer) {
			return nil, ErrInvalidStandardCollection
		}
		key := fmt.Sprintf("%d:%s", input.PrincipalID, input.Role)
		if _, exists := seen[key]; exists {
			return nil, ErrInvalidStandardCollection
		}
		seen[key] = struct{}{}
		principals = append(principals, input.PrincipalID)
		if input.Role == models.CollectionAssignmentOwner {
			hasOwner = true
		}
		assignments = append(assignments, models.StandardCollectionAssignment{PrincipalID: input.PrincipalID, Role: input.Role})
	}
	if !hasOwner {
		return nil, ErrStandardCollectionOwnerRequired
	}
	unique := uniquePrincipalIDs(principals)
	resolved, err := s.directory.WithTenantID(uint(tenantID)).ResolveStandardGovernanceUsers(ctx, unique)
	if err != nil {
		return nil, ErrStandardGovernanceDirectoryUnavailable
	}
	for _, item := range resolved {
		if !item.Found || !item.Referenceable {
			return nil, ErrInvalidStandardCollection
		}
	}
	if err := s.repo.ReplaceAssignments(id, tenantID, userID, req.Version, assignments); err != nil {
		return nil, err
	}
	return s.Get(ctx, id, tenantID, userID)
}

func (s *StandardCollectionService) ListUserCandidates(ctx context.Context, tenantID int64, search string, page, pageSize int) (*commonclient.StandardGovernanceUserList, error) {
	result, err := s.directory.WithTenantID(uint(tenantID)).ListStandardGovernanceUsers(ctx, search, page, pageSize)
	if err != nil {
		return nil, ErrStandardGovernanceDirectoryUnavailable
	}
	return result, nil
}

func (s *StandardCollectionService) Delete(id, tenantID, userID, version int64) error {
	if err := s.requireRole(id, tenantID, userID, models.CollectionAssignmentOwner); err != nil {
		return err
	}
	if err := s.repo.Delete(id, tenantID, userID, version); err != nil {
		if errors.Is(err, repository.ErrRevisionNotEditable) {
			return ErrInvalidRevisionTransition
		}
		return err
	}
	return nil
}

func (s *StandardCollectionService) requireRole(id, tenantID, userID int64, roles ...string) error {
	ok, err := s.repo.HasRole(id, tenantID, userID, roles...)
	if err != nil {
		return err
	}
	if !ok {
		return ErrStandardCollectionAccessDenied
	}
	return nil
}

func (s *StandardCollectionService) enrichAggregate(ctx context.Context, tenantID int64, aggregate *models.StandardCollectionAggregate) error {
	if aggregate.DraftRevision != nil {
		if err := s.enrichMembers(tenantID, aggregate.DraftRevision); err != nil {
			return err
		}
	}
	if aggregate.CurrentRevision != nil {
		if err := s.enrichMembers(tenantID, aggregate.CurrentRevision); err != nil {
			return err
		}
	}
	if len(aggregate.Assignments) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(aggregate.Assignments))
	for _, assignment := range aggregate.Assignments {
		ids = append(ids, assignment.PrincipalID)
	}
	resolved, err := s.directory.WithTenantID(uint(tenantID)).ResolveStandardGovernanceUsers(ctx, uniquePrincipalIDs(ids))
	if err != nil {
		return ErrStandardGovernanceDirectoryUnavailable
	}
	byID := make(map[int64]commonclient.StandardGovernanceUser, len(resolved))
	for _, item := range resolved {
		byID[item.ID] = item
	}
	for index := range aggregate.Assignments {
		item := byID[aggregate.Assignments[index].PrincipalID]
		aggregate.Assignments[index].PrincipalName, aggregate.Assignments[index].PrincipalCode = item.Name, item.Code
		aggregate.Assignments[index].Referenceable = item.Found && item.Referenceable
	}
	return nil
}

func (s *StandardCollectionService) enrichMembers(tenantID int64, revision *models.StandardCollectionRevision) error {
	inputs := make([]models.StandardCollectionMemberInput, 0, len(revision.Members))
	for _, member := range revision.Members {
		inputs = append(inputs, models.StandardCollectionMemberInput{MemberType: member.MemberType, MemberID: member.MemberID})
	}
	resolved, err := s.refs.ResolveCollectionMembers(tenantID, inputs)
	if err != nil {
		return err
	}
	for index := range revision.Members {
		revision.Members[index].Name, revision.Members[index].Code = resolved[index].Name, resolved[index].Code
	}
	return nil
}

func uniquePrincipalIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
