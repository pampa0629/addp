package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

const maxDataApplicationComponents = 24

var applicationParameterKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

type dataApplicationRepository interface {
	List(tenantID, ownerUserID int64, offset, limit int) ([]models.DataApplication, int64, error)
	Get(tenantID, ownerUserID int64, id string) (*models.DataApplication, error)
	Create(*models.DataApplication) error
	Update(*models.DataApplication, int64) error
	Publish(tenantID, ownerUserID int64, id string, expectedVersion int64, publishedBy int64) (*models.DataApplicationRevision, error)
	Offline(tenantID, ownerUserID int64, id string, expectedVersion int64) error
	Delete(tenantID, ownerUserID int64, id string, expectedVersion int64) error
	GetRuntime(tenantID, ownerUserID int64, id string) (*models.DataApplicationRevision, error)
	GetRuntimeApplication(tenantID int64, id string) (*models.DataApplication, *models.DataApplicationRevision, error)
}

type dataApplicationAccessRuleRepository interface {
	CanExecuteDataApplication(tenantID, subjectID int64, resourceID string, now time.Time) (bool, error)
}

type DataApplicationService struct {
	repository  dataApplicationRepository
	descriptors DescriptorReader
	accessRules dataApplicationAccessRuleRepository
}

func NewDataApplicationService(repository dataApplicationRepository, descriptors DescriptorReader, accessRules dataApplicationAccessRuleRepository) *DataApplicationService {
	return &DataApplicationService{repository: repository, descriptors: descriptors, accessRules: accessRules}
}

