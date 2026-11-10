package service

import (
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

var errPrivatePublication = errors.New("private GeoPackage publication requires reconciliation")

type filePublicationAuthority interface {
	AuthorizeDevelopFilePublication(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFilePublicationRequest) (*client.DevelopFilePublicationObservation, error)
}

// publishPrivateFile handles one original, already bound Runtime output. It
// reports only private Provider facts. Public execution completion, recovery
// under a new credential, scanning and initial grants belong to later stages.
func (f *privateFileWorkflowExecutor) publishPrivateFile(ctx context.Context, lease execution.Lease, runtimeID string, output plugin.WorkflowPrivateFile, content io.Reader) (*plugin.NewFilePublicationOutcome, error) {
	provider, ok := f.provider.(plugin.NewFilePublicationProvider)
	if !ok || f.publication == nil || content == nil || output.Size <= 0 || lease.TenantID <= 0 {
		return nil, errPrivatePublication
	}
	tenant := uint(lease.TenantID)
	operation := uuid.NewString()
	if err := f.repository.RegisterPrivatePublication(ctx, lease, runtimeID, operation, output); err != nil {
		return nil, errPrivatePublication
	}
	record, err := f.repository.ReadPrivatePublication(ctx, tenant, lease.ExecutionID)
	if err != nil {
		return nil, errPrivatePublication
	}
	authority := f.publication(tenant)
	request := client.DevelopFilePublicationRequest{
		ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token,
		ScopeDigest: record.Issued.ScopeDigest, ApprovalID: record.ApprovalID, OperationID: operation,
		Stage: client.DevelopFilePublicationPrepare,
	}
	admission, err := authority.AuthorizeDevelopFilePublication(ctx, record.Issued, request)
	if err != nil || admission == nil || admission.Engine == nil {
		return nil, errPrivatePublication
	}
	prepareCtx, stopPrepare := context.WithDeadline(ctx, admission.ExpiresAt)
	defer stopPrepare()
	if prepareCtx.Err() != nil {
		return nil, errPrivatePublication
	}
	connection := plugin.ConnectionInfo(admission.Engine.ConnectionInfo)
	frozen := record.Prepared.NewFilePublicationRequest
	prepared, prepareErr := provider.PrepareNewFile(prepareCtx, connection, frozen, content)
	if prepareErr != nil {
		if reflect.DeepEqual(prepared.NewFilePublicationRequest, frozen) && len(prepared.ProviderState) != 0 {
			// Best effort only while the original lease is current. The prior
			// registration remains the recovery anchor if this write fails.
			_ = f.repository.RecordPrivatePublicationPreparation(prepareCtx, lease, prepared, false)
		}
		return nil, errPrivatePublication
	}
	if err := f.repository.RecordPrivatePublicationPreparation(prepareCtx, lease, prepared, true); err != nil {
		return nil, errPrivatePublication
	}
	record, err = f.repository.ReadPrivatePublication(prepareCtx, tenant, lease.ExecutionID)
	if err != nil {
		return nil, errPrivatePublication
	}
	request.Stage, request.PreparationDigest = client.DevelopFilePublicationRelease, record.PreparationDigest
	release, err := authority.AuthorizeDevelopFilePublication(prepareCtx, record.Issued, request)
	if err != nil || release == nil || !release.BusinessDeadline.Equal(admission.BusinessDeadline) {
		return nil, errPrivatePublication
	}
	publishCtx, stopPublish := context.WithDeadline(prepareCtx, release.ExpiresAt)
	defer stopPublish()
	claimed, err := f.repository.ClaimPrivatePublicationDispatch(publishCtx, lease, *release)
	if err != nil || publishCtx.Err() != nil {
		return nil, errPrivatePublication
	}
	outcome, publishErr := provider.PublishNewFile(publishCtx, connection, *claimed, plugin.NewFileNotPublished)
	if outcome.Validate(*claimed) != nil || (publishErr != nil && outcome.State != plugin.NewFileNotPublished) || outcome.State == plugin.NewFileUnknown {
		if publishCtx.Err() != nil {
			return nil, errPrivatePublication
		}
		// One observation of the original prepared object after the call
		// returned. Only Provider evidence can settle an uncertain LINK.
		outcome, err = provider.ResolveNewFile(publishCtx, connection, *claimed)
		if err != nil || outcome.State != plugin.NewFilePublished || outcome.Validate(*claimed) != nil {
			return nil, errPrivatePublication
		}
	}
	if err := f.repository.RecordPrivatePublicationOutcome(publishCtx, lease, *claimed, outcome); err != nil {
		return nil, errPrivatePublication
	}
	if outcome.CleanupPending {
		if publishCtx.Err() != nil {
			return &outcome, errPrivatePublication
		}
		if err := provider.CleanupNewFile(publishCtx, connection, *claimed, outcome.State); err != nil {
			return &outcome, errPrivatePublication
		}
		if err := f.repository.MarkPrivatePublicationCleaned(publishCtx, lease, *claimed); err != nil {
			return &outcome, errPrivatePublication
		}
		outcome.CleanupPending = false
	}
	if outcome.State != plugin.NewFilePublished {
		return &outcome, errPrivatePublication
	}
	return &outcome, nil
}
