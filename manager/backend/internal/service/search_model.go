package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"

	commonInference "github.com/addp/common/inference"
	"github.com/addp/manager/internal/repository"
)

func (s *searchSession) prepareModel(ctx context.Context, initial bool) error {
	service := s.service
	if service.vectorRepo == nil || service.configurationProvider == nil || service.bindingService == nil || service.inferenceClient == nil {
		key := sha256.Sum256([]byte("manager-search/keyword-only/v1"))
		if !initial && key != s.modelKey {
			return ErrSearchContextChanged
		}
		s.modelKey = key
		return nil
	}
	runtime := service.configurationProvider.Current()
	binding, err := service.bindingService.Resolve(ctx, s.tenant)
	if errors.Is(err, ErrInferenceScenarioNotConfigured) {
		key := sha256.Sum256([]byte("manager-search/no-embedding-binding/v1"))
		if !initial && key != s.modelKey {
			return ErrSearchContextChanged
		}
		s.modelKey = key
		return nil
	}
	if err != nil {
		return err
	}
	profile, err := service.inferenceClient.ResolveProfile(ctx, commonInference.ResolveProfileRequest{SchemaVersion: commonInference.SchemaVersion, TenantID: s.tenant, ModelProfileID: binding.ModelProfileID, Operation: commonInference.OperationEmbedding, Modality: commonInference.ModalityText})
	if err != nil {
		return err
	}
	if profile == nil || profile.Dimension != runtime.Dimension || profile.ProfileVersion <= 0 {
		return fmt.Errorf("search embedding profile does not match configured dimension")
	}
	encoded, _ := json.Marshal(struct {
		Binding        ResolvedInferenceScenarioBinding
		Version        uint64
		Dimension      int
		Distance       float64
		ProfileVersion int64
	}{binding, runtime.Version, runtime.Dimension, runtime.MaxDistance, profile.ProfileVersion})
	key := sha256.Sum256(encoded)
	if !initial {
		if key != s.modelKey {
			return ErrSearchContextChanged
		}
		return nil
	}
	response, err := service.inferenceClient.Embed(ctx, commonInference.EmbeddingRequest{SchemaVersion: commonInference.SchemaVersion, TenantID: s.tenant, ModelProfileID: binding.ModelProfileID, Inputs: []commonInference.EmbeddingInput{{Modality: commonInference.ModalityText, Text: s.query}}})
	if err != nil {
		return err
	}
	if response == nil || response.ProfileVersion != profile.ProfileVersion || len(response.Vectors) != 1 || len(response.Vectors[0]) != profile.Dimension {
		return ErrSearchContextChanged
	}
	size := int64(unsafe.Sizeof(repository.SearchVectorQuery{})) + int64(len(binding.ModelProfileID)) + int64(profile.Dimension)*4
	if err := s.reserve(size); err != nil {
		return err
	}
	vector := make([]float32, len(response.Vectors[0]))
	copy(vector, response.Vectors[0])
	s.vectorQuery = &repository.SearchVectorQuery{TenantID: s.tenant, EngineID: s.engineFilter(), Vector: vector, ModelProfileID: binding.ModelProfileID, ProfileVersion: profile.ProfileVersion, Dimension: profile.Dimension, MaxDistance: runtime.MaxDistance}
	s.modelKey = key
	return nil
}