func (s *DataApplicationService) List(tenantID, ownerUserID int64, page, pageSize int) ([]models.DataApplicationSummaryResponse, int64, error) {
	applications, total, err := s.repository.List(tenantID, ownerUserID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	responses := make([]models.DataApplicationSummaryResponse, len(applications))
	for index := range applications {
		responses[index] = dataApplicationSummary(applications[index])
	}
	return responses, total, nil
}

func (s *DataApplicationService) Get(tenantID, ownerUserID int64, id string) (*models.DataApplicationResponse, error) {
	application, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	return dataApplicationResponse(*application)
}

func (s *DataApplicationService) Create(ctx context.Context, tenantID, ownerUserID int64, descriptorRequest DescriptorRequest, input models.DataApplicationCreateRequest) (*models.DataApplicationResponse, error) {
	name, description, err := validateDataApplicationIdentity(input.Name, input.Description)
	if err != nil || tenantID <= 0 || ownerUserID <= 0 {
		return nil, ErrInvalidDataApplication
	}
	snapshot, err := s.normalizeDraftSnapshot(ctx, descriptorRequest, input.Snapshot)
	if err != nil {
		return nil, err
	}
	normalized, contentHash, err := normalizeDataApplication(name, description, snapshot)
	if err != nil {
		return nil, err
	}
	application := &models.DataApplication{
		ID: uuid.NewString(), TenantID: tenantID, OwnerUserID: ownerUserID,
		Name: name, Description: description, DraftSnapshot: datatypes.JSON(normalized), DraftContentHash: contentHash,
		PublicationStatus: models.PublicationStatusUnpublished, CurrentRevisionHash: "", Version: 1,
	}
	if err := s.repository.Create(application); err != nil {
		return nil, err
	}
	return dataApplicationResponse(*application)
}

func (s *DataApplicationService) Update(ctx context.Context, tenantID, ownerUserID int64, id string, descriptorRequest DescriptorRequest, input models.DataApplicationUpdateRequest) (*models.DataApplicationResponse, error) {
	name, description, err := validateDataApplicationIdentity(input.Name, input.Description)
	if err != nil || input.Version <= 0 {
		return nil, ErrInvalidDataApplication
	}
	existing, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	if existing.Version != input.Version {
		return nil, ErrDataApplicationVersionConflict
	}
	normalizedSnapshot, err := s.normalizeDraftSnapshot(ctx, descriptorRequest, input.Snapshot)
	if err != nil {
		return nil, err
	}
	normalized, contentHash, err := normalizeDataApplication(name, description, normalizedSnapshot)
	if err != nil {
		return nil, err
	}
	application := &models.DataApplication{
		ID: id, TenantID: tenantID, OwnerUserID: ownerUserID, Name: name, Description: description,
		DraftSnapshot: datatypes.JSON(normalized), DraftContentHash: contentHash,
	}
	if err := s.repository.Update(application, input.Version); err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	return s.Get(tenantID, ownerUserID, id)
}

func (s *DataApplicationService) Publish(ctx context.Context, tenantID, ownerUserID int64, id string, descriptorRequest DescriptorRequest, version int64) (*models.DataApplicationResponse, error) {
	if version <= 0 {
		return nil, ErrInvalidDataApplication
	}
	application, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	if application.Version != version {
		return nil, ErrDataApplicationVersionConflict
	}
	snapshot, err := decodeDataApplicationSnapshot(application.DraftSnapshot)
	if err != nil {
		return nil, err
	}
	if err := s.validateSnapshot(ctx, descriptorRequest, snapshot); err != nil {
		return nil, err
	}
	if _, err := s.repository.Publish(tenantID, ownerUserID, id, version, ownerUserID); err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	return s.Get(tenantID, ownerUserID, id)
}

func (s *DataApplicationService) Offline(tenantID, ownerUserID int64, id string, version int64) (*models.DataApplicationResponse, error) {
	if version <= 0 {
		return nil, ErrInvalidDataApplication
	}
	if err := s.repository.Offline(tenantID, ownerUserID, id, version); err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	return s.Get(tenantID, ownerUserID, id)
}

func (s *DataApplicationService) Delete(tenantID, ownerUserID int64, id string, version int64) error {
	if version <= 0 {
		return ErrInvalidDataApplication
	}
	return mapDataApplicationRepositoryError(s.repository.Delete(tenantID, ownerUserID, id, version))
}

func (s *DataApplicationService) Runtime(tenantID, userID int64, id string) (*models.DataApplicationRuntimeResponse, error) {
	application, revision, err := s.repository.GetRuntimeApplication(tenantID, id)
	if err != nil {
		return nil, mapDataApplicationRepositoryError(err)
	}
	if application.OwnerUserID != userID {
		if s.accessRules == nil {
			return nil, ErrDataApplicationAccessDenied
		}
		allowed, accessErr := s.accessRules.CanExecuteDataApplication(tenantID, userID, id, time.Now().UTC())
		if accessErr != nil {
			return nil, accessErr
		}
		if !allowed {
			return nil, ErrDataApplicationAccessDenied
		}
	}
	snapshot, err := decodeDataApplicationSnapshot(revision.Snapshot)
	if err != nil {
		return nil, err
	}
	return &models.DataApplicationRuntimeResponse{
		ID: revision.ApplicationID, Name: revision.Name, Description: revision.Description,
		RevisionNumber: revision.RevisionNumber, Snapshot: snapshot, PublishedAt: revision.PublishedAt,
	}, nil
}

func (s *DataApplicationService) normalizeDraftSnapshot(ctx context.Context, request DescriptorRequest, snapshot models.DataApplicationSnapshot) (models.DataApplicationSnapshot, error) {
	canonicalizeDataApplicationSnapshot(&snapshot)
	for index := range snapshot.Components {
		component := &snapshot.Components[index]
		request.Ref = component.ServiceRef
		descriptor, err := s.descriptors.GetDescriptor(ctx, request)
		if err != nil {
			return models.DataApplicationSnapshot{}, err
		}
		component.ContractFingerprint = descriptor.ContractFingerprint
		if err := validateComponentConfiguration(componentConfigurationFromComponent(*component), descriptor); err != nil {
			return models.DataApplicationSnapshot{}, ErrInvalidDataApplication
		}
	}
	if err := s.validateSnapshot(ctx, request, snapshot); err != nil {
		return models.DataApplicationSnapshot{}, err
	}
	return snapshot, nil
}

func (s *DataApplicationService) validateSnapshot(ctx context.Context, request DescriptorRequest, snapshot models.DataApplicationSnapshot) error {
	if snapshot.SchemaVersion != models.DataApplicationSnapshotSchemaVersion || len(snapshot.Components) == 0 || len(snapshot.Components) > maxDataApplicationComponents {
		return ErrInvalidDataApplication
	}
	pageID, err := uuid.Parse(strings.TrimSpace(snapshot.Page.ID))
	if err != nil || pageID == uuid.Nil || strings.TrimSpace(snapshot.Page.Title) == "" || len([]rune(snapshot.Page.Title)) > 200 || !allowedApplicationDisplayMode(snapshot.Page.DisplayMode) || !allowedApplicationRefreshPolicy(snapshot.Page) {
		return ErrInvalidDataApplication
	}
	components := make(map[string]models.DataApplicationComponent, len(snapshot.Components))
	componentParameters := make(map[string]map[string]models.ComponentParameterDefinition, len(snapshot.Components))
	descriptors := make(map[string]*models.ConsumerDescriptor, len(snapshot.Components))
	for _, component := range snapshot.Components {
		componentID, parseErr := uuid.Parse(strings.TrimSpace(component.ID))
		if parseErr != nil || componentID == uuid.Nil || strings.TrimSpace(component.Title) == "" || len([]rune(component.Title)) > 200 || len([]rune(component.Description)) > 2000 {
			return ErrInvalidDataApplication
		}
		if _, duplicate := components[component.ID]; duplicate {
			return ErrInvalidDataApplication
		}
		input := componentConfigurationFromComponent(component)
		request.Ref = component.ServiceRef
		descriptor, readErr := s.descriptors.GetDescriptor(ctx, request)
		if readErr != nil {
			return readErr
		}
		if component.ContractFingerprint != descriptor.ContractFingerprint || validateComponentConfiguration(input, descriptor) != nil {
			return ErrInvalidDataApplication
		}
		components[component.ID] = component
		descriptors[component.ID] = descriptor
		parameters := make(map[string]models.ComponentParameterDefinition, len(component.ParameterDefinitions))
		for _, parameter := range component.ParameterDefinitions {
			parameters[parameter.Key] = parameter
		}
		componentParameters[component.ID] = parameters
	}
	if err := validateApplicationPlacements(snapshot.Page.Placements, components); err != nil {
		return err
	}
	if err := validateApplicationParameters(snapshot.Parameters, snapshot.ParameterBindings, components, componentParameters, descriptors); err != nil {
		return err
	}
	if err := validateApplicationPresentationSections(snapshot.Page, snapshot.Parameters, snapshot.ParameterBindings, components); err != nil {
		return err
	}
	return validateSelectionBindings(snapshot.SelectionBindings, snapshot.Parameters, snapshot.ParameterBindings, components, descriptors)
}

func validateApplicationPlacements(placements []models.DataApplicationComponentLayout, components map[string]models.DataApplicationComponent) error {
	if len(placements) != len(components) {
		return ErrInvalidDataApplication
	}
	seen := make(map[string]struct{}, len(placements))
	for index, placement := range placements {
		if _, ok := components[placement.ComponentID]; !ok || placement.X < 0 || placement.Y < 0 || placement.Width <= 0 || placement.Height <= 0 || placement.X+placement.Width > 12 || placement.Height > 24 {
			return ErrInvalidDataApplication
		}
		if _, duplicate := seen[placement.ComponentID]; duplicate {
			return ErrInvalidDataApplication
		}
		seen[placement.ComponentID] = struct{}{}
		for otherIndex := 0; otherIndex < index; otherIndex++ {
			other := placements[otherIndex]
			if placement.X < other.X+other.Width && placement.X+placement.Width > other.X && placement.Y < other.Y+other.Height && placement.Y+placement.Height > other.Y {
				return ErrInvalidDataApplication
			}
		}
	}
	return nil
}

func validateApplicationParameters(parameters []models.DataApplicationParameter, bindings []models.DataApplicationParameterBinding, components map[string]models.DataApplicationComponent, componentParameters map[string]map[string]models.ComponentParameterDefinition, descriptors map[string]*models.ConsumerDescriptor) error {
	applicationParameters := make(map[string]models.DataApplicationParameter, len(parameters))
	for _, parameter := range parameters {
		if !applicationParameterKeyPattern.MatchString(parameter.Key) || strings.TrimSpace(parameter.Label) == "" || !allowedControlType(parameter.ControlType) {
			return ErrInvalidDataApplication
		}
		if _, duplicate := applicationParameters[parameter.Key]; duplicate {
			return ErrInvalidDataApplication
		}
		applicationParameters[parameter.Key] = parameter
	}
	targetBindings := make(map[string]struct{}, len(bindings))
	boundApplicationParameters := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		applicationParameter, ok := applicationParameters[binding.ApplicationParameterKey]
		if !ok {
			return ErrInvalidDataApplication
		}
		component, ok := components[binding.ComponentID]
		if !ok {
			return ErrInvalidDataApplication
		}
		componentParameter, ok := componentParameters[binding.ComponentID][binding.ComponentParameterKey]
		if !ok || componentParameter.ControlType != applicationParameter.ControlType {
			return ErrInvalidDataApplication
		}
		targetKey := binding.ComponentID + "\x00" + binding.ComponentParameterKey
		if _, duplicate := targetBindings[targetKey]; duplicate {
			return ErrInvalidDataApplication
		}
		targetBindings[targetKey] = struct{}{}
		boundApplicationParameters[binding.ApplicationParameterKey] = struct{}{}
		if len(applicationParameter.DefaultValue) > 0 {
			parameterFilter, exists := componentParameterFilter(component, binding.ComponentParameterKey)
			if !exists {
				return ErrInvalidDataApplication
			}
			descriptor := descriptors[binding.ComponentID]
			field, exists := consumerField(descriptor, parameterFilter.Field)
			if !exists || validateRawFilterValue(applicationParameter.DefaultValue, field, parameterFilter.Operator, descriptor.InputContract.Filter.MaxInValues) != nil {
				return ErrInvalidDataApplication
			}
		}
	}
	for key := range applicationParameters {
		if _, bound := boundApplicationParameters[key]; !bound {
			return ErrInvalidDataApplication
		}
	}
	for componentID, parameterMap := range componentParameters {
		for parameterKey := range parameterMap {
			if _, bound := targetBindings[componentID+"\x00"+parameterKey]; !bound {
				return ErrInvalidDataApplication
			}
		}
	}
	return nil
}

