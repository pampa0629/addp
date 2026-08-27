package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type viewRepository interface {
	List(tenantID, ownerUserID int64, offset, limit int) ([]models.View, int64, error)
	Get(tenantID, ownerUserID int64, id string) (*models.View, error)
	Create(*models.View) error
	Update(*models.View, int64) error
	Delete(tenantID, ownerUserID int64, id string) error
}

type ViewService struct {
	repository  viewRepository
	descriptors DescriptorReader
}

func NewViewService(repository viewRepository, descriptors DescriptorReader) *ViewService {
	return &ViewService{repository: repository, descriptors: descriptors}
}

func (s *ViewService) List(tenantID, ownerUserID int64, page, pageSize int) ([]models.ViewResponse, int64, error) {
	views, total, err := s.repository.List(tenantID, ownerUserID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	responses := make([]models.ViewResponse, len(views))
	for index := range views {
		responses[index] = models.ViewResponseOf(views[index])
	}
	return responses, total, nil
}

func (s *ViewService) Get(tenantID, ownerUserID int64, id string) (*models.ViewResponse, error) {
	view, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	response := models.ViewResponseOf(*view)
	return &response, nil
}

func (s *ViewService) Create(ctx context.Context, tenantID, ownerUserID int64, request DescriptorRequest, input models.ViewWriteRequest) (*models.ViewResponse, error) {
	if tenantID <= 0 || ownerUserID <= 0 || input.ServiceRef == nil || input.Version != nil {
		return nil, ErrInvalidView
	}
	request.Ref = *input.ServiceRef
	descriptor, err := s.descriptors.GetDescriptor(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := validateViewRequest(input, descriptor); err != nil {
		return nil, err
	}
	view, err := buildView(tenantID, ownerUserID, input, descriptor)
	if err != nil {
		return nil, err
	}
	if err := s.repository.Create(view); err != nil {
		return nil, err
	}
	response := models.ViewResponseOf(*view)
	return &response, nil
}

func (s *ViewService) Update(ctx context.Context, tenantID, ownerUserID int64, id string, request DescriptorRequest, input models.ViewWriteRequest) (*models.ViewResponse, error) {
	if tenantID <= 0 || ownerUserID <= 0 || input.ServiceRef == nil || input.Version == nil || *input.Version <= 0 {
		return nil, ErrInvalidView
	}
	existing, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if existing.ServiceType != input.ServiceRef.ServiceType || existing.ServiceID != input.ServiceRef.ServiceID {
		return nil, ErrInvalidView
	}
	request.Ref = *input.ServiceRef
	descriptor, err := s.descriptors.GetDescriptor(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := validateViewRequest(input, descriptor); err != nil {
		return nil, err
	}
	view, err := buildView(tenantID, ownerUserID, input, descriptor)
	if err != nil {
		return nil, err
	}
	view.ID = id
	view.CreatedAt = existing.CreatedAt
	if err := s.repository.Update(view, *input.Version); err != nil {
		return nil, mapRepositoryError(err)
	}
	updated, err := s.repository.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	response := models.ViewResponseOf(*updated)
	return &response, nil
}

func (s *ViewService) Delete(tenantID, ownerUserID int64, id string) error {
	return mapRepositoryError(s.repository.Delete(tenantID, ownerUserID, id))
}

func buildView(tenantID, ownerUserID int64, input models.ViewWriteRequest, descriptor *models.ConsumerDescriptor) (*models.View, error) {
	parameters, err := json.Marshal(input.ParameterDefinitions)
	if err != nil {
		return nil, fmt.Errorf("encode parameter definitions: %w", err)
	}
	query, err := json.Marshal(input.QueryTemplate)
	if err != nil {
		return nil, fmt.Errorf("encode query template: %w", err)
	}
	defaults := input.DefaultParameterValues
	if defaults == nil {
		defaults = map[string]json.RawMessage{}
	}
	defaultValues, err := json.Marshal(defaults)
	if err != nil {
		return nil, fmt.Errorf("encode default parameter values: %w", err)
	}
	return &models.View{
		ID: uuid.NewString(), TenantID: tenantID, OwnerUserID: ownerUserID,
		Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		ServiceType: input.ServiceRef.ServiceType, ServiceID: input.ServiceRef.ServiceID,
		ContractFingerprint:  descriptor.ContractFingerprint,
		ParameterDefinitions: datatypes.JSON(parameters), QueryTemplate: datatypes.JSON(query),
		DefaultParameterValues: datatypes.JSON(defaultValues), RendererType: input.RendererType,
		RendererConfig: datatypes.JSON(append([]byte(nil), input.RendererConfig...)), Version: 1,
	}, nil
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrViewNotFound):
		return ErrViewNotFound
	case errors.Is(err, repository.ErrViewVersionConflict):
		return ErrViewVersionConflict
	default:
		return err
	}
}
