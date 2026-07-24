package iam

import (
	"context"
	"fmt"

	commonapi "github.com/addp/common/api"
)

type LoginLocalBrowserInput struct {
	Username string
	Password string
	Audit    AuditMetadata
}

type BrowserLoginService struct {
	identityService  *IdentityService
	selectionService *ContextSelectionService
}

func NewBrowserLoginService(
	identityService *IdentityService,
	selectionService *ContextSelectionService,
) (*BrowserLoginService, error) {
	if identityService == nil || selectionService == nil {
		return nil, fmt.Errorf("%w: browser login dependencies are required", commonapi.ErrBadRequest)
	}
	return &BrowserLoginService{
		identityService:  identityService,
		selectionService: selectionService,
	}, nil
}

func (s *BrowserLoginService) LoginLocalBrowser(
	ctx context.Context,
	input LoginLocalBrowserInput,
) (*ContextSelectionResult, error) {
	if s == nil || s.identityService == nil || s.selectionService == nil {
		return nil, fmt.Errorf("%w: browser login service is required", commonapi.ErrBadRequest)
	}
	authenticated, err := s.identityService.AuthenticateLocalAccount(
		ctx,
		input.Username,
		input.Password,
		input.Audit,
	)
	if err != nil {
		return nil, err
	}
	return s.selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID: authenticated.PrincipalID,
		Authentication: SessionAuthentication{
			Methods:         []string{"password"},
			AssuranceLevel:  AssuranceLevelAAL1,
			AuthenticatedAt: authenticated.AuthenticatedAt,
		},
		Audit: input.Audit,
	})
}