func componentParameterFilter(component models.DataApplicationComponent, key string) (models.ComponentParameterFilter, bool) {
	for _, filter := range component.QueryTemplate.ParameterFilters {
		if filter.ParameterKey == key {
			return filter, true
		}
	}
	return models.ComponentParameterFilter{}, false
}

func validateSelectionBindings(bindings []models.DataApplicationSelectionBinding, parameters []models.DataApplicationParameter, parameterBindings []models.DataApplicationParameterBinding, components map[string]models.DataApplicationComponent, descriptors map[string]*models.ConsumerDescriptor) error {
	applicationParameters := make(map[string]models.DataApplicationParameter, len(parameters))
	for _, parameter := range parameters {
		applicationParameters[parameter.Key] = parameter
	}
	targetBindings := make(map[string][]models.DataApplicationParameterBinding, len(parameters))
	for _, binding := range parameterBindings {
		targetBindings[binding.ApplicationParameterKey] = append(targetBindings[binding.ApplicationParameterKey], binding)
	}
	seenSources := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		sourceComponent, exists := components[binding.SourceComponentID]
		if !exists || len(binding.Assignments) == 0 {
			return ErrInvalidDataApplication
		}
		if _, duplicate := seenSources[binding.SourceComponentID]; duplicate {
			return ErrInvalidDataApplication
		}
		seenSources[binding.SourceComponentID] = struct{}{}
		sourceDescriptor := descriptors[binding.SourceComponentID]
		selectedFields := make(map[string]struct{}, len(sourceComponent.QueryTemplate.Select))
		for _, field := range sourceComponent.QueryTemplate.Select {
			selectedFields[field] = struct{}{}
		}
		outputFields := make(map[string]models.ConsumerOutputField, len(sourceDescriptor.OutputContract.Fields))
		for _, field := range sourceDescriptor.OutputContract.Fields {
			outputFields[field.Name] = field
		}
		seenParameters := make(map[string]struct{}, len(binding.Assignments))
		for _, assignment := range binding.Assignments {
			sourceField, exists := outputFields[assignment.SourceField]
			if !exists || !selectionScalarFieldType(sourceField.Type) {
				return ErrInvalidDataApplication
			}
			if _, selected := selectedFields[assignment.SourceField]; !selected {
				return ErrInvalidDataApplication
			}
			applicationParameter, exists := applicationParameters[assignment.ApplicationParameterKey]
			if !exists || (applicationParameter.Required && sourceField.Nullable) {
				return ErrInvalidDataApplication
			}
			if _, duplicate := seenParameters[assignment.ApplicationParameterKey]; duplicate {
				return ErrInvalidDataApplication
			}
			seenParameters[assignment.ApplicationParameterKey] = struct{}{}
			bindingsForTarget := targetBindings[assignment.ApplicationParameterKey]
			if len(bindingsForTarget) == 0 {
				return ErrInvalidDataApplication
			}
			for _, targetBinding := range bindingsForTarget {
				targetComponent := components[targetBinding.ComponentID]
				parameterFilter, exists := componentParameterFilter(targetComponent, targetBinding.ComponentParameterKey)
				if !exists || !selectionScalarOperator(parameterFilter.Operator) {
					return ErrInvalidDataApplication
				}
				targetField, exists := consumerField(descriptors[targetBinding.ComponentID], parameterFilter.Field)
				if !exists || targetField.Type != sourceField.Type {
					return ErrInvalidDataApplication
				}
			}
		}
	}
	return nil
}

