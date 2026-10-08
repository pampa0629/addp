package service

import (
	"context"
	"strconv"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

type SystemPlatformPublicationAuthorizer struct{ client *client.SystemServiceClient }

func NewSystemPlatformPublicationAuthorizer(c *client.SystemServiceClient) (*SystemPlatformPublicationAuthorizer, error) {
	if c == nil {
		return nil, repository.ErrInvalid
	}
	return &SystemPlatformPublicationAuthorizer{client: c}, nil
}

// Check obtains fresh audit identity for this exact compiled release. The
// returned actor is not a credential, publication receipt or long-lived grant.
func (a *SystemPlatformPublicationAuthorizer) Check(ctx context.Context, snapshot *platform.Snapshot) (models.PlatformActor, error) {
	if a == nil || a.client == nil || snapshot == nil {
		return models.PlatformActor{}, repository.ErrInvalid
	}
	definition, err := snapshot.Context()
	if err != nil {
		return models.PlatformActor{}, err
	}
	observed, err := a.client.CheckPlatformPublication(ctx, authorization.PlatformPublicationCheck{Capability: definition.Capability, Revision: strconv.FormatUint(definition.Revision, 10), Digest: snapshot.Digest()})
	if err != nil {
		return models.PlatformActor{}, err
	}
	principal, err := strconv.ParseInt(observed.PrincipalID, 10, 64)
	if err != nil {
		return models.PlatformActor{}, repository.ErrIntegrity
	}
	version, err := strconv.ParseInt(observed.AuthorizationVersion, 10, 64)
	if err != nil {
		return models.PlatformActor{}, repository.ErrIntegrity
	}
	return models.PlatformActor{ContextType: observed.ContextType, PrincipalID: principal, PrincipalType: observed.PrincipalType, AuthorizationVersion: version}, nil
}
