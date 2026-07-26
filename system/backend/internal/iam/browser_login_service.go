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
	mfaService       *MFAService
	selectionService *ContextSelectionService
}

func NewBrowserLoginService(
	identityService *IdentityService,
	mfaService *MFAService,
	selectionService *ContextSelectionService,
) (*BrowserLoginService, error) {
	if identityService == nil || mfaService == nil || selectionService == nil {
		return nil, fmt.Errorf("%w: browser login dependencies are required", commonapi.ErrBadRequest)
	}
	return &BrowserLoginService{
		identityService:  identityService,
		mfaService:       mfaService,
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
	hasMFA, err := s.mfaService.HasActiveTOTP(ctx, authenticated.PrincipalID)
	if err != nil {
		return nil, err
	}
	if hasMFA {
		challenge, err := s.mfaService.BeginChallenge(ctx, authenticated, input.Audit)
		if err != nil {
			return nil, err
		}
		return &ContextSelectionResult{
			NextAction: ContextSelectionNextActionVerifyMFA,
			MFA:        challenge,
		}, nil
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

func (s *BrowserLoginService) VerifyLocalBrowserMFA(
	ctx context.Context,
	input VerifyMFAChallengeInput,
) (*ContextSelectionResult, error) {
	if s == nil || s.mfaService == nil || s.selectionService == nil {
		return nil, fmt.Errorf("%w: browser login service is required", commonapi.ErrBadRequest)
	}
	selectionInput, err := s.mfaService.VerifyChallenge(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.selectionService.BeginContextSelection(ctx, *selectionInput)
}