func selectionScalarFieldType(fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeBool,
		datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat,
		datatype.FieldTypeDouble, datatype.FieldTypeDecimal,
		datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp,
		datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func selectionScalarOperator(operator string) bool {
	switch operator {
	case "eq", "ne", "lt", "lte", "gt", "gte":
		return true
	default:
		return false
	}
}

func allowedApplicationDisplayMode(displayMode string) bool {
	switch displayMode {
	case models.ApplicationDisplayModeDesktop, models.ApplicationDisplayModeWallboard:
		return true
	default:
		return false
	}
}

func allowedApplicationRefreshPolicy(page models.DataApplicationPage) bool {
	if page.RefreshIntervalSeconds == nil {
		return false
	}
	refreshIntervalSeconds := *page.RefreshIntervalSeconds
	if page.DisplayMode != models.ApplicationDisplayModeWallboard {
		return refreshIntervalSeconds == models.ApplicationRefreshIntervalDisabled
	}
	switch refreshIntervalSeconds {
	case models.ApplicationRefreshIntervalDisabled,
		models.ApplicationRefreshInterval30Seconds,
		models.ApplicationRefreshInterval60Seconds,
		models.ApplicationRefreshInterval300Seconds:
		return true
	default:
		return false
	}
}

func defaultApplicationVisibleSections() []string {
	return []string{
		models.ApplicationVisibleSectionTitle,
		models.ApplicationVisibleSectionParameters,
		models.ApplicationVisibleSectionQueryActions,
	}
}

func validateApplicationPresentationSections(page models.DataApplicationPage, parameters []models.DataApplicationParameter, bindings []models.DataApplicationParameterBinding, components map[string]models.DataApplicationComponent) error {
	if page.VisibleSections == nil {
		return ErrInvalidDataApplication
	}
	visible := make(map[string]struct{}, len(page.VisibleSections))
	for _, section := range page.VisibleSections {
		switch section {
		case models.ApplicationVisibleSectionTitle, models.ApplicationVisibleSectionParameters, models.ApplicationVisibleSectionQueryActions:
		default:
			return ErrInvalidDataApplication
		}
		if _, duplicate := visible[section]; duplicate {
			return ErrInvalidDataApplication
		}
		visible[section] = struct{}{}
	}
	if page.DisplayMode == models.ApplicationDisplayModeDesktop {
		if len(visible) != len(defaultApplicationVisibleSections()) {
			return ErrInvalidDataApplication
		}
		return nil
	}
	if _, queryActionsVisible := visible[models.ApplicationVisibleSectionQueryActions]; !queryActionsVisible {
		if page.RefreshIntervalSeconds == nil || *page.RefreshIntervalSeconds == models.ApplicationRefreshIntervalDisabled {
			return ErrInvalidDataApplication
		}
	}
	if _, parametersVisible := visible[models.ApplicationVisibleSectionParameters]; parametersVisible {
		return nil
	}
	bindingsByParameter := make(map[string][]models.DataApplicationParameterBinding, len(parameters))
	for _, binding := range bindings {
		bindingsByParameter[binding.ApplicationParameterKey] = append(bindingsByParameter[binding.ApplicationParameterKey], binding)
	}
	for _, parameter := range parameters {
		if !parameter.Required {
			continue
		}
		parameterBindings := bindingsByParameter[parameter.Key]
		if len(parameter.DefaultValue) == 0 || len(parameterBindings) == 0 {
			return ErrInvalidDataApplication
		}
		for _, binding := range parameterBindings {
			component, exists := components[binding.ComponentID]
			if !exists {
				return ErrInvalidDataApplication
			}
			filter, exists := componentParameterFilter(component, binding.ComponentParameterKey)
			if !exists || !rawApplicationParameterHasValue(parameter.DefaultValue, filter.Operator) {
				return ErrInvalidDataApplication
			}
		}
	}
	return nil
}

func rawApplicationParameterHasValue(raw json.RawMessage, operator string) bool {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return false
	}
	if operator == "is_null" || operator == "is_not_null" {
		booleanValue, ok := value.(bool)
		return ok && booleanValue
	}
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []any:
		if len(typed) == 0 {
			return false
		}
		for _, item := range typed {
			if item == nil || item == "" {
				return false
			}
		}
	}
	return true
}

