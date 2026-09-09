package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type APIConsumerService struct {
	repo *repository.APIConsumerRepository
}

func NewAPIConsumerService(repo *repository.APIConsumerRepository) *APIConsumerService {
	return &APIConsumerService{repo: repo}
}

func (s *APIConsumerService) Create(
	req *models.CreateAPIConsumerRequest,
	tenantID, principalID uint,
) (*models.APIConsumer, error) {
	grants, err := normalizeAPIConsumerGrants(req.ServiceGrants)
	if err != nil {
		return nil, err
	}
	rateLimit := req.RateLimitPerMinute
	if rateLimit == 0 {
		rateLimit = 60
	}
	consumer := &models.APIConsumer{
		Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description),
		TenantID: tenantID, RateLimitPerMinute: rateLimit, Status: "active",
		CreatedByPrincipalID: principalID, ServiceGrants: grants,
	}
	if consumer.Name == "" {
		return nil, errors.New("API consumer name is required")
	}
	if err := s.repo.Create(consumer); err != nil {
		return nil, fmt.Errorf("create API consumer: %w", err)
	}
	return consumer, nil
}

func (s *APIConsumerService) Get(id, tenantID uint) (*models.APIConsumer, error) {
	consumer, err := s.repo.FindByIDAndTenant(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("API consumer not found: %w", err)
	}
	return consumer, nil
}

func (s *APIConsumerService) List(tenantID uint) ([]models.APIConsumer, error) {
	return s.repo.FindByTenantID(tenantID)
}

func (s *APIConsumerService) Update(
	id, tenantID uint,
	req *models.UpdateAPIConsumerRequest,
) (*models.APIConsumer, error) {
	consumer, err := s.repo.FindByIDAndTenant(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("API consumer not found: %w", err)
	}
	if req.Name != "" {
		consumer.Name = strings.TrimSpace(req.Name)
	}
	if req.Description != nil {
		consumer.Description = strings.TrimSpace(*req.Description)
	}
	if req.RateLimitPerMinute > 0 {
		consumer.RateLimitPerMinute = req.RateLimitPerMinute
	}
	if req.Status != "" {
		consumer.Status = req.Status
	}
	var grants []models.APIConsumerServiceGrant
	if req.ServiceGrants != nil {
		grants, err = normalizeAPIConsumerGrants(req.ServiceGrants)
		if err != nil {
			return nil, err
		}
	}
	if err := s.repo.Update(consumer, grants); err != nil {
		return nil, fmt.Errorf("update API consumer: %w", err)
	}
	return s.repo.FindByIDAndTenant(id, tenantID)
}

func (s *APIConsumerService) Delete(id, tenantID uint) error {
	return s.repo.Delete(id, tenantID)
}

func (s *APIConsumerService) CreateCredential(
	consumerID, tenantID, principalID uint,
	req *models.CreateAPIConsumerCredentialRequest,
) (*models.APIConsumerCredential, error) {
	consumer, err := s.repo.FindByIDAndTenant(consumerID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("API consumer not found: %w", err)
	}
	if consumer.Status != "active" {
		return nil, errors.New("API consumer is not active")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, errors.New("credential expiry must be in the future")
	}
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("generate API consumer credential: %w", err)
	}
	plainCredential := "addp_api_" + base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(plainCredential))
	credential := &models.APIConsumerCredential{
		APIConsumerID: consumerID, KeyPrefix: plainCredential[:12],
		KeyHash: hex.EncodeToString(hash[:]), Name: strings.TrimSpace(req.Name),
		ExpiresAt: req.ExpiresAt, Status: "active", CreatedByPrincipalID: principalID,
		PlainTextCredential: plainCredential,
	}
	if err := s.repo.CreateCredential(credential); err != nil {
		return nil, fmt.Errorf("create API consumer credential: %w", err)
	}
	return credential, nil
}

func (s *APIConsumerService) ListCredentials(consumerID, tenantID uint) ([]models.APIConsumerCredential, error) {
	if _, err := s.repo.FindByIDAndTenant(consumerID, tenantID); err != nil {
		return nil, fmt.Errorf("API consumer not found: %w", err)
	}
	return s.repo.FindCredentials(consumerID)
}

func (s *APIConsumerService) RevokeCredential(
	consumerID, credentialID, tenantID, principalID uint,
) error {
	if _, err := s.repo.FindByIDAndTenant(consumerID, tenantID); err != nil {
		return fmt.Errorf("API consumer not found: %w", err)
	}
	return s.repo.RevokeCredential(consumerID, credentialID, principalID)
}

func (s *APIConsumerService) ValidateCredential(
	keyHash string,
) (*models.APIConsumerCredentialValidationResponse, error) {
	credential, consumer, err := s.repo.FindCredentialWithConsumer(keyHash)
	if err != nil || consumer.Status != "active" ||
		(credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) {
		return &models.APIConsumerCredentialValidationResponse{Valid: false}, nil
	}
	go func() { _ = s.repo.UpdateCredentialLastUsed(credential.ID) }()
	grants := make([]models.APIConsumerServiceReference, 0, len(consumer.ServiceGrants))
	for _, grant := range consumer.ServiceGrants {
		grants = append(grants, models.APIConsumerServiceReference{
			ServiceType: grant.ServiceType, ServiceID: grant.ServiceID,
		})
	}
	return &models.APIConsumerCredentialValidationResponse{
		Valid: true, APIConsumerID: consumer.ID, APIConsumerName: consumer.Name,
		TenantID: consumer.TenantID, ServiceGrants: grants,
		RateLimitPerMinute: consumer.RateLimitPerMinute, ExpiresAt: credential.ExpiresAt,
	}, nil
}

func normalizeAPIConsumerGrants(
	references []models.APIConsumerServiceReference,
) ([]models.APIConsumerServiceGrant, error) {
	if len(references) == 0 {
		return nil, errors.New("at least one service grant is required")
	}
	seen := make(map[string]struct{}, len(references))
	grants := make([]models.APIConsumerServiceGrant, 0, len(references))
	for _, reference := range references {
		if reference.ServiceType != models.APIConsumerServiceTypeQuery || reference.ServiceID == 0 {
			return nil, errors.New("invalid API consumer service reference")
		}
		key := fmt.Sprintf("%s:%d", reference.ServiceType, reference.ServiceID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		grants = append(grants, models.APIConsumerServiceGrant{
			ServiceType: reference.ServiceType, ServiceID: reference.ServiceID,
		})
	}
	return grants, nil
}