func consumerField(descriptor *models.ConsumerDescriptor, name string) (models.ConsumerQueryField, bool) {
	for _, field := range descriptor.InputContract.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return models.ConsumerQueryField{}, false
}

func componentConfigurationFromComponent(component models.DataApplicationComponent) models.ComponentConfiguration {
	return models.ComponentConfiguration{
		Name: component.Title, Description: component.Description, ServiceRef: &component.ServiceRef,
		ParameterDefinitions: component.ParameterDefinitions, QueryTemplate: component.QueryTemplate,
		DefaultParameterValues: component.DefaultParameterValues, RendererType: component.RendererType,
		RendererConfig: component.RendererConfig,
	}
}

func normalizeDataApplication(name, description string, snapshot models.DataApplicationSnapshot) ([]byte, string, error) {
	canonicalizeDataApplicationSnapshot(&snapshot)
	normalized, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", fmt.Errorf("encode data application snapshot: %w", err)
	}
	publishedDocument, err := json.Marshal(struct {
		Name        string                         `json:"name"`
		Description string                         `json:"description"`
		Snapshot    models.DataApplicationSnapshot `json:"snapshot"`
	}{Name: name, Description: description, Snapshot: snapshot})
	if err != nil {
		return nil, "", fmt.Errorf("encode data application content: %w", err)
	}
	digest := sha256.Sum256(publishedDocument)
	return normalized, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateDataApplicationIdentity(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len([]rune(name)) > 200 || len([]rune(description)) > 2000 {
		return "", "", ErrInvalidDataApplication
	}
	return name, description, nil
}

func decodeDataApplicationSnapshot(raw []byte) (models.DataApplicationSnapshot, error) {
	var snapshot models.DataApplicationSnapshot
	if err := decodeStrict(json.RawMessage(raw), &snapshot); err != nil {
		return models.DataApplicationSnapshot{}, fmt.Errorf("decode data application snapshot: %w", err)
	}
	canonicalizeDataApplicationSnapshot(&snapshot)
	return snapshot, nil
}

func canonicalizeDataApplicationSnapshot(snapshot *models.DataApplicationSnapshot) {
	if snapshot.Page.DisplayMode == "" {
		snapshot.Page.DisplayMode = models.ApplicationDisplayModeDesktop
	}
	if snapshot.Page.RefreshIntervalSeconds == nil {
		refreshIntervalSeconds := models.ApplicationRefreshIntervalDisabled
		snapshot.Page.RefreshIntervalSeconds = &refreshIntervalSeconds
	}
	if snapshot.Page.VisibleSections == nil {
		snapshot.Page.VisibleSections = defaultApplicationVisibleSections()
	} else {
		visibleSections := make(map[string]struct{}, len(snapshot.Page.VisibleSections))
		for _, section := range snapshot.Page.VisibleSections {
			visibleSections[section] = struct{}{}
		}
		snapshot.Page.VisibleSections = snapshot.Page.VisibleSections[:0]
		for _, section := range defaultApplicationVisibleSections() {
			if _, visible := visibleSections[section]; visible {
				snapshot.Page.VisibleSections = append(snapshot.Page.VisibleSections, section)
			}
		}
	}
	if snapshot.Page.Placements == nil {
		snapshot.Page.Placements = []models.DataApplicationComponentLayout{}
	}
	if snapshot.Components == nil {
		snapshot.Components = []models.DataApplicationComponent{}
	}
	if snapshot.Parameters == nil {
		snapshot.Parameters = []models.DataApplicationParameter{}
	}
	if snapshot.ParameterBindings == nil {
		snapshot.ParameterBindings = []models.DataApplicationParameterBinding{}
	}
	if snapshot.SelectionBindings == nil {
		snapshot.SelectionBindings = []models.DataApplicationSelectionBinding{}
	}
	for index := range snapshot.Components {
		component := &snapshot.Components[index]
		if component.ParameterDefinitions == nil {
			component.ParameterDefinitions = []models.ComponentParameterDefinition{}
		}
		if component.DefaultParameterValues == nil {
			component.DefaultParameterValues = map[string]json.RawMessage{}
		}
		if component.QueryTemplate.Select == nil {
			component.QueryTemplate.Select = []string{}
		}
		if component.QueryTemplate.ParameterFilters == nil {
			component.QueryTemplate.ParameterFilters = []models.ComponentParameterFilter{}
		}
		if component.QueryTemplate.OrderBy == nil {
			component.QueryTemplate.OrderBy = []models.QueryOrder{}
		}
	}
}

func dataApplicationSummary(application models.DataApplication) models.DataApplicationSummaryResponse {
	return models.DataApplicationSummaryResponse{
		ID: application.ID, Name: application.Name, Description: application.Description,
		PublicationStatus: application.PublicationStatus, CurrentRevisionNumber: application.CurrentRevisionNumber,
		HasUnpublishedChanges: application.CurrentRevisionNumber == nil || application.DraftContentHash != application.CurrentRevisionHash,
		Version:               application.Version, CreatedAt: application.CreatedAt, UpdatedAt: application.UpdatedAt,
	}
}

func dataApplicationResponse(application models.DataApplication) (*models.DataApplicationResponse, error) {
	snapshot, err := decodeDataApplicationSnapshot(application.DraftSnapshot)
	if err != nil {
		return nil, err
	}
	return &models.DataApplicationResponse{
		DataApplicationSummaryResponse: dataApplicationSummary(application), TenantID: application.TenantID,
		OwnerUserID: application.OwnerUserID, Snapshot: snapshot,
	}, nil
}

func mapDataApplicationRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrDataApplicationNotFound):
		return ErrDataApplicationNotFound
	case errors.Is(err, repository.ErrDataApplicationVersionConflict):
		return ErrDataApplicationVersionConflict
	case errors.Is(err, repository.ErrDataApplicationAlreadyPublished):
		return ErrDataApplicationAlreadyPublished
	case errors.Is(err, repository.ErrDataApplicationNotPublished):
		return ErrDataApplicationNotPublished
	default:
		return err
	}
}
